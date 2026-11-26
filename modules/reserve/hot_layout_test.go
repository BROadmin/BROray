package reserve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"testing"
)

func hotFixture(t *testing.T, port int) []byte {
	t.Helper()
	raw := []byte(fmt.Sprintf(`{"log":{"loglevel":"warning"},"inbounds":[{"tag":"socks","protocol":"socks","listen":"0.0.0.0","port":2080,"settings":{"auth":"noauth","udp":true}}],"outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":"947dee4b-019c-4489-8b63-82853bb5f08c","encryption":"none"}]}]}},{"tag":"broray-direct","protocol":"freedom"},{"tag":"broray-deny","protocol":"blackhole"}],"routing":{"domainStrategy":"IPOnDemand","rules":[{"type":"field","inboundTag":["socks"],"domain":["full:deny.fixture.test"],"outboundTag":"broray-deny"},{"type":"field","inboundTag":["socks"],"domain":["full:direct.fixture.test"],"outboundTag":"broray-direct"}]}}`, port))
	result, err := AttachHealthInbound(raw, netip.MustParseAddrPort("127.0.0.1:39255"))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHotLayoutDiagnosticAuthority(t *testing.T) {
	endpoint := netip.MustParseAddrPort("127.0.0.1:39255")
	const socket = "/tmp/broray/run/reserve-control/api.sock"
	sources := [][]byte{hotFixture(t, 33451), hotFixture(t, 33452), hotFixture(t, 33453)}
	data, err := BuildHotLayout(sources, endpoint, socket)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := DiagnosticEndpoint(data); !ok || got != endpoint {
		t.Fatalf("generated pool lost owned diagnostic layout: %v %v", got, ok)
	}
	for _, change := range []struct{ old, replacement string }{
		{`"selector":["broray-reserve-blocked"]`, `"selector":["broray-direct"]`},
		{`"full:cp.cloudflare.com"`, `"full:arbitrary.fixture.test"`},
		{`"listen":"` + socket + `,0600"`, `"listen":"127.0.0.1:2088"`},
		{`"tag":"broray-reserve-slot-2"`, `"tag":"broray-reserve-slot-1"`},
	} {
		modified := bytes.Replace(data, []byte(change.old), []byte(change.replacement), 1)
		if bytes.Equal(modified, data) {
			t.Fatal("test mutation did not apply", change.old)
		}
		if _, ok := DiagnosticEndpoint(modified); ok {
			t.Fatal("accepted a drifted layout", change.replacement)
		}
	}
	var cfg map[string]any
	if err := json.Unmarshal(sources[1], &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["routing"].(map[string]any)["domainStrategy"] = "AsIs"
	sources[1], _ = json.Marshal(cfg)
	if _, err := BuildHotLayout(sources, endpoint, socket); err == nil {
		t.Fatal("different route policies in one pool were accepted")
	}
}
