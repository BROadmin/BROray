package routes

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
)

type geoProbeLease func() error

func (f geoProbeLease) Close() error { return f() }

func TestGeodataAPIPrepareAndApplyAsOneBatch(t *testing.T) {
	for _, mode := range []string{"apply", "verified VPN", "failed VPN", "prepared VPN", "lease close failed", "ordinary VPN rollback", "stale rules", "damaged payload", "upstream reverted", "superseded receipt", "apply rollback"} {
		t.Run(mode, func(t *testing.T) {
			env, _ := geoAPIEnv(t)
			env.e.geoNow = func() time.Time { return time.Date(2026, 11, 24, 10, 12, 0, 0, time.UTC) }
			upstreamChanged := false
			fetched := map[geodata.Kind]int{}
			env.e.geoFetch = func(_ context.Context, source geodata.Source) ([]byte, error) {
				fetched[source.Kind]++
				if source.Kind == geodata.GeoIP {
					address := byte(2)
					if upstreamChanged {
						address = 3
					}
					return []byte{0x0a, 0x0e, 0x0a, 0x02, 'F', 'I', 0x12, 0x08, 0x0a, 0x04, 192, 0, address, 0, 0x10, 24}, nil
				}
				domain := byte('a')
				if upstreamChanged {
					domain = 'b'
				}
				return []byte{0x0a, 0x0e, 0x0a, 0x07, 'y', 'o', 'u', 't', 'u', 'b', 'e', 0x12, 0x03, 0x12, 0x01, domain}, nil
			}
			path := env.d.Root.XrayConfig()
			if err := os.WriteFile(path, []byte(policyTestBase), 0600); err != nil {
				t.Fatal(err)
			}
			env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) {
				return env.e.configureRouting(ctx, []byte(policyTestBase))
			}
			applies := 0
			env.d.Svc.XrayApplyConfig = func(_ context.Context, cfg []byte) error {
				applies++
				return platform.WriteFileAtomicNoFollow(path, cfg, 0600)
			}
			geoMustComplete(t, geoSubmit(env, "geo-upsert", "own", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
			geoMustComplete(t, geoSubmit(env, "geo-upsert", "site", `,"name":"YouTube","kind":"geosite","tags":["youtube"]`))
			for _, id := range []string{"own", "site"} {
				revision := policyView(env)["revision"].(string)
				env.mustOK(env.callH("POST", "/api/routes/policy.cgi", `{"expectedRevision":"`+revision+`","operation":"enabled","id":"`+id+`","enabled":true}`, policyHeaders))
			}
			before := geoLoad(t, env)
			beforePolicy := policyView(env)["revision"]
			beforeApplies := applies
			probes := 0
			prior, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "prepared VPN" || mode == "lease close failed" || mode == "ordinary VPN rollback" {
				preparations, releases := 0, 0
				var candidate []byte
				env.d.Svc.ServersPreparePolicyVPN = func(ctx context.Context, cfg []byte) ([]byte, io.Closer) {
					if !compat.FenceHeld(ctx) {
						t.Fatal("lease created outside the policy fence")
					}
					preparations++
					candidate = append(append([]byte(nil), cfg...), '\n')
					return candidate, geoProbeLease(func() error {
						releases++
						if mode == "lease close failed" {
							return os.ErrInvalid
						}
						return nil
					})
				}
				env.d.Svc.XrayApplyConfig = func(_ context.Context, cfg []byte) error {
					applies++
					if releases != 1 {
						t.Fatal("config applied while lease held")
					}
					want := candidate
					if applies > beforeApplies+1 {
						want = prior
					}
					if string(cfg) != string(want) {
						t.Fatal("applied config differs from prepared bytes or exact predecessor")
					}
					return platform.WriteFileAtomicNoFollow(path, cfg, 0600)
				}
				env.d.Svc.ServersProbePolicyVPN = func(_ context.Context, cfg []byte) compat.VPNCheck {
					probes++
					got, err := os.ReadFile(path)
					if err != nil || string(got) != string(cfg) {
						t.Fatalf("checked config differs from installed bytes: %v", err)
					}
					if mode == "ordinary VPN rollback" {
						if probes == 1 {
							return compat.VPNFailed
						}
						return compat.VPNUnconfirmed
					}
					return compat.VPNVerified
				}
				defer func() {
					wantApplies, wantProofs := 1, 1
					switch mode {
					case "lease close failed":
						wantApplies, wantProofs = 0, 0
					case "ordinary VPN rollback":
						wantApplies, wantProofs = 2, 2
					}
					if preparations != 1 || releases != 1 || probes != wantProofs || applies != beforeApplies+wantApplies {
						t.Errorf("prepare=%d release=%d proof=%d apply=%d", preparations, releases, probes, applies-beforeApplies)
					}
				}()
			}
			upstreamChanged = true
			env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return strings.Repeat("b", 40), nil }
			geoMustComplete(t, geoSubmit(env, "geo-check", "geodata", ""))
			view := policyView(env)
			update, ok := view["geoPending"].(map[string]any)
			if !ok || update["revision"] == "" || applies != beforeApplies || geoLoad(t, env).Revision != before.Revision || view["revision"] != beforePolicy {
				t.Fatalf("preparation changed live state or pending update missing: %v", update)
			}
			preparedRevision := update["revision"].(string)
			if fetched[geodata.GeoIP] != 2 || fetched[geodata.GeoSite] != 2 {
				t.Fatalf("both changed inputs must be prepared together: %v", fetched)
			}
			if mode == "stale rules" {
				env.mustOK(policyPost(env, view["revision"].(string), "enabled", `,"enabled":false`))
				beforeApplies = applies
			}
			if mode == "damaged payload" {
				root := filepath.Join(env.d.Root.Runtime(), geodata.AssetDirectory)
				entries, err := filepath.Glob(filepath.Join(root, "gen-*", "snapshot.json"))
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, entry := range entries {
					data, err := os.ReadFile(entry)
					if err != nil {
						t.Fatal(err)
					}
					var snapshot geodata.Snapshot
					if err := json.Unmarshal(data, &snapshot); err != nil {
						t.Fatal(err)
					}
					if snapshot.Revision == preparedRevision {
						if err := os.WriteFile(filepath.Join(root, snapshot.Assets[0].Path), []byte("damaged"), 0600); err != nil {
							t.Fatal(err)
						}
						found = true
						break
					}
				}
				if !found {
					t.Fatal("did not find prepared generation")
				}
			}
			if mode == "upstream reverted" {
				env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return strings.Repeat("a", 40), nil }
				geoMustComplete(t, geoSubmit(env, "geo-check", "geodata", ""))
				if policyView(env)["geoPending"] != nil {
					t.Fatal("current data revalidated, but older differing batch is still ready")
				}
			}
			if mode == "superseded receipt" {
				env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return strings.Repeat("c", 40), nil }
				geoMustComplete(t, geoSubmit(env, "geo-check", "geodata", ""))
				if policyView(env)["geoPending"].(map[string]any)["revision"] == preparedRevision {
					t.Fatal("new check did not supersede the receipt")
				}
			}
			if mode == "apply rollback" {
				workingConfig, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				env.d.Svc.XrayApplyConfig = func(_ context.Context, cfg []byte) error {
					applies++
					if err := platform.WriteFileAtomicNoFollow(path, cfg, 0600); err != nil {
						return err
					}
					if applies == beforeApplies+1 {
						return os.ErrInvalid
					}
					return nil
				}
				request := geoSubmit(env, "geo-apply", "geodata", `,"expectedPendingRevision":"`+preparedRevision+`"`)
				restored, err := os.ReadFile(path)
				if request.State != "failed" || request.ErrorCode != "POLICY_APPLY_FAILED" || err != nil ||
					string(restored) != string(workingConfig) || geoLoad(t, env).Revision != before.Revision || applies != beforeApplies+2 {
					t.Fatalf("failed batch did not restore working config: %v", request)
				}
				if policyView(env)["geoPending"].(map[string]any)["revision"] != preparedRevision {
					t.Fatal("confirmed rollback lost the prepared batch")
				}
				return
			}
			suffix := `,"expectedPendingRevision":"` + preparedRevision + `"`
			if mode == "verified VPN" || mode == "failed VPN" {
				env.d.Svc.ServersProbePolicyVPN = func(ctx context.Context, cfg []byte) compat.VPNCheck {
					probes++
					current, err := os.ReadFile(path)
					if err != nil || string(current) != string(cfg) || !compat.FenceHeld(ctx) {
						t.Fatal("VPN proof did not use the actual fenced configuration")
					}
					if mode == "failed VPN" && probes == 1 {
						return compat.VPNFailed
					}
					return compat.VPNVerified
				}
			}
			request := geoSubmit(env, "geo-apply", "geodata", suffix)
			after := geoLoad(t, env)
			if mode == "lease close failed" || mode == "ordinary VPN rollback" {
				want := "POLICY_APPLY_FAILED"
				if mode == "ordinary VPN rollback" {
					want = "GEODATA_VPN_CHECK_FAILED"
					failures := env.d.Queue.Journal().ReportEvents()["failures"].([]map[string]any)
					if len(failures) == 0 || failures[0]["vpnRecovery"] != "unconfirmed" || failures[0]["configurationRollback"] != "restored" {
						t.Fatalf("expected exact rollback with unconfirmed VPN: %+v", failures)
					}
				}
				working, err := os.ReadFile(path)
				if request.State != "failed" || request.ErrorCode != want || after.Revision != before.Revision ||
					err != nil || string(working) != string(prior) {
					t.Fatalf("failed phase did not keep previous working configuration and Geo: %+v", request)
				}
				return
			}
			if mode == "failed VPN" {
				if request.State != "failed" || request.ErrorCode != "GEODATA_VPN_CHECK_FAILED" || after.Revision != before.Revision ||
					probes != 2 || applies != beforeApplies+2 {
					t.Fatal("bound negative probe did not trigger verified rollback")
				}
				failures := env.d.Queue.Journal().ReportEvents()["failures"].([]map[string]any)
				if len(failures) == 0 || failures[0]["vpnRecovery"] != "verified" || failures[0]["configurationRollback"] != "restored" {
					t.Fatalf("restored files and VPN were not independently reported: %+v", failures)
				}
				return
			}
			if mode != "apply" && mode != "verified VPN" && mode != "prepared VPN" {
				if request.State != "failed" || after.Revision != before.Revision || applies != beforeApplies {
					t.Fatal("stale or damaged batch mutated active state")
				}
				return
			}
			geoMustComplete(t, request)
			if applies != beforeApplies+1 || after.Revision != preparedRevision || len(after.Assets) != 2 || policyView(env)["geoPending"] != nil {
				t.Fatal("batch was not applied once and consumed")
			}
			// The fixture restarts through a mock, so Xray readiness alone
			// cannot report a successful VPN payload check.
			wantProof := string(compat.VPNUnconfirmed)
			if mode == "verified VPN" || mode == "prepared VPN" {
				wantProof = string(compat.VPNVerified)
				if probes != 1 {
					t.Fatal("successful apply did not check VPN once")
				}
			}
			events := env.d.Queue.Journal().ReportEvents()["events"].([]map[string]any)
			var reasons, checks int
			for _, event := range events {
				if event["operationId"] != request.OperationID {
					continue
				}
				switch event["event"] {
				case "geodata_xray_apply":
					if event["restartReason"] == "geodata-update" {
						reasons++
					}
				case "geodata_vpn_check":
					if event["vpnCheck"] == wantProof {
						checks++
					}
				}
			}
			if reasons != 1 || checks != 1 {
				t.Fatalf("missing bounded geodata operation evidence: reasons=%d checks=%d", reasons, checks)
			}
			for _, asset := range after.Assets {
				if asset.Source.Revision != strings.Repeat("b", 40) {
					t.Fatal("mixed data from two checks")
				}
			}
			duplicate := geoSubmit(env, "geo-apply", "geodata", suffix)
			if duplicate.State != "failed" || applies != beforeApplies+1 {
				t.Fatal("duplicate request caused another apply")
			}
		})
	}
}
