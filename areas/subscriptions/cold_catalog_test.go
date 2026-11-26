package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

// Queue, catalog, registered server providers, supervisor and native config
// validation are real. The subscription HTTP server and owned router interface
// are fixtures; this is not a physical-router or online-proxy health test.
func TestColdCatalogNativeOwnerAcceptance(t *testing.T) {
	for _, kind := range []string{"refresh-removed", "refresh-same-id", "refresh-rename", "refresh-pending", "delete-replace", "delete-withdraw", "keep-active"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			cfg := platform.DefaultConfig()
			cfg.Keenetic.RCIURL = "http://127.0.0.1:1"
			cfg.Keenetic.WriteTransport = "rci"
			e.d.App = app.New(e.d.Root, cfg, e.d.Log)
			e.d.DaemonGuard = e.daemonGuard
			core, err := os.ReadFile(os.Getenv("BRORAY_XRAY_BIN"))
			must(err)
			must(os.WriteFile(e.d.Root.XrayBinary(), core, 0700))
			// Replace service-boundary doubles with actual registered areas. The
			// existing maintenance setting prevents timers or background starts.
			must(compat.StartAreas(e.d))
			e.s = cur()
			settings := e.d.Path("config", "system", "settings.json")
			must(os.MkdirAll(filepath.Dir(settings), 0700))
			must(compat.WriteJSON(settings, map[string]any{"listenAddress": "127.0.0.1", "socksPort": 2080, "logLevel": "none"}))
			description := "BROray - A"
			e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
				rec, err := servers.ReadRecord(e.d, servers.ActiveID(e.d))
				if err != nil {
					return "", err
				}
				name, ok := rec["name"].(string)
				if !ok || name == "" {
					return "", errors.New("fixture name absent")
				}
				return "BROray - " + name, nil
			}
			e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) { return "BROray - " + name, nil }
			e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if !compat.FenceHeld(ctx) || want != description {
					return compat.OwnedInterfaceTarget{}, errors.New("fixture interface owner mismatch")
				}
				return compat.OwnedInterfaceTarget{Name: "Proxy0", Upstream: netip.MustParseAddrPort("127.0.0.1:2080"), DescriptionLine: fmt.Sprintf("    description %q", description)}, nil
			}
			e.d.Svc.KeeneticVerifyInterfaceTarget = func(context.Context, string) (compat.OwnedInterfaceTarget, error) {
				return compat.OwnedInterfaceTarget{}, errors.New("cold core has no live listener")
			}
			e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
				var err error
				description, err = e.d.Svc.KeeneticOrdinaryDescription()
				return err
			}
			e.d.Svc.XrayValidateConfig = func(ctx context.Context, raw []byte) error {
				path := filepath.Join(t.TempDir(), "native-candidate.json")
				if err := os.WriteFile(path, raw, 0600); err != nil {
					return err
				}
				return xray.Test(ctx, e.d.Root.XrayBinary(), path)
			}
			must(os.WriteFile(e.d.Path("config", "interface.json"), []byte(`{"fixture":"owned-interface"}`), 0600))
			provider := newProvider(t, "trojan://pw@127.0.0.1:1443?security=tls&sni=localhost#A\ntrojan://pw@127.0.0.1:2443?security=tls&sni=localhost#B\n")
			sub := e.create(t, provider.URL+"/source", "")
			must(e.refreshSync(t, sub))
			active, next := "", ""
			for _, f := range e.s.liveServers() {
				if f.rec["name"] == "A" {
					active = f.id
				}
				if f.rec["name"] == "B" {
					next = f.id
				}
			}
			if active == "" || next == "" {
				t.Fatal("source fixture incomplete")
			}
			must(e.s.writeActive(active))
			rec, err := servers.ReadRecord(e.d, active)
			must(err)
			config, err := servers.XrayConfig(rec, servers.GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "none"}, "cold catalog fixture")
			must(err)
			if kind == "refresh-rename" {
				config = append(config, '\n')
			}
			must(os.WriteFile(e.d.Root.XrayConfig(), config, 0600))
			if kind == "delete-replace" || kind == "keep-active" {
				manual, err := servers.ParseLink("trojan://pw@127.0.0.1:3443?security=tls&sni=localhost#C", "manual-c", nil)
				must(err)
				must(servers.WriteRecord(e.d, manual))
			}
			p := e.d.App.Xray
			before := p.RuntimeSnapshot()
			if !before.ColdStopped() {
				t.Fatal("fixture supervisor was launched", before)
			}
			raw, err := os.ReadFile(servers.RecordPath(e.d, active))
			must(err)
			hold, err := json.Marshal(map[string]any{"schemaVersion": 1, "operation": strings.Repeat("a", 64), "serverId": active, "recordSHA256": compat.SHA256Hex(raw), "configSHA256": compat.SHA256Hex(config)})
			must(err)
			holdPath := filepath.Join(e.d.Root.Config(), reserve.ProfileBootHoldFile)
			must(os.WriteFile(holdPath, hold, 0600))
			if kind == "keep-active" {
				ids, err := servers.RecordIDs(e.d)
				must(err)
				for _, id := range ids {
					path := servers.QualityPath(e.d, id)
					must(os.MkdirAll(filepath.Dir(path), 0700))
					must(os.WriteFile(path, []byte(`{"status":"available"}`), 0600))
				}
				body, err := json.Marshal(map[string]string{"activeId": active})
				must(err)
				r := call(t, "POST", "/api/servers/delete-inactive.cgi", string(body), queueHeaders())
				if r.Status != 200 {
					t.Fatal("keep active", r.Status, r.Raw)
				}
			} else {

				if kind == "refresh-pending" {
					actual := e.d.Svc.ServersPrepareSourceRefresh
					e.d.Svc.ServersPrepareSourceRefresh = func(ctx context.Context, source string, removed, candidates []string) (string, error) {
						next, err := actual(ctx, source, removed, candidates)
						if err != nil {
							return next, err
						}
						return next, errors.Join(compat.ErrReserveTransactionPending, errors.New("synthetic uncertainty after actual cold selection"))
					}
				}
				nonce := strings.Repeat("de", 16)
				var reply resp
				if strings.HasPrefix(kind, "refresh-") {
					payload := "trojan://pw@127.0.0.1:2443?security=tls&sni=localhost#B\n"
					if kind == "refresh-same-id" {
						payload = "trojan://rotated@127.0.0.1:1443?security=tls&sni=localhost#A\n" + payload
					}
					if kind == "refresh-rename" {
						payload = "trojan://pw@127.0.0.1:1443?security=tls&sni=localhost#Renamed\n" + payload
					}
					provider.set(payload)
					body, err := json.Marshal(map[string]string{"id": sub, "nonce": nonce})
					must(err)
					reply = call(t, "POST", "/api/subscriptions/refresh.cgi", string(body), queueHeaders())
				} else {
					reply = submitSourceRemoval(t, sub, nonce)
				}
				if reply.Status != 202 {
					t.Fatal("queued request rejected", reply.Status, reply.Raw)
				}
				stop := e.startWorker()
				defer stop()
				waitFor(t, "native cold catalog request", func() bool {
					q, _ := e.d.Queue.Request("q-" + nonce)
					return q.State == "completed" || q.State == "failed"
				})
				q, _ := e.d.Queue.Request("q-" + nonce)

				if kind == "refresh-pending" {
					if q.State != "failed" || q.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
						t.Fatal("uncertain operation not retained", q.State, q.ErrorCode)
					}
					holder, held := e.d.Lock.Current()
					if !held || !holder.Pending || holder.Resumable || holder.Action != compat.SourceRefreshAction {
						t.Fatal("uncertain global owner lost", holder)
					}
					assertSourceRefreshPending(t, e, compat.ErrReserveTransactionPending)
					ids, err := servers.RecordIDs(e.d)
					must(err)
					if len(ids) != 2 || servers.ActiveID(e.d) != next || !exists2(servers.RecordPath(e.d, active)) || !exists2(e.s.recPath(sub)) {
						t.Fatal("uncertain catalog lost recovery data", ids)
					}
					if p.RuntimeSnapshot() != before {
						t.Fatal("uncertain operation changed stopped process")
					}
					after, err := os.ReadFile(holdPath)
					must(err)
					if !bytes.Equal(after, hold) {
						t.Fatal("uncertain operation lost hold")
					}
					if err := p.StartAtBoot(e.d.Ctx); !errors.Is(err, reserve.ErrProfileBootHeld) {
						t.Fatal("uncertain operation allowed boot", err)
					}
					stop()
					return
				}
				if q.State != "completed" {
					t.Fatal("cold queued operation", q.State, q.ErrorCode, q.Message)
				}
				stop()
			}
			ids, err := servers.RecordIDs(e.d)
			must(err)
			wantActive := active
			wantCount := 1
			switch kind {
			case "refresh-removed":
				wantActive = next
			case "refresh-same-id":
				wantCount = 2
				rec, err := servers.ReadRecord(e.d, active)
				must(err)
				if rec["password"] != "rotated" {
					t.Fatal("same-ID credentials not refreshed")
				}
			case "refresh-rename":
				wantCount = 2
				rec, err := servers.ReadRecord(e.d, active)
				must(err)
				if rec["name"] != "Renamed" || description != "BROray - Renamed" {
					t.Fatal("cold profile name and owned description did not advance together")
				}
				installed, err := os.ReadFile(e.d.Root.XrayConfig())
				must(err)
				if !bytes.Equal(installed, config) {
					t.Fatal("cold rename altered installed bytes")
				}
			case "delete-replace":
				wantActive = "manual-c"
			case "delete-withdraw":
				wantActive = ""
				wantCount = 0
			}
			if len(ids) != wantCount || servers.ActiveID(e.d) != wantActive {
				t.Fatal("wrong final catalog/selection", ids, servers.ActiveID(e.d), wantActive)
			}
			if strings.HasPrefix(kind, "delete-") && exists2(e.s.recPath(sub)) {
				t.Fatal("deleted subscription retained")
			}
			if kind == "keep-active" {
				after, err := os.ReadFile(e.d.Root.XrayConfig())
				must(err)
				if !bytes.Equal(after, config) {
					t.Fatal("keep-active changed config")
				}
			}
			if p.RuntimeSnapshot() != before {
				t.Fatal("catalog operation changed stopped process intent")
			}
			after, err := os.ReadFile(holdPath)
			must(err)
			if !bytes.Equal(after, hold) {
				t.Fatal("catalog operation lost stopped hold")
			}
			pending, err := reserve.TransactionPending(e.d.Ctx, e.d.Root)
			must(err)
			if pending || e.d.Lock.Busy() {
				t.Fatal("successful operation retained owner/journal")
			}
			if err := p.StartAtBoot(e.d.Ctx); !errors.Is(err, reserve.ErrProfileBootHeld) {
				t.Fatal("boot lost stopped decision", err)
			}
		})
	}
}
