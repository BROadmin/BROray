package servers

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"strconv"
	"testing"

	"github.com/BROadmin/broray/modules/reserve"
)

func TestReserveInterfaceDescriptionProvider(t *testing.T) {
	e := newEnv(t)
	if got, err := e.d.Svc.InterfaceDescription(); err != nil || got != "" {
		t.Fatal("absent must retain ordinary description")
	}
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "trojan://pw@b.example:443#B")
	writeReserveAutomatic(t, e, a, b, true)
	if got, err := e.d.Svc.InterfaceDescription(); err != nil || got != "BROray - mix-auto" {
		t.Fatalf("mode description: %q %v", got, err)
	}
	writeReserveAutomatic(t, e, a, b, false)
	if got, err := e.d.Svc.InterfaceDescription(); err != nil || got != "" {
		t.Fatal("disabled override remains")
	}
	if err := os.WriteFile(reservePreferencesFile(e.d), []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.Svc.InterfaceDescription(); err == nil {
		t.Fatal("invalid reserve state authorized rename")
	}
}

func TestReserveRetainedDiagnosticKeepsOrdinaryHealth(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@127.0.0.1:1#A")
	rec, err := ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	port := freeTCP(t)
	base, err := XrayConfig(rec, GenSettings{Listen: "127.0.0.1", SocksPort: port, LogLevel: "none"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	diag := freeTCP(t)
	for diag == port {
		diag = freeTCP(t)
	}
	rendered, err := reserve.AttachHealthInbound(base, netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(diag)))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		raw  []byte
	}{{"ordinary", base}, {"retained", rendered}} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(e.d.Root.XrayConfig(), test.raw, 0600); err != nil {
				t.Fatal(err)
			}
			got := e.s.socksProbe(context.Background(), e.d.Root.XrayConfig(), id)
			if got["endpoint"] != "127.0.0.1:"+strconv.Itoa(port) || got["errorCode"] == "SOCKS_ENDPOINT_UNSUPPORTED" || got["attempts"] == 0 {
				t.Fatalf("wrong ordinary endpoint: %v", got)
			}
			after, err := os.ReadFile(e.d.Root.XrayConfig())
			if err != nil || string(after) != string(test.raw) {
				t.Fatal("probe changed live config")
			}
		})
	}
	for _, mutation := range []string{"tag-only", "route", "input", "no-main-socks"} {
		t.Run(mutation, func(t *testing.T) {
			var cfg map[string]any
			if err := json.Unmarshal(rendered, &cfg); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "tag-only":
				delete(cfg, "routing")
			case "route":
				cfg["routing"].(map[string]any)["rules"].([]any)[0].(map[string]any)["outboundTag"] = "direct"
			case "input":
				cfg["inbounds"].([]any)[1].(map[string]any)["settings"].(map[string]any)["udp"] = true
			case "no-main-socks":
				cfg["inbounds"].([]any)[0].(map[string]any)["protocol"] = "http"
			}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(e.d.Root.XrayConfig(), raw, 0600); err != nil {
				t.Fatal(err)
			}
			got := e.s.socksProbe(context.Background(), e.d.Root.XrayConfig(), id)
			if got["errorCode"] != "SOCKS_ENDPOINT_UNSUPPORTED" || got["attempts"] != 0 {
				t.Fatalf("untrusted extra input ignored: %v", got)
			}
		})
	}
}
