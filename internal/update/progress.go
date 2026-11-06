package update

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Event is one JSON line of `--progress json` (launcher and installer
// alike). Diagnostics go to stderr; the last line of a run is always a
// "result" event — a run that ends without one did not succeed.
type Event struct {
	Event             string `json:"event"` // stage | log | result
	Stage             string `json:"stage,omitempty"`
	Progress          int    `json:"progress,omitempty"`
	Message           string `json:"message,omitempty"`
	OK                *bool  `json:"ok,omitempty"`
	Code              string `json:"code,omitempty"`
	State             string `json:"state,omitempty"` // result: committed | rolled-back | failed | rollback-failed
	Version           string `json:"version,omitempty"`
	From              string `json:"from,omitempty"`
	RollbackPerformed bool   `json:"rollbackPerformed,omitempty"`
	Source            string `json:"source,omitempty"` // launcher | installer
	At                string `json:"at,omitempty"`
}

// Emitter writes events as JSON lines; safe for concurrent use.
type Emitter struct {
	mu     sync.Mutex
	w      io.Writer
	Source string
}

func NewEmitter(w io.Writer, source string) *Emitter { return &Emitter{w: w, Source: source} }

func (e *Emitter) Emit(ev Event) {
	if e == nil || e.w == nil {
		return
	}
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339)
	}
	if ev.Source == "" {
		ev.Source = e.Source
	}
	b, _ := json.Marshal(ev)
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.w.Write(append(b, '\n'))
}

func (e *Emitter) Stage(stage string, progress int, msg string) {
	e.Emit(Event{Event: "stage", Stage: stage, Progress: progress, Message: msg})
}

func (e *Emitter) Log(msg string) { e.Emit(Event{Event: "log", Message: msg}) }

func Bool(b bool) *bool { return &b }
