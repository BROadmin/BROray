package servers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func TestRoutingPolicyAllActivePaths(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	rules := []routepolicy.Rule{{ID: "deny", Enabled: true, Action: routepolicy.Deny, Domains: []string{"ads.example"}}}
	calls := 0
	e.d.Svc.RoutesConfigureXray = func(ctx context.Context, cfg []byte) ([]byte, error) {
		calls++
		return routepolicy.Render(cfg, rules, geodata.Snapshot{}, geodata.AssetDirectory, routepolicy.OutboundTags{VPN: "proxy", Direct: "broray-direct", Deny: "broray-deny"}, []string{"socks"}, "")
	}
	if _, err := e.s.activate(id); err != nil {
		t.Fatal(err)
	}
	if len(e.applied) != 1 || !bytes.Contains(e.applied[0], []byte(`"domainStrategy":"IPOnDemand"`)) {
		t.Fatal("activation lost routing policy")
	}
	if err := e.s.ReapplyActive(context.Background(), "subscription"); err != nil {
		t.Fatal(err)
	}
	if len(e.applied) != 2 || !bytes.Contains(e.applied[1], []byte(`"broray-deny"`)) {
		t.Fatal("reapply lost routing policy")
	}
	record, err := ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := GenerateConfig(context.Background(), e.d, record)
	if err != nil || !bytes.Contains(cfg, []byte(`"broray-deny"`)) {
		t.Fatal("persistent generation lost policy")
	}
	before := calls
	probe, err := XrayConfig(record, e.s.genSettings(context.Background()), "probe")
	if err != nil {
		t.Fatal(err)
	}
	if calls != before || bytes.Contains(probe, []byte(`"routing"`)) {
		t.Fatal("policy contaminates health probe")
	}
}

func TestRoutingPolicyFailurePreservesActiveConfig(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(id); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), e.applied[0]...)
	e.d.Svc.RoutesConfigureXray = func(context.Context, []byte) ([]byte, error) { return nil, errors.New("policy data unavailable") }
	if _, err := e.s.activate(id); err == nil {
		t.Fatal("activation ignored policy failure")
	}
	if err := e.s.ReapplyActive(context.Background(), "subscription"); err == nil {
		t.Fatal("reapply ignored policy failure")
	}
	if len(e.applied) != 1 || !bytes.Equal(e.applied[0], before) || ActiveID(e.d) != id {
		t.Fatal("working active configuration changed")
	}
}
