package ops

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestReportFailuresFollowOperationAcrossRestart(t *testing.T) {
	a := testArea(t)
	q := a.d.Queue
	ids := []string{"op-q-" + strings.Repeat("a", 32), "op-q-" + strings.Repeat("b", 32), "op-q-" + strings.Repeat("c", 32)}
	one := q.StartOperation(ids[0], "server_operation", compat.ReserveSelectAction, "USER", "protected", "")
	one.Phase("switching")
	one.Finish(errors.Join(errors.New("RESERVE_MODE_ROLLED_BACK"),
		fmt.Errorf("%w: https://private-fixture.example/sub?password=raw-fixture-password", compat.ErrXrayCandidateRejected)))
	two := q.StartOperation(ids[1], "route_operation", "routes:geodata", "SCHEDULER", "protected", "")
	two.Phase("committing")
	two.Finish(errors.New("PRIVATE_FIXTURE_BEARER: UnlabeledPrivateCredential"))
	q.StartOperation(ids[2], "server_operation", compat.ReserveSwitchAction, "SCHEDULER", "protected", "").Finish(nil)
	raw, err := os.ReadFile(a.d.Path("state", "operation-events", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	a.d.Queue = compat.NewCoordinator(a.d)
	rep := call(t, "GET", "/api/operations/report.cgi", "", nil)
	rows, ok := rep.Body["operationFailures"].([]any)
	if rep.Code != 200 || !ok || len(rows) != 2 {
		t.Fatalf("missing durable diagnostic failures: %d %v", rep.Code, rows)
	}
	recent := rows[0].(map[string]any)
	if recent["operationId"] != ids[1] || recent["errorCode"] != "OPERATION_FAILED" ||
		recent["phase"] != "committing" || recent["action"] != "routes:geodata" ||
		recent["errorText"] != "Подробности исходной ошибки недоступны." {
		t.Fatal("second operation misclassified", recent)
	}
	earlier := rows[1].(map[string]any)
	if earlier["operationId"] != ids[0] || earlier["errorCode"] != "XRAY_CANDIDATE_REJECTED" ||
		earlier["phase"] != "switching" || earlier["action"] != compat.ReserveSelectAction ||
		earlier["errorText"] != "Xray отклонил новую конфигурацию." {
		t.Fatal("first operation cause lost", earlier)
	}
	for _, secret := range []string{"private-fixture.example", "raw-fixture-password", "PRIVATE_FIXTURE_BEARER", "UnlabeledPrivateCredential"} {
		if strings.Contains(rep.Raw, secret) || strings.Contains(string(raw), secret) {
			t.Fatal("untrusted failure text reached the journal or report")
		}
	}
	events := call(t, "GET", "/api/operations/events.cgi", "", nil)
	if strings.Contains(events.Raw, "errorText") || strings.Contains(events.Raw, "operationFailures") ||
		!strings.Contains(events.Raw, "Операция завершилась с ошибкой") {
		t.Fatal("events endpoint changed its existing publication contract")
	}
}

func TestReportRollbackEvidence(t *testing.T) {
	a := testArea(t)
	for i, tc := range []struct{ config, vpn bool }{{true, true}, {true, false}, {false, true}} {
		id := fmt.Sprintf("op-q-%032x", i+1)
		op := a.d.Queue.StartOperation(id, "server_operation", compat.ReserveSelectAction, "USER", "protected", "")
		op.RecordRollback(tc.config, tc.vpn)
		op.Finish(compat.ErrXrayCandidateRejected)
	}
	ordinary := a.d.Queue.StartOperation(fmt.Sprintf("op-q-%032x", 4), "server_operation", "servers:activate", "USER", "protected", "")
	original := &compat.XrayConfigRollback{Err: errors.New("https://sensitive-core-fixture.example"), Restored: true}
	ordinary.Finish(compat.PreserveXrayRollback(original, errors.New("public apply failure")))
	a.d.Queue = compat.NewCoordinator(a.d)
	rep := call(t, "GET", "/api/operations/report.cgi", "", nil)
	rows, ok := rep.Body["operationFailures"].([]any)
	if rep.Code != 200 || !ok || len(rows) != 4 {
		t.Fatal("rollback operations unavailable from report")
	}
	ordinaryRow := rows[0].(map[string]any)
	if ordinaryRow["errorCode"] != "XRAY_APPLY_FAILED" || ordinaryRow["configurationRollback"] != "restored" ||
		ordinaryRow["vpnRecovery"] != "unconfirmed" || ordinaryRow["vpnRecoveryText"] != "Восстановление не подтверждено." ||
		ordinaryRow["configurationRollbackText"] != "Предыдущая конфигурация восстановлена." || strings.Contains(rep.Raw, "sensitive-core-fixture") {
		t.Fatal("ordinary apply did not retain safe rollback evidence", ordinaryRow)
	}
	for i, want := range [][2]string{{"unconfirmed", "unconfirmed"}, {"restored", "unconfirmed"}, {"restored", "verified"}} {
		row := rows[i+1].(map[string]any)
		if row["configurationRollback"] != want[0] || row["vpnRecovery"] != want[1] {
			t.Fatalf("rollback facts changed: %v, expected %v", row, want)
		}
		if want[1] == "verified" && row["vpnRecoveryText"] != "Восстановление VPN подтверждено." {
			t.Fatal("verified traffic recovery lacks its explicit label", row)
		}
	}
}
