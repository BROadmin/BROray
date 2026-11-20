package servers

import (
	"bytes"
	"encoding/json"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"os"
	"strings"
	"testing"
)

func TestDPIQueueAdmissionAndStaleRevision(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	guard, e := platform.TryLock(f.e.d.Root.DaemonLock())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { guard.Release() })
	f.e.d.DaemonGuard = guard
	f.release()
	if e = f.e.d.Lock.ReconcileDaemonReserve(f.e.d.Root, guard, false, nil); e != nil {
		t.Fatal(e)
	}
	id := f.plan.node.ID
	before, e := os.ReadFile(RecordPath(f.e.d, id))
	if e != nil {
		t.Fatal(e)
	}
	snap := f.process.RuntimeSnapshot()
	headers := map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"}
	body := `{"id":"` + id + `","enabled":true,"expectedRevision":"` + strings.Repeat("0", 64) + `","nonce":"` + strings.Repeat("a", 32) + `"}`
	for _, raw := range []string{strings.Replace(body, `"enabled":true`, `"enabled":null`, 1), strings.Replace(body, `"enabled":true`, `"enabled":true,"enabled":false`, 1), strings.Replace(body, `"id":"`+id+`"`, `"id":"../outside"`, 1), strings.Replace(body, strings.Repeat("a", 32), strings.Repeat("g", 32), 1), strings.Replace(body, `"enabled":true`, `"extra":1,"enabled":true`, 1)} {
		response := call(t, "POST", "http://example.com/api/servers/dpi-profile.cgi", raw, headers)
		if response.Code != 400 {
			t.Fatal("unsafe request", response.Code, response.Raw)
		}
	}
	if response := call(t, "POST", "http://example.com/api/servers/dpi-profile.cgi", body, nil); response.Code == 202 {
		t.Fatal("missing operations authority accepted")
	}
	for i := 0; i < 2; i++ {
		response := call(t, "POST", "http://example.com/api/servers/dpi-profile.cgi", body, headers)
		if response.Code != 202 {
			t.Fatal(response.Code, response.Raw)
		}
	}
	row, ok := f.e.d.Queue.FindLive(compat.DPIProfileAction, compat.ReserveOperationTarget)
	if !ok || row.RequestID != "q-"+strings.Repeat("a", 32) {
		t.Fatal("dedicated queue receipt", row)
	}
	if snap != f.process.RuntimeSnapshot() {
		t.Fatal("handler mutated main")
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	waitReq(t, f.e, row.RequestID, "failed")
	if f.e.d.Lock.Busy() {
		t.Fatal("stale request retained owner")
	}
	after, e := os.ReadFile(RecordPath(f.e.d, id))
	if e != nil || !bytes.Equal(before, after) || snap != f.process.RuntimeSnapshot() {
		t.Fatal("stale changed images", e)
	}
	response := call(t, "GET", "http://example.com/api/servers/dpi-status.cgi?id="+id, "", nil)
	if response.Code != 200 {
		t.Fatal(response.Raw)
	}
	var data map[string]any
	if e = json.Unmarshal([]byte(response.Raw), &data); e != nil {
		t.Fatal(e)
	}
	if v, ok := data["data"].(map[string]any); ok {
		data = v
	}
	if data["revision"] != compat.SHA256Hex(before) || data["enabled"] != false || data["recoveryRequired"] != false {
		t.Fatal("readback", data)
	}
}

func TestDPIQueueCoreFailureReason(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	guard, err := platform.TryLock(f.e.d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guard.Release() })
	f.e.d.DaemonGuard = guard
	f.release()
	if err = f.e.d.Lock.ReconcileDaemonReserve(f.e.d.Root, guard, false, nil); err != nil {
		t.Fatal(err)
	}
	core := f.e.d.Root.XrayBinary()
	if err = os.Rename(core, core+".original"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(core, []byte("#!/bin/sh\necho 'Xray 26.1.1 (fixture)'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	id := f.plan.node.ID
	before, err := os.ReadFile(RecordPath(f.e.d, id))
	if err != nil {
		t.Fatal(err)
	}
	snap := f.process.RuntimeSnapshot()
	body := `{"id":"` + id + `","enabled":true,"expectedRevision":"` + compat.SHA256Hex(before) + `","nonce":"` + strings.Repeat("c", 32) + `"}`
	response := call(t, "POST", "http://example.com/api/servers/dpi-profile.cgi", body, map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"})
	if response.Code != 202 {
		t.Fatal(response.Code, response.Raw)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	requestID := "q-" + strings.Repeat("c", 32)
	waitReq(t, f.e, requestID, "failed")
	row, ok := f.e.d.Queue.Request(requestID)
	if !ok || row.ErrorCode != "XRAY_DPI_CORE_UNVERIFIED" || !strings.Contains(row.Message, "26.11.9") {
		t.Fatalf("lost actionable failure reason: %+v", row)
	}
	after, err := os.ReadFile(RecordPath(f.e.d, id))
	if err != nil || !bytes.Equal(before, after) || snap != f.process.RuntimeSnapshot() || f.e.d.Lock.Busy() {
		t.Fatal("rejected core changed state", err)
	}
}
