// Package ops serializes mutating operations: exactly one runs at a time,
// each has an id, phase, progress, a cancel hook and a durable journal line
// at start and end (state/operations.jsonl).
package ops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

var ErrBusy = errors.New("OPERATION_IN_PROGRESS")

type State string

const (
	Running   State = "running"
	Completed State = "completed"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

type Record struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Target     string    `json:"target,omitempty"`
	State      State     `json:"state"`
	Phase      string    `json:"phase,omitempty"`
	Progress   int       `json:"progress"` // 0..100
	Message    string    `json:"message,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	Result     any       `json:"result,omitempty"`
}

// Manager holds the current operation and the last few finished ones.
type Manager struct {
	mu         sync.Mutex
	journal    string
	current    *Record
	cancel     context.CancelFunc
	history    []Record
	Background func(kind string) bool // optional: kinds allowed to run while another op is running (none in dev1)
}

func NewManager(journalPath string) *Manager {
	m := &Manager{journal: journalPath}
	m.loadTail()
	return m
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "op-" + time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:4])
}

// Handle lets the running function report progress.
type Handle struct {
	m   *Manager
	rec *Record
	Ctx context.Context
}

func (h *Handle) Phase(phase, message string) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	if h.m.current == h.rec {
		h.rec.Phase, h.rec.Message = phase, message
	}
}

func (h *Handle) Progress(done, total int) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	if h.m.current == h.rec && total > 0 {
		h.rec.Progress = done * 100 / total
	}
}

// Run starts fn in a goroutine as the single current operation and returns
// its id immediately. If an operation is running, ErrBusy.
func (m *Manager) Run(kind, target string, fn func(h *Handle) (any, error)) (string, error) {
	m.mu.Lock()
	if m.current != nil {
		m.mu.Unlock()
		return "", ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	rec := &Record{ID: newID(), Kind: kind, Target: target, State: Running, StartedAt: time.Now().UTC()}
	m.current, m.cancel = rec, cancel
	m.mu.Unlock()
	m.append(*rec)
	go func() {
		result, err := fn(&Handle{m: m, rec: rec, Ctx: ctx})
		m.mu.Lock()
		rec.FinishedAt = time.Now().UTC()
		rec.Progress = 100
		switch {
		case err == nil:
			rec.State, rec.Result = Completed, result
		case errors.Is(err, context.Canceled):
			rec.State, rec.Error = Cancelled, "cancelled"
		default:
			rec.State, rec.Error = Failed, err.Error()
		}
		done := *rec
		m.history = append(m.history, done)
		if len(m.history) > 20 {
			m.history = m.history[len(m.history)-20:]
		}
		m.current, m.cancel = nil, nil
		m.mu.Unlock()
		cancel()
		m.append(done)
	}()
	return rec.ID, nil
}

// RunSync runs fn and waits for it; used by the CLI.
func (m *Manager) RunSync(kind, target string, fn func(h *Handle) (any, error)) (Record, error) {
	id, err := m.Run(kind, target, fn)
	if err != nil {
		return Record{}, err
	}
	for {
		time.Sleep(200 * time.Millisecond)
		if r, ok := m.Get(id); ok && r.State != Running {
			if r.State == Completed {
				return r, nil
			}
			return r, errors.New(r.Error)
		}
	}
}

func (m *Manager) Cancel() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return false
	}
	m.cancel()
	return true
}

func (m *Manager) Current() (Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return Record{}, false
	}
	return *m.current, true
}

func (m *Manager) Get(id string) (Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil && m.current.ID == id {
		return *m.current, true
	}
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].ID == id {
			return m.history[i], true
		}
	}
	return Record{}, false
}

func (m *Manager) History() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, len(m.history))
	copy(out, m.history)
	return out
}

func (m *Manager) append(r Record) {
	if m.journal == "" {
		return
	}
	f, err := os.OpenFile(m.journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	_, _ = f.Write(append(b, '\n'))
	_ = f.Sync()
	if st, err := f.Stat(); err == nil && st.Size() > 512<<10 {
		go m.truncate()
	}
}

func (m *Manager) truncate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := os.ReadFile(m.journal)
	if err != nil || len(b) <= 256<<10 {
		return
	}
	tail := b[len(b)-256<<10:]
	if i := indexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	_ = os.WriteFile(m.journal, tail, 0o600)
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// loadTail restores finished records from the journal so the WebUI shows
// history after a restart. A record still "running" at load time means the
// daemon died mid-operation: it is recorded as failed with a clear message.
func (m *Manager) loadTail() {
	b, err := os.ReadFile(m.journal)
	if err != nil {
		return
	}
	byID := map[string]Record{}
	var order []string
	start := 0
	for i := 0; i <= len(b); i++ {
		if i == len(b) || b[i] == '\n' {
			line := b[start:i]
			start = i + 1
			if len(line) == 0 {
				continue
			}
			var r Record
			if json.Unmarshal(line, &r) != nil {
				continue
			}
			if _, ok := byID[r.ID]; !ok {
				order = append(order, r.ID)
			}
			byID[r.ID] = r
		}
	}
	for _, id := range order {
		r := byID[id]
		if r.State == Running {
			r.State, r.Error, r.FinishedAt = Failed, "interrupted: daemon stopped before the operation finished", time.Now().UTC()
		}
		m.history = append(m.history, r)
	}
	if len(m.history) > 20 {
		m.history = m.history[len(m.history)-20:]
	}
}
