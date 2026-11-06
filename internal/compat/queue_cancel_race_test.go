package compat

import (
	"context"
	"testing"
)

// A status reader may cancel as soon as the queue exposes a running request,
// including before the worker has entered execute.
func TestRunningRequestIsCancellableBeforeWorkerExecute(t *testing.T) {
	c := testDeps(t).Queue
	c.Define(ActionSpec{Name: "servers:quality", Type: "server_operation", Priority: map[string]int{"USER": 2},
		Stages: []Stage{{Name: "probe", Run: func(*Task) error { return nil }}}})
	rq, _, err := c.Submit("servers:quality", "a", "USER", "ctx", RandHex(16))
	if err != nil {
		t.Fatal(err)
	}
	c.next()
	running, ok := c.Request(rq.RequestID)
	if !ok || running.State != "running" || running.OperationID == "" {
		t.Fatalf("request was not exposed as running: %+v %v", running, ok)
	}
	out, err := c.CancelOperation(running.OperationID)
	if err != nil || out["ok"] != true || out["cancelRequested"] != true {
		t.Fatalf("visible running operation cancellation: %v %v", out, err)
	}
}

// Cancellation can be accepted after the operation is registered but before
// its first cooperative stage has attached a context.CancelFunc.
func TestAcceptedCancelBeforeStageBindIsDelivered(t *testing.T) {
	c := testDeps(t).Queue
	id := "q-" + RandHex(16)
	opID := "op-" + id
	c.mu.Lock()
	c.running = &Request{RequestID: id, OperationID: opID, State: "running"}
	c.mu.Unlock()
	op := c.StartOperation(opID, "server_operation", "servers:quality", "USER", "cooperative", id)
	out, err := c.CancelOperation(opID)
	if err != nil || out["cancelRequested"] != true {
		t.Fatalf("cancellation not accepted: %v %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.bindCooperativeCancel(op, cancel)
	if ctx.Err() != context.Canceled {
		t.Fatalf("accepted cancellation lost before stage bind: %v", ctx.Err())
	}
}
