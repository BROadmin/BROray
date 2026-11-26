package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
)

func activeReplacementFixture(t *testing.T) (*env, string) {
	t.Helper()
	e := newEnv(t)
	if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B"); err != nil {
		t.Fatal(err)
	}
	old := ""
	for _, r := range e.s.liveServers() {
		if r.rec["address"] == "a.example" {
			old = r.id
		}
	}
	if old == "" {
		t.Fatal("fixture active missing")
	}
	if err := e.s.writeActive(old); err != nil {
		t.Fatal(err)
	}
	return e, old
}

func TestSyncRemovedActiveSwitchesWithinRefreshedSubscription(t *testing.T) {
	e, old := activeReplacementFixture(t)
	calls := 0
	e.d.Svc.ServersReapplyActive = func(context.Context, string) error {
		calls++
		id := servers.ActiveID(e.d)
		r, err := servers.ReadRecord(e.d, id)
		if err != nil || id == old || r["address"] != "b.example" || !belongsTo(r, subX) {
			t.Fatal("replacement not from refreshed subscription", err)
		}
		return nil
	}
	result, err := e.sync(t, "replace", "trojan://pw@b.example:443#B")
	if err != nil || calls != 1 || result.Impact != "removed-replaced" || result.Removed != 1 || result.Total != 1 || servers.ActiveID(e.d) == "" {
		t.Fatalf("replacement: %+v %v calls%d", result, err, calls)
	}
	if _, err := os.Stat(servers.RecordPath(e.d, old)); !os.IsNotExist(err) {
		t.Fatal("withdrawn server retained", err)
	}
}

func TestSyncRemovedActiveValidatesRefreshedCandidates(t *testing.T) {
	for _, allInvalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "skip-invalid", true: "all-invalid"}[allInvalid], func(t *testing.T) {
			e, old := activeReplacementFixture(t)
			testActiveConfig = func(_ context.Context, _ *compat.Deps, r servers.Record) error {
				if allInvalid || r["address"] == "b.example" {
					return errors.New("invalid candidate")
				}
				return nil
			}
			result, err := e.sync(t, "replace", "trojan://pw@b.example:443#B", "trojan://pw@c.example:443#C")
			if allInvalid {
				if err == nil || servers.ActiveID(e.d) != old || len(liveIDs(t, e)) != 2 {
					t.Fatalf("invalid candidates changed old catalog: %+v %v", result, err)
				}
			} else {
				r, readErr := servers.ReadRecord(e.d, servers.ActiveID(e.d))
				if err != nil || readErr != nil || r["address"] != "c.example" {
					t.Fatalf("valid replacement not selected: %+v %v %v", result, err, readErr)
				}
			}
		})
	}
}

func TestSyncRemovedActiveReplacementRollsBackFailures(t *testing.T) {
	for _, kind := range []string{"apply", "interface", "remove"} {
		t.Run(kind, func(t *testing.T) {
			e, old := activeReplacementFixture(t)
			runtime := old
			e.d.Svc.ServersReapplyActive = func(context.Context, string) error {
				id := servers.ActiveID(e.d)
				if kind == "apply" && id != old {
					return errors.New("apply failed")
				}
				runtime = id
				return nil
			}
			e.d.Svc.KeeneticEnsureInterface = func(context.Context) error {
				if kind == "interface" && servers.ActiveID(e.d) != old {
					return errors.New("interface failed")
				}
				return nil
			}
			if kind == "remove" {
				path := servers.QualityPath(e.d, old)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(path, "keep"), "blocked")
			}
			result, err := e.sync(t, "replace", "trojan://pw@b.example:443#B")
			if err == nil || servers.ActiveID(e.d) != old || runtime != old || len(liveIDs(t, e)) != 2 {
				t.Fatalf("replacement rollback: %+v %v active%s runtime%s", result, err, servers.ActiveID(e.d), runtime)
			}
		})
	}
}

func TestLiveSubscriptionRefreshDefersActiveConnectionParameters(t *testing.T) {
	e, id := activeReplacementFixture(t)
	source := e.record(t, e.create(t, "https://pending.example/source", ""))
	source["id"] = subX
	if err := e.s.writeRecord(source); err != nil {
		t.Fatal(err)
	}
	core, err := os.ReadFile(os.Getenv("BRORAY_XRAY_BIN"))
	if err != nil {
		t.Fatal("pinned Xray fixture:", err)
	}
	if err := os.WriteFile(e.d.Root.XrayBinary(), core, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := listener.Addr().(*net.TCPAddr).AddrPort()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	e.d.App = app.New(e.d.Root, platform.DefaultConfig(), e.d.Log)
	rec, err := servers.ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	config, err := servers.XrayConfig(rec, servers.GenSettings{Listen: "127.0.0.1", SocksPort: int(endpoint.Port()), LogLevel: "none"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.d.Root.XrayConfig(), config, 0600); err != nil {
		t.Fatal(err)
	}
	process := e.d.App.Xray
	if err := process.Start(e.d.Ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := process.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	before := process.RuntimeSnapshot()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		owned, err := reserve.CaptureProcess(before.PID, e.d.Root.XrayBinary())
		if err == nil {
			if _, err = reserve.VerifyListener(owned, endpoint); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("working local fixture did not own its SOCKS listener", err)
		}
	}
	runtimeApplies := 0
	e.d.Svc.ServersReapplySourceRefresh = func(context.Context, string, string, string) (string, error) {
		runtimeApplies++
		return id, nil
	}
	oldRecord, err := os.ReadFile(servers.RecordPath(e.d, id))
	if err != nil {
		t.Fatal(err)
	}
	b, c := "trojan://pw@b.example:443#B", "trojan://pw@c.example:443#C"
	refresh := func(policy, credential, impact string) {
		t.Helper()
		result, err := e.sync(t, policy, "trojan://"+credential+"@a.example:443#A", b, c)
		if err != nil {
			t.Fatal(err)
		}
		if result.Impact != impact || len(e.s.liveServers()) != 3 {
			t.Fatalf("catalog did not refresh with the expected impact: %s", result.Impact)
		}
		working, err := os.ReadFile(servers.RecordPath(e.d, id))
		if err != nil || !bytes.Equal(working, oldRecord) || runtimeApplies != 0 || process.RuntimeSnapshot() != before || servers.ActiveID(e.d) != id {
			t.Fatal("background refresh changed the working profile or its process before apply")
		}
	}
	pending := func(credential string) deletionImage {
		t.Helper()
		image, change, err := e.s.readPendingActiveProfile(subX)
		if err != nil {
			t.Fatal(err)
		}
		if credential == "" {
			if change != nil {
				t.Fatal("unexpected pending candidate")
			}
			return image
		}
		if change == nil || change.ServerID != id || change.ActiveSHA256 != compat.SHA256Hex(oldRecord) || image.info.Mode().Perm() != 0600 {
			t.Fatal("missing or unbound pending candidate")
		}
		record, ok := decodeObject(change.Candidate)
		if !ok || record["password"] != credential {
			t.Fatal("pending candidate does not represent the complete response")
		}
		return image
	}
	refresh("retain-unmatched", "partial", "configuration-retained")
	pending("")
	refresh("replace", "rotated", "configuration-pending")
	first := pending("rotated")
	refresh("replace", "rotated", "configuration-pending")
	if !first.unchanged() {
		t.Fatal("same pending profile acquired a new revision")
	}
	refresh("retain-unmatched", "partial", "configuration-retained")
	refresh("retain-unmatched", "pw", "configuration-retained")
	if !first.unchanged() {
		t.Fatal("partial response changed pending profile")
	}
	defaultReplace := e.d.Svc.ServersPrepareSourceRefresh
	e.d.Svc.ServersPrepareSourceRefresh = func(context.Context, string, []string, []string) (string, error) {
		pending("newer")
		return "", compat.ErrXrayCandidateRejected
	}
	if _, err := e.sync(t, "replace", "trojan://newer@a.example:443#A", b); !errors.Is(err, compat.ErrXrayCandidateRejected) {
		t.Fatal("catalog rejection was not preserved", err)
	}
	e.d.Svc.ServersPrepareSourceRefresh = defaultReplace
	if restored := pending("rotated"); !bytes.Equal(first.raw, restored.raw) {
		t.Fatal("catalog rollback did not restore the previous pending revision")
	}
	if err := process.Stop(e.d.Ctx); err != nil {
		t.Fatal(err)
	}
	before = process.RuntimeSnapshot()
	refresh("replace", "newest", "configuration-pending")
	newest := pending("newest")
	details, err := e.s.details(subX)
	if err != nil {
		t.Fatal(err)
	}
	status, ok := details["pendingActiveProfile"].(map[string]string)
	if !ok || len(status) != 2 || status["serverId"] != id || status["revision"] != compat.SHA256Hex(newest.raw) {
		t.Fatal("current pending projection did not expose just the exact revision and selected profile")
	}
	if _, found := details[pendingSourceRevisionField]; found {
		t.Fatal("private source authority leaked")
	}
	originalAgent, _ := source["httpUserAgent"].(string)
	for _, agent := range []string{"new-agent", originalAgent} {
		view, err := e.s.updateSettings(subX, map[string]any{"httpUserAgent": agent})
		if err != nil || view.(map[string]any)["pendingActiveProfile"] != nil {
			t.Fatalf("source input change exposed stale pending status: %v", err)
		}
	}
	refresh("replace", "newest", "configuration-pending")
	if rebound := pending("newest"); bytes.Equal(rebound.raw, newest.raw) {
		t.Fatal("source generation was not rebound after an authoritative refresh")
	}
	refresh("replace", "pw", "none")
	pending("")
}
