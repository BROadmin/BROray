package routes

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeleteStopAfterVerifiedBatchResumes(t *testing.T) {
	env := newEnv(t)
	verifyBatchSize = 4
	env.installBundle("telegram")
	before := env.r.routeCount("Proxy0")
	token := env.preflightToken("telegram", "delete")
	requested := false
	var stop, stale, beforeStatus, duringStatus resp
	var stoppedOperationID string
	env.r.failCmd = func(cmd string, _ int) bool {
		if requested || !strings.HasPrefix(cmd, "no ip route ") {
			return false
		}
		requested = true
		p, ok := readObj(env.e.progressFile("telegram"))
		if !ok {
			t.Fatal("running progress missing")
		}
		stoppedOperationID = str(p, "backgroundOperationId")
		if stoppedOperationID == "" {
			t.Fatal("running operation ID missing")
		}
		beforeStatus = env.get("/api/routes/operation-status.cgi")
		stale = env.post("/api/routes/stop.cgi?bundleId=telegram&operationId=stale")
		stop = env.post("/api/routes/stop.cgi?bundleId=telegram&operationId=" + url.QueryEscape(stoppedOperationID))
		duringStatus = env.get("/api/routes/operation-status.cgi")
		return false
	}
	result := env.mustOK(env.post("/api/routes/delete.cgi?bundleId=telegram&preflightToken=" + token))
	if !requested || stop.status != 202 {
		t.Fatalf("stop request status=%d body=%s", stop.status, stop.raw)
	}
	if stale.status != 409 || stale.code() != "ROUTES_STOP_NOT_RUNNING" {
		t.Fatalf("stale operation was accepted: %d %s", stale.status, stale.raw)
	}
	if beforeStatus.data()["canStop"] != true || duringStatus.data()["canStop"] != true ||
		duringStatus.data()["progress"].(map[string]any)["stopRequested"] != true {
		t.Fatalf("stop availability/status not projected: before=%s during=%s", beforeStatus.raw, duringStatus.raw)
	}
	progress := result["operationProgress"].(map[string]any)
	if progress["phase"] != "paused" || progress["resumable"] != true || progress["stoppedByUser"] != true || progress["current"] != float64(4) {
		t.Fatalf("delete not paused after verified batch: %v", progress)
	}
	if got := env.r.routeCount("Proxy0"); got != before-4 {
		t.Fatalf("router routes=%d, want %d", got, before-4)
	}
	if !env.d.Lock.Busy() {
		t.Fatal("paused route operation lost its fence")
	}
	env.r.failCmd = nil
	resume := env.preflightToken("telegram", "resume")
	env.mustOK(env.post("/api/routes/resume.cgi?bundleId=telegram&preflightToken=" + resume))
	if env.r.routeCount("Proxy0") != 0 || env.d.Lock.Busy() {
		t.Fatal("resumed deletion did not finish and release the fence")
	}
	env.wantErr(env.post("/api/routes/stop.cgi?bundleId=telegram&operationId="+url.QueryEscape(stoppedOperationID)), 409, "ROUTES_STOP_NOT_RUNNING")
}

func TestStopRefusesIdleAndMissingOperation(t *testing.T) {
	env := newEnv(t)
	env.wantErr(env.post("/api/routes/stop.cgi?bundleId=telegram"), 400, "ROUTES_OPERATION_ID_REQUIRED")
	env.wantErr(env.post("/api/routes/stop.cgi?bundleId=telegram&operationId=stale"), 409, "ROUTES_STOP_NOT_RUNNING")
	if env.r.routeCount("Proxy0") != 0 {
		t.Fatal("idle stop changed router routes")
	}
}

func TestExportStopRollsBackAndResumes(t *testing.T) {
	env := newEnv(t)
	verifyBatchSize = 4
	baseTime := time.Date(2026, 11, 4, 13, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return baseTime }
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	token := env.preflightToken("telegram", "export")
	beforeRouter := strings.Join(env.r.snapshot(), "\n")
	beforeState, err := os.ReadFile(env.e.stateFile("telegram"))
	if err != nil {
		t.Fatal(err)
	}
	nowFn = func() time.Time { return baseTime.Add(2 * time.Second) }
	requested := false
	var stop resp
	env.r.failCmd = func(cmd string, _ int) bool {
		if requested || !strings.HasPrefix(cmd, "ip route ") {
			return false
		}
		requested = true
		p, ok := readObj(env.e.progressFile("telegram"))
		if !ok {
			t.Fatal("running progress missing")
		}
		opID := str(p, "backgroundOperationId")
		if opID == "" {
			t.Fatal("running operation ID missing")
		}
		stop = env.post("/api/routes/stop.cgi?bundleId=telegram&operationId=" + url.QueryEscape(opID))
		return false
	}
	result := env.mustOK(env.post("/api/routes/export.cgi?bundleId=telegram&preflightToken=" + token))
	if !requested || stop.status != 202 {
		t.Fatalf("stop request status=%d body=%s", stop.status, stop.raw)
	}
	progress := result["operationProgress"].(map[string]any)
	if progress["phase"] != "paused" || progress["resumable"] != true || progress["stoppedByUser"] != true || progress["current"] != float64(0) {
		t.Fatalf("export did not roll back to resumable baseline: %v", progress)
	}
	afterState, err := os.ReadFile(env.e.stateFile("telegram"))
	if err != nil {
		t.Fatal(err)
	}
	if afterRouter := strings.Join(env.r.snapshot(), "\n"); afterRouter != beforeRouter {
		t.Fatalf("stopped export changed router: before bytes=%d after bytes=%d", len(beforeRouter), len(afterRouter))
	}
	if !bytes.Equal(afterState, beforeState) {
		first := 0
		for first < len(beforeState) && first < len(afterState) && beforeState[first] == afterState[first] {
			first++
		}
		lo := max(0, first-32)
		t.Fatalf("stopped export changed local state at byte %d: before=%q after=%q", first,
			beforeState[lo:min(len(beforeState), first+64)], afterState[lo:min(len(afterState), first+64)])
	}
	if !env.d.Lock.Busy() {
		t.Fatal("paused export lost its fence")
	}
	env.r.failCmd = nil
	resume := env.preflightToken("telegram", "resume")
	state := env.mustOK(env.post("/api/routes/resume.cgi?bundleId=telegram&preflightToken=" + resume))
	if state["status"] != "installed" || env.d.Lock.Busy() {
		t.Fatal("resumed export did not finish and release the fence")
	}
}

func TestExportStopRollbackFailureBlocksWrites(t *testing.T) {
	env := newEnv(t)
	verifyBatchSize = 4
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	token := env.preflightToken("telegram", "export")
	requested := false
	var stop resp
	env.r.failCmd = func(cmd string, _ int) bool {
		if strings.HasPrefix(cmd, "ip route ") && !requested {
			requested = true
			p, ok := readObj(env.e.progressFile("telegram"))
			if !ok {
				t.Fatal("running progress missing")
			}
			stop = env.post("/api/routes/stop.cgi?bundleId=telegram&operationId=" + url.QueryEscape(str(p, "backgroundOperationId")))
			return false
		}
		return requested && strings.HasPrefix(cmd, "no ip route ")
	}
	result := env.post("/api/routes/export.cgi?bundleId=telegram&preflightToken=" + token)
	env.wantErr(result, 502, "ROUTES_EXPORT_FAILED")
	if !requested || stop.status != 202 || !env.e.rollbackBlocked() {
		t.Fatalf("failed rollback was not blocked: requested=%t stop=%d result=%s", requested, stop.status, result.raw)
	}
	progress := env.mustOK(env.get("/api/routes/progress.cgi?bundleId=telegram"))
	if progress["resumable"] == true || progress["phase"] == "paused" || progress["rolledBack"] == true {
		t.Fatalf("failed rollback reported as a safe pause: %v", progress)
	}
}

func TestRollbackMarkerWriteFailureStaysBlocked(t *testing.T) {
	env := newEnv(t)
	env.e.markerWrite = func(string, any) error { return errors.New("injected marker failure") }
	if err := env.e.writeRollbackMarker("transaction", "rollback uncertain"); err == nil {
		t.Fatal("marker writer failure was swallowed")
	}
	if fileExists(env.e.rollbackMarker()) {
		t.Fatal("marker unexpectedly exists in injected failure case")
	}
	if !env.e.rollbackBlocked() {
		t.Fatal("failed marker publication allowed further route writes")
	}
}

func TestUnfinishedRouteTransactionBlocksAfterRestart(t *testing.T) {
	env := newEnv(t)
	file := filepath.Join(env.e.p("transactions", "sync-telegram-proof"), "transaction.json")
	if err := writeObj(file, obj{"schemaVersion": 2, "operation": "sync", "bundleId": "telegram", "phase": "planned"}); err != nil {
		t.Fatal(err)
	}
	restarted := newEngine(env.d)
	if !restarted.rollbackBlocked() {
		t.Fatal("unfinished durable transaction was ignored after restart")
	}
}

func TestMissingRouteTransactionRecordWithBackupBlocksAfterRestart(t *testing.T) {
	env := newEnv(t)
	txDir := env.e.p("transactions", "sync-telegram-lost-record")
	backup := filepath.Join(txDir, "original", "routes.bat")
	if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("original routes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !newEngine(env.d).rollbackBlocked() {
		t.Fatal("transaction backup without journal allowed route writes after restart")
	}
}

func TestEmptyRouteTransactionDirectoryDoesNotBlockAfterRestart(t *testing.T) {
	env := newEnv(t)
	if err := os.MkdirAll(env.e.p("transactions", "sync-telegram-empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if newEngine(env.d).rollbackBlocked() {
		t.Fatal("empty transaction directory blocked route writes")
	}
}

func TestExportStopMarkerFailureRetainsLeaseAndRestartFence(t *testing.T) {
	env := newEnv(t)
	verifyBatchSize = 4
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	token := env.preflightToken("telegram", "export")
	markerAttempts := 0
	env.e.markerWrite = func(string, any) error {
		markerAttempts++
		return errors.New("injected marker failure")
	}
	requested := false
	env.r.failCmd = func(cmd string, _ int) bool {
		if strings.HasPrefix(cmd, "ip route ") && !requested {
			requested = true
			p, ok := readObj(env.e.progressFile("telegram"))
			if !ok {
				t.Fatal("running progress missing")
			}
			env.post("/api/routes/stop.cgi?bundleId=telegram&operationId=" + url.QueryEscape(str(p, "backgroundOperationId")))
			return false
		}
		return requested && strings.HasPrefix(cmd, "no ip route ")
	}
	result := env.post("/api/routes/export.cgi?bundleId=telegram&preflightToken=" + token)
	env.wantErr(result, 502, "ROUTES_EXPORT_FAILED")
	if !requested || markerAttempts != 1 || fileExists(env.e.rollbackMarker()) {
		t.Fatalf("marker failure not exercised exactly once: stop=%t writes=%d marker=%t response=%s", requested, markerAttempts, fileExists(env.e.rollbackMarker()), result.raw)
	}
	if h, ok := env.d.Lock.Current(); !ok || !h.Pending || h.Resumable || !env.d.Lock.Busy() {
		t.Fatalf("uncertain rollback lost its non-resumable global lease: holder=%+v ok=%t", h, ok)
	}
	if !newEngine(env.d).rollbackBlocked() {
		t.Fatal("restart did not recognize unfinished rollback transaction")
	}
}

func TestTerminalRouteTransactionsDoNotBlockRestart(t *testing.T) {
	env := newEnv(t)
	for _, phase := range []string{"committed", "paused", "rolled_back", "resume_attempt_rolled_back"} {
		file := filepath.Join(env.e.p("transactions", "sync-telegram-"+phase), "transaction.json")
		if err := writeObj(file, obj{"schemaVersion": 2, "operation": "sync", "bundleId": "telegram", "phase": phase}); err != nil {
			t.Fatal(err)
		}
	}
	if newEngine(env.d).rollbackBlocked() {
		t.Fatal("proven terminal transactions blocked restart")
	}
}

func TestPreexistingSameBundleTransactionIsNotTreatedAsLive(t *testing.T) {
	env := newEnv(t)
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	token := env.preflightToken("telegram", "export")
	file := filepath.Join(env.e.p("transactions", "sync-telegram-older"), "transaction.json")
	if err := writeObj(file, obj{"schemaVersion": 2, "operation": "sync", "bundleId": "telegram", "phase": "planned"}); err != nil {
		t.Fatal(err)
	}
	env.wantErr(env.post("/api/routes/export.cgi?bundleId=telegram&preflightToken="+token), 502, "ROUTES_EXPORT_FAILED")
	if env.r.routeCount("Proxy0") != 0 || !newEngine(env.d).rollbackBlocked() {
		t.Fatal("old transaction was bypassed or router was changed")
	}
}
