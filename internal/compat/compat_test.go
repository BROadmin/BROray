package compat

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

func testDeps(t *testing.T) *Deps {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	d := &Deps{Root: root, Log: log.New(io.Discard, "", 0), Ctx: context.Background(), Svc: &Services{}}
	d.Lock = NewGlobalLock(d.RunPath("global-operation.json"))
	d.Queue = NewCoordinator(d)
	return d
}

func TestGlobalLockPending(t *testing.T) {
	d := testDeps(t)
	rel, ok := d.Lock.TryAcquire("routes", "export", "netflix")
	if !ok {
		t.Fatal("acquire")
	}
	if _, ok := d.Lock.TryAcquire("dot", "dot:apply", "dns-over-tls"); ok {
		t.Fatal("second acquire must fail")
	}
	d.Lock.MarkPending(true)
	rel()
	if !d.Lock.Busy() {
		t.Fatal("pending fence must survive release")
	}
	// restart restores the pending fence
	g2 := NewGlobalLock(d.RunPath("global-operation.json"))
	if h, ok := g2.Current(); !ok || !h.Pending || h.BundleID != "netflix" {
		t.Fatalf("pending not restored: %+v %v", h, ok)
	}
	rel2, ok := d.Lock.TakePending("routes", "netflix", "resume")
	if !ok {
		t.Fatal("take pending")
	}
	rel2()
	if d.Lock.Busy() {
		t.Fatal("must be free")
	}
}

func TestQueueStagesCoalesceCancel(t *testing.T) {
	d := testDeps(t)
	q := d.Queue
	gate := make(chan struct{})
	var ran []string
	q.Define(ActionSpec{Name: "servers:quality", Type: "server_operation", Priority: map[string]int{"USER": 2, "SERVER_CHECK_AUTO": 3},
		Stages: []Stage{
			{Name: "probe", Phase: "checking", Run: func(t *Task) error { <-gate; ran = append(ran, "probe:"+t.Target); t.Set("x", 1); return nil }},
			{Name: "apply", Phase: "committing", Protected: true, Global: true, Run: func(t *Task) error {
				if t.Get("x") != 1 {
					return errors.New("BAD: lost data")
				}
				ran = append(ran, "apply:"+t.Target)
				t.Result("Проверено", "")
				return nil
			}},
		}})
	n1, n2, n3 := RandHex(16), RandHex(16), RandHex(16)
	r1, co, err := q.Submit("servers:quality", "a", "USER", "ctx", n1)
	if err != nil || co || r1.RequestID != "q-"+n1 {
		t.Fatal(err, co)
	}
	r2, co, _ := q.Submit("servers:quality", "a", "USER", "ctx", n2)
	if !co || r2.RequestID != r1.RequestID {
		t.Fatal("must coalesce")
	}
	if _, _, err := q.Submit("servers:quality", "b", "USER", "ctx", n1); err == nil {
		t.Fatal("nonce reuse with other input must fail")
	}
	r3, _, _ := q.Submit("servers:quality", "c", "USER", "ctx", n3)
	if _, err := q.CancelRequest(r3.RequestID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)
	close(gate)
	deadline := time.Now().Add(3 * time.Second)
	for {
		r, _ := q.Request(r1.RequestID)
		if r.State == "completed" {
			if r.Message != "Проверено" {
				t.Fatal(r.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not completed: %+v", r)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ran) != 2 || ran[0] != "probe:a" || ran[1] != "apply:a" {
		t.Fatal(ran)
	}
	if r, _ := q.Lookup(n3); r.State != "cancelled" {
		t.Fatal(r.State)
	}
	snap := q.Snapshot()
	if snap["complete"] != true || len(snap["queue"].([]map[string]any)) != 2 || len(snap["operations"].([]map[string]any)) != 1 {
		t.Fatalf("%v", snap)
	}
	if ev := q.Journal().Events()["events"].([]map[string]any); len(ev) < 3 {
		t.Fatal(len(ev))
	}
}

func TestStamp(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	if s := Stamp(time.Date(2026, 11, 2, 12, 0, 0, 0, loc)); s != "2026-11-02T12:00:00+0300" {
		t.Fatal(s)
	}
	b, _ := Canonical(map[string]any{"b": 1, "a": []any{"<x>", 2.5}})
	if string(b) != "{\"a\":[\"<x>\",2.5],\"b\":1}\n" {
		t.Fatal(string(b))
	}
}
