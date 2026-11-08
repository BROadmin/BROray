package servers

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func TestWithdrawActiveService(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(id); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": true, "selectionRule": "lowest-ping"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(autoSwitchFile(e.d))
	undo, err := e.d.Svc.WithdrawActiveServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := readObjectFile(autoSwitchFile(e.d))
	if ActiveID(e.d) != "" || e.running.Load() || cfg["enabled"] != true || !RecordExists(e.d, id) {
		t.Fatal("withdrawal not complete")
	}
	// Callback restores control files and the previous runtime after catalog rollback.
	if undo == nil {
		t.Fatal("missing rollback")
	}
	if err := undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(autoSwitchFile(e.d))
	if ActiveID(e.d) != id || !e.running.Load() || string(before) != string(after) {
		t.Fatal("rollback not exact")
	}
}

func TestWithdrawAutoReplacement(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "trojan://pw@b.example:443#B")
	c := e.addServer(t, "trojan://pw@c.example:443#C")
	e.probeOK[b], e.probeOK[c] = true, true
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": true, "minimumRating": "good", "selectionRule": "preferred", "preferredServerId": c, "cooldownMinutes": 1}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(autoSwitchFile(e.d))
	if _, err := e.d.Svc.WithdrawActiveServer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := DeleteRecord(e.d, a); err != nil {
		t.Fatal(err)
	}
	if err := e.s.enqueueDue(); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.d.Queue.FindLive("servers:replacement", "all"); !ok {
		t.Fatal("replacement not queued")
	}
	go e.d.Queue.Run(e.d.Ctx)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ActiveID(e.d) == "" {
		time.Sleep(20 * time.Millisecond)
	}
	if ActiveID(e.d) != c {
		t.Fatalf("preferred replacement not selected: %q", ActiveID(e.d))
	}
	after, _ := os.ReadFile(autoSwitchFile(e.d))
	if string(before) != string(after) {
		t.Fatal("owner policy changed")
	}
}

func TestWithdrawNoReplacementAndManualStop(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	e.addServer(t, "trojan://pw@dead.example:443#Dead")
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": true, "minimumRating": "good", "selectionRule": "best-quality", "cooldownMinutes": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.Svc.WithdrawActiveServer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := DeleteRecord(e.d, a); err != nil {
		t.Fatal(err)
	}
	if err := e.s.enqueueDue(); err != nil {
		t.Fatal(err)
	}
	r, ok := e.d.Queue.FindLive("servers:replacement", "all")
	if !ok {
		t.Fatal("replacement not queued")
	}
	go e.d.Queue.Run(e.d.Ctx)
	waitReq(t, e, r.RequestID, "completed")
	st, _ := e.s.readState()
	if ActiveID(e.d) != "" || e.running.Load() || st["status"] != "no-candidates" {
		t.Fatalf("bad no-candidate state: %v", st["status"])
	}
	if n, _ := num(get(st, "withdrawal", "nextAttemptEpoch")); n <= float64(e.s.now().Unix()) {
		t.Fatal("retry is unbounded")
	}
	if _, err := e.s.deactivate(); err != nil {
		t.Fatal(err)
	}
	st, _ = e.s.readState()
	if get(st, "withdrawal", "pending") == true {
		t.Fatal("manual disconnect must cancel replacement intent")
	}
}

func TestWithdrawStopFailure(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(id); err != nil {
		t.Fatal(err)
	}
	e.s.stopXray = func(context.Context) error { e.running.Store(false); return errors.New("stop interrupted") }
	undo, err := e.d.Svc.WithdrawActiveServer(context.Background())
	if err == nil || undo == nil {
		t.Fatalf("failure must provide rollback: %v", err)
	}
	if err := undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ActiveID(e.d) != id || !e.running.Load() {
		t.Fatal("failed stop stranded runtime")
	}
}
