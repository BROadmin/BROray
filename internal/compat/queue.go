package compat

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Coordinator is the 3.x background-operations coordinator
// (lib/operation-coordinator.sh + lib/operation-scheduling.sh) in one
// process: a priority queue of requests (servers checks, failover,
// subscription refreshes, DoT auto-check), each executed as a sequence of
// stages by one worker, plus the registry of running and finished
// operations shown on the BROray page (operations panel).
//
// Queue policy (operation-scheduling.sh:5-18): priority by action+source,
// limit 64 queued, last 128 receipts kept, coalescing of identical live
// requests, nonce idempotency, automation pause for non-USER sources.
type Coordinator struct {
	d    *Deps
	mu   sync.Mutex
	spec map[string]ActionSpec
	seq  int64

	live     []*Request          // queued + running, in submit order
	byNonce  map[string]*Request // nonce → request (live or receipt)
	receipts []*Request          // finished, newest last, ≤128

	ops    []*Operation // running + finished (≤128 finished kept)
	paused bool

	journal  *Journal
	wake     chan struct{}
	workerUp bool
	running  *Request
	cancel   context.CancelFunc // of the running stage, if cooperative
}

// Stage is one step of a queued action.
type Stage struct {
	Name      string // public stage: checking|verify|probe|fetch|parse|apply|activate
	Phase     string // operation phase while running: checking|fetching|parsing|committing|switching|working|waiting
	Protected bool   // not cancelable (3.x "protected")
	Global    bool   // holds the global operation fence while running
	Run       func(t *Task) error
}

// ActionSpec defines a queue action ("servers:quality", "subscriptions:refresh", …).
type ActionSpec struct {
	Name     string
	Type     string         // public operation type: server_operation, subscription_update, auto_switch, dns_operation, …
	Priority map[string]int // by source; "*" = default
	Stages   []Stage
}

// Task is the execution context of one request.
type Task struct {
	Ctx       context.Context
	RequestID string
	Action    string
	Target    string
	Source    string
	Context   string
	Op        *Operation
	data      map[string]any
	req       *Request
	c         *Coordinator
}

// Set / Get carry data from one stage to the next.
func (t *Task) Set(k string, v any) { t.data[k] = v }
func (t *Task) Get(k string) any    { return t.data[k] }

// Result records the user-visible result message and error code of the request.
func (t *Task) Result(message, errorCode string) {
	t.c.mu.Lock()
	t.req.Message, t.req.ErrorCode = message, errorCode
	t.c.mu.Unlock()
}

// Request is one queue item / receipt.
type Request struct {
	RequestID   string
	Nonce       string
	Action      string
	Target      string
	Source      string
	Context     string
	InputHash   string
	Priority    int
	Seq         int64
	State       string // queued|running|completed|failed|cancelled
	Stage       string
	OperationID string
	Message     string
	ErrorCode   string
	SubmittedAt time.Time
	FinishedAt  time.Time
}

// QueueError carries a coordinator error code (D-shape {"ok":false,"errorCode":…}).
type QueueError struct{ Code string }

func (e *QueueError) Error() string { return e.Code }

// HTTPStatus maps a coordinator error code like api/operations/common.sh:100-116.
func (e *QueueError) HTTPStatus() int {
	switch e.Code {
	case "OPERATION_BUSY", "DOMAIN_OPERATION_BUSY", "CANCEL_NOT_SUPPORTED", "RECOVERY_BLOCKED":
		return 409
	case "REQUEST_UNCONFIRMED":
		return 404
	}
	return 503
}

const (
	queueLimit   = 64
	receiptLimit = 128
)

var nonceRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func NewCoordinator(d *Deps) *Coordinator {
	c := &Coordinator{d: d, spec: map[string]ActionSpec{}, byNonce: map[string]*Request{}, wake: make(chan struct{}, 1)}
	c.journal = NewJournal(d.Path("state", "operation-events"))
	var a struct {
		Paused *bool `json:"paused"`
	}
	if err := ReadJSON(c.automationFile(), &a); err == nil {
		c.paused = a.Paused == nil || *a.Paused // 3.x: anything but exactly paused:false is paused
	}
	return c
}

func (c *Coordinator) automationFile() string { return c.d.Path("state", "background-automation.json") }

// Journal returns the operation event journal.
func (c *Coordinator) Journal() *Journal { return c.journal }

// Define registers an action. Call from an area Setup.
func (c *Coordinator) Define(s ActionSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spec[s.Name] = s
}

func (s ActionSpec) priority(source string) int {
	if p, ok := s.Priority[source]; ok {
		return p
	}
	if p, ok := s.Priority["*"]; ok {
		return p
	}
	return 5
}

// Receipt is the public queue receipt (operation-scheduling.sh:115-126).
func (r *Request) Receipt(coalesced bool) map[string]any {
	return map[string]any{"ok": true, "requestId": r.RequestID, "state": r.State, "priority": r.Priority, "coalesced": coalesced}
}

// Submit enqueues a request (queue-submit). nonce: 32 lowercase hex.
func (c *Coordinator) Submit(action, target, source, context, nonce string) (*Request, bool, error) {
	if !nonceRe.MatchString(nonce) {
		return nil, false, &QueueError{"REQUEST_MISMATCH"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.spec[action]
	if !ok {
		return nil, false, &QueueError{"INVALID_QUEUE_ACTION"}
	}
	hash := SHA256Hex([]byte(action + "\n" + target + "\n" + source + "\n" + context))
	if r, ok := c.byNonce[nonce]; ok {
		if r.InputHash != hash {
			return nil, false, &QueueError{"REQUEST_MISMATCH"}
		}
		cp := *r // a copy: the worker mutates the live request
		return &cp, r.Nonce != nonce, nil
	}
	for _, r := range c.live {
		if r.InputHash == hash {
			c.byNonce[nonce] = r
			cp := *r // a copy: the worker mutates the live request
			return &cp, true, nil
		}
	}
	queued := 0
	for _, r := range c.live {
		if r.State == "queued" {
			queued++
		}
	}
	if queued >= queueLimit {
		return nil, false, &QueueError{"QUEUE_FULL"}
	}
	c.seq++
	r := &Request{RequestID: "q-" + nonce, Nonce: nonce, Action: action, Target: target, Source: source, Context: context,
		InputHash: hash, Priority: s.priority(source), Seq: c.seq, State: "queued", SubmittedAt: time.Now()}
	if len(s.Stages) > 0 {
		r.Stage = s.Stages[0].Name
	}
	c.live = append(c.live, r)
	c.byNonce[nonce] = r
	c.kick()
	cp := *r // a copy: the worker mutates the live request
	return &cp, false, nil
}

// Enqueue is Submit for internal (automatic) sources with a fresh nonce.
// It reuses a live request with the same input (3.x reuses queued/running).
func (c *Coordinator) Enqueue(action, target, source, context string) (*Request, error) {
	r, _, err := c.Submit(action, target, source, context, RandHex(16))
	return r, err
}

// Lookup returns a request by nonce (queue-lookup).
func (c *Coordinator) Lookup(nonce string) (*Request, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.byNonce[nonce]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// FindLive returns a live (queued/running) request for action+target, if any.
func (c *Coordinator) FindLive(action, target string) (*Request, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.live {
		if r.Action == action && r.Target == target {
			cp := *r
			return &cp, true
		}
	}
	return nil, false
}

// Request returns a copy of a request (live or receipt) by id.
func (c *Coordinator) Request(id string) (*Request, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.live {
		if r.RequestID == id {
			cp := *r
			return &cp, true
		}
	}
	for _, r := range c.receipts {
		if r.RequestID == id {
			cp := *r
			return &cp, true
		}
	}
	return nil, false
}

// CancelRequest cancels a queued request (ops_queue_cancel).
func (c *Coordinator) CancelRequest(id string) (*Request, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, r := range c.live {
		if r.RequestID != id {
			continue
		}
		if r.State == "running" {
			return nil, &QueueError{"OPERATION_BUSY"}
		}
		r.State, r.FinishedAt, r.Stage = "cancelled", time.Now(), "finished"
		c.live = append(c.live[:i], c.live[i+1:]...)
		c.addReceiptLocked(r)
		cp := *r
		return &cp, nil
	}
	for _, r := range c.receipts {
		if r.RequestID == id && r.State == "cancelled" {
			cp := *r
			return &cp, nil
		}
	}
	return nil, &QueueError{"REQUEST_UNCONFIRMED"}
}

func (c *Coordinator) addReceiptLocked(r *Request) {
	c.receipts = append(c.receipts, r)
	if len(c.receipts) > receiptLimit {
		drop := c.receipts[0]
		c.receipts = c.receipts[1:]
		for n, x := range c.byNonce {
			if x == drop {
				delete(c.byNonce, n)
			}
		}
	}
}

// Paused reports the automation pause flag.
func (c *Coordinator) Paused() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.paused }

// SetPaused writes background-automation.json {schemaVersion:1,paused,updatedAt}.
func (c *Coordinator) SetPaused(p bool) error {
	c.mu.Lock()
	c.paused = p
	c.mu.Unlock()
	c.kick()
	return WriteJSON(c.automationFile(), map[string]any{"schemaVersion": 1, "paused": p, "updatedAt": StampUTC(time.Now())})
}

func (c *Coordinator) kick() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// reasonLocked implements operation-scheduling.sh:208-223.
func (c *Coordinator) reasonLocked(r *Request) any {
	if r.State != "queued" {
		return nil
	}
	if c.paused && r.Source != "USER" {
		return "automation_paused"
	}
	if r.Priority >= 2 {
		for _, x := range c.live {
			if x != r && x.Priority < 2 && (!c.paused || x.Source == "USER" || x.State == "running") {
				return "active_connection"
			}
		}
	}
	return "awaiting_resource"
}

// QueueRows is queue_public for live requests + finished receipts, sorted
// by priority, sequence.
func (c *Coordinator) QueueRows() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var all []*Request
	all = append(all, c.live...)
	all = append(all, c.receipts...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Priority != all[j].Priority {
			return all[i].Priority < all[j].Priority
		}
		return all[i].Seq < all[j].Seq
	})
	rows := make([]map[string]any, 0, len(all))
	for _, r := range all {
		typ := PublicType(r.Action)
		if s, ok := c.spec[r.Action]; ok && typ == "unknown" {
			typ = PublicType(s.Type)
		}
		var op any
		if r.State == "running" && r.OperationID != "" {
			op = PublicOperationID(r.OperationID)
		}
		stage := r.Stage
		if r.State != "queued" && r.State != "running" {
			stage = "finished"
		}
		var prio any
		if r.Priority >= 0 && r.Priority <= 5 {
			prio = r.Priority
		}
		row := map[string]any{"requestId": PublicRequestID(r.RequestID), "priority": prio, "type": typ, "source": PublicSource(r.Source),
			"stage":       enumOr(stage, "unknown", "checking", "verify", "probe", "fetch", "parse", "apply", "activate", "finished"),
			"state":       enumOr(r.State, "unknown", "queued", "running", "completed", "cancelled", "failed"),
			"operationId": op, "reason": c.reasonLocked(r)}
		if r.Message != "" {
			row["message"] = r.Message // 3.x JS shows result.message for terminal rows
		}
		rows = append(rows, row)
	}
	return rows
}

// WorkerRunning reports whether Run is active (diagnostics report).
func (c *Coordinator) WorkerRunning() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.workerUp }

// Run starts the worker; it returns when ctx ends.
func (c *Coordinator) Run(ctx context.Context) {
	c.mu.Lock()
	c.workerUp = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.workerUp = false; c.mu.Unlock() }()
	for {
		r, s, op := c.next()
		if r == nil {
			select {
			case <-ctx.Done():
				return
			case <-c.wake:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		c.execute(ctx, r, s, op)
	}
}

func (c *Coordinator) next() (*Request, ActionSpec, *Operation) {
	c.mu.Lock()
	var best *Request
	for _, r := range c.live {
		if r.State != "queued" || (c.paused && r.Source != "USER") {
			continue
		}
		if best == nil || r.Priority < best.Priority || (r.Priority == best.Priority && r.Seq < best.Seq) {
			best = r
		}
	}
	if best == nil {
		c.mu.Unlock()
		return nil, ActionSpec{}, nil
	}
	spec := c.spec[best.Action]
	best.OperationID = "op-" + best.RequestID // op-q-<32hex>
	op := c.startOperationLocked(best.OperationID, spec.Type, best.Action, best.Source, "cooperative", best.RequestID)
	best.State = "running"
	c.running = best
	c.mu.Unlock()
	c.journal.Append(op, "started", "pending", "")
	return best, spec, op
}

func (c *Coordinator) execute(ctx context.Context, r *Request, s ActionSpec, op *Operation) {
	t := &Task{RequestID: r.RequestID, Action: r.Action, Target: r.Target, Source: r.Source, Context: r.Context,
		Op: op, data: map[string]any{}, req: r, c: c}
	var err error
	for _, st := range s.Stages {
		c.mu.Lock()
		r.Stage = st.Name
		c.mu.Unlock()
		op.SetCancelability(map[bool]string{true: "protected", false: "cooperative"}[st.Protected])
		// A cancellation accepted after operation registration but before this
		// stage began must not be lost or allow the next stage to start.
		if op.CancelRequested() {
			err = errCancelled
			break
		}
		if st.Phase != "" {
			op.Phase(st.Phase)
		}
		sctx, cancel := context.WithCancel(ctx)
		if !st.Protected {
			c.bindCooperativeCancel(op, cancel)
		}
		var release Release
		if st.Global {
			op.Phase("waiting")
			release, err = c.d.Lock.Acquire(sctx, "operations", r.Action, r.Target)
			if err == nil && st.Phase != "" {
				op.Phase(st.Phase)
			}
		}
		if err == nil {
			t.Ctx = sctx
			err = safeRun(st.Run, t)
		}
		if release != nil {
			release()
		}
		c.mu.Lock()
		c.cancel = nil
		c.mu.Unlock()
		cancelled := sctx.Err() != nil && ctx.Err() == nil
		cancel()
		if cancelled && err != nil {
			err = errCancelled
		}
		if err != nil {
			break
		}
	}
	c.mu.Lock()
	switch {
	case err == nil:
		r.State = "completed"
	case errors.Is(err, errCancelled) || op.CancelRequested():
		r.State = "cancelled"
	default:
		r.State = "failed"
		if r.Message == "" {
			r.Message = err.Error()
		}
		if r.ErrorCode == "" {
			r.ErrorCode = codeOf(err)
		}
	}
	r.FinishedAt, r.Stage = time.Now(), "finished"
	for i, x := range c.live {
		if x == r {
			c.live = append(c.live[:i], c.live[i+1:]...)
			break
		}
	}
	c.addReceiptLocked(r)
	c.running = nil
	c.mu.Unlock()
	op.Finish(err)
}

// bindCooperativeCancel closes the window between accepting cancellation and
// installing the stage's cancel function. Either this check or cancelRunning
// observes the request, regardless of which wins the race.
func (c *Coordinator) bindCooperativeCancel(op *Operation, cancel context.CancelFunc) {
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	if op.CancelRequested() {
		cancel()
	}
}

var errCancelled = errors.New("OPERATION_CANCELLED: Операция отменена.")

func safeRun(fn func(*Task) error, t *Task) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = errors.New("INTERNAL_ERROR: stage panic")
		}
	}()
	return fn(t)
}

func codeOf(err error) string {
	m := err.Error()
	if i := strings.Index(m, ":"); i > 0 && strings.ToUpper(m[:i]) == m[:i] && !strings.Contains(m[:i], " ") {
		return m[:i]
	}
	return "OPERATION_FAILED"
}

// CancelRunning requests cancellation of the running queued request if its
// current stage is cooperative. Returns false if protected or none.
func (c *Coordinator) cancelRunning(opID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil || c.running.OperationID != opID || c.cancel == nil {
		return false
	}
	c.cancel()
	return true
}
