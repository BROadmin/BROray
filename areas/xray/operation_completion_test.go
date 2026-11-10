package xray

import (
	"github.com/BROadmin/broray/internal/compat"
	"testing"
)

func TestOperationCompletionReconcilesPreviouslyReadPointer(t *testing.T) {
	h := newHarness(t, false)
	op := h.d.Queue.StartOperation("", "xray_maintenance", "xray:install", "USER", "cooperative", "")
	stale := map[string]any{"backgroundOperationId": op.ID, "running": true, "success": nil}
	final := map[string]any{"backgroundOperationId": op.ID, "running": false, "success": true, "result": map[string]any{"version": "26.11.9"}}
	if err := compat.WriteJSON(h.s.pointerPath(), final); err != nil {
		t.Fatal(err)
	}
	op.Finish(nil)
	if got := h.s.finishedOperationResult(stale, op.Public()); got["success"] != true {
		t.Fatalf("completed result misreported as interrupted: %v", got)
	}
	final["backgroundOperationId"] = "op-20261111000000-1234-abcdefabcdef"
	if err := compat.WriteJSON(h.s.pointerPath(), final); err != nil {
		t.Fatal(err)
	}
	if got := h.s.finishedOperationResult(stale, op.Public()); got["success"] != false || got["backgroundOperationId"] != op.ID {
		t.Fatalf("result from a different operation accepted: %v", got)
	}
}
