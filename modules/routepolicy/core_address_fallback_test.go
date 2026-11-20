package routepolicy

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/BROadmin/broray/modules/geodata"
)

// A resolved address may be unreachable while another A/AAAA address works.
// Exercise the rendered production outbound, not a test-selected IP family.
func TestCoreDirectAddressFallback(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("pinned actual core required")
	}
	for _, family := range []struct{ name, network, host string }{
		{"IPv4", "tcp4", "127.0.0.1"}, {"IPv6", "tcp6", "::1"},
	} {
		t.Run(family.name, func(t *testing.T) {
			target, err := net.Listen(family.network, net.JoinHostPort(family.host, "0"))
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			go func() {
				for {
					conn, err := target.Accept()
					if err != nil {
						return
					}
					go func() {
						defer conn.Close()
						conn.SetDeadline(time.Now().Add(3 * time.Second))
						_, _ = io.Copy(conn, conn)
					}()
				}
			}()
			addresses := []string{}
			for i := 2; i <= 49; i++ {
				addresses = append(addresses, fmt.Sprintf("127.0.0.%d", i))
			}
			addresses = append(addresses, family.host)
			config := map[string]any{
				"log":       map[string]any{"loglevel": "warning"},
				"dns":       map[string]any{"hosts": map[string]any{"fallback.example.test": addresses}},
				"inbounds":  []any{map[string]any{"tag": "socks", "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}},
				"outbounds": []any{map[string]any{"tag": "proxy", "protocol": "blackhole"}},
			}
			base, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := Render(base, []Rule{{ID: "direct", Enabled: true, Action: Direct, Domains: []string{"full:fallback.example.test"}}}, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, "lo")
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(rendered, &config); err != nil {
				t.Fatal(err)
			}
			for _, item := range config["outbounds"].([]any) {
				out := item.(map[string]any)
				if out["tag"] == "direct" {
					// Confine this offline fixture to loopback. Keep generated sockopt.
					out["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.0/8", "::1/128"}}}}
				}
			}
			proxy := startCore(t, bin, t.TempDir(), config)
			for attempt := 0; attempt < 16; attempt++ {
				if err := echoThroughSOCKS(proxy, "fallback.example.test", target.Addr().(*net.TCPAddr).Port); err != nil {
					t.Fatalf("healthy %s alternative lost on request %d: %v", family.name, attempt, err)
				}
			}
		})
	}
}
