package subscriptions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/cgi"
	"go.yaml.in/yaml/v3"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func clashStage(t *testing.T, body string) *staged {
	t.Helper()
	ex, e := extractNodes([]byte(body), newMetadata())
	if e != nil {
		t.Fatal(e)
	}
	st, _ := stageNodes("test", ex, true, nil)
	return st
}
func TestClash355Basic(t *testing.T) {
	st := clashStage(t, "proxies:\n  - {name: invented, type: vless, server: example.invalid, port: 443, uuid: 11111111-1111-4111-8111-111111111111}\n")
	if st.Accepted != 1 {
		t.Fatalf("%+v", st)
	}
}
func clashProxy(kind string) map[string]any {
	return map[string]any{"name": "invented-" + kind, "type": kind, "server": "example.invalid", "port": 443, "uuid": "11111111-1111-4111-8111-111111111111"}
}
func clashBody(p ...any) string { b, _ := json.Marshal(map[string]any{"proxies": p}); return string(b) }
func TestClash355ProviderShape(t *testing.T) {
	p := clashProxy("vless")
	p["network"] = "xhttp"
	p["tls"] = true
	p["udp"] = true
	p["servername"] = "sni.example.invalid"
	p["client-fingerprint"] = "chrome"
	p["reality-opts"] = map[string]any{"public-key": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE", "short-id": "aabbccdd", "support-x25519mlkem768": true}
	p["xhttp-opts"] = map[string]any{"mode": "auto", "host": "front.example.invalid", "path": "/invented", "reuse-settings": map[string]any{"h-max-request-times": "600-900", "h-max-reusable-secs": "1800-3000", "max-concurrency": "18-22"}, "x-padding-bytes": "100-1000", "x-padding-obfs-mode": true, "no-grpc-header": true}
	yb, _ := yaml.Marshal(map[string]any{"proxies": []any{p}})
	st := clashStage(t, string(yb))
	if st.Accepted != 1 {
		t.Fatalf("%+v", st)
	}
	r := st.Records[0]
	b, _ := json.Marshal(r)
	for _, v := range []string{"11111111-1111-4111-8111-111111111111", "sni.example.invalid", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE", "aabbccdd", "600-900", "1800-3000", "18-22", "100-1000", "xPaddingObfsMode"} {
		if !strings.Contains(string(b), v) {
			t.Errorf("missing preserved parameter %s", v)
		}
	}
	if !strings.Contains(strings.Join(st.Warnings, " "), "Mihomo") {
		t.Fatal("missing advisory")
	}
	if r["sourceFormat"] != "clash-yaml" {
		t.Fatal("source")
	}
	out, err := servers.Outbound(r)
	if err != nil {
		t.Fatal(err)
	}
	stream := out["streamSettings"].(map[string]any)
	reality := stream["realitySettings"].(map[string]any)
	if reality["serverName"] != "sni.example.invalid" || reality["shortId"] != "aabbccdd" || reality["publicKey"] != "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE" {
		t.Fatal("REALITY semantics lost")
	}
	x := stream["xhttpSettings"].(map[string]any)
	extra := x["extra"].(map[string]any)
	if x["host"] != "front.example.invalid" || x["path"] != "/invented" || x["mode"] != "auto" || extra["xPaddingObfsMode"] != true || extra["xPaddingBytes"] != "100-1000" || extra["noGRPCHeader"] != true {
		t.Fatal("XHTTP semantics lost")
	}
	xm := extra["xmux"].(map[string]any)
	if xm["maxConcurrency"] != "18-22" || xm["hMaxRequestTimes"] != "600-900" || xm["hMaxReusableSecs"] != "1800-3000" {
		t.Fatal("XMUX semantics lost")
	}
	if r["uuid"] != p["uuid"] {
		t.Fatal("credential changed")
	}
	// Three independent provider VLESS definitions plus the AWG neutral profile.
	q := clashProxy("wireguard")
	delete(q, "uuid")
	q["private-key"] = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	q["public-key"] = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	q["ip"] = "10.0.0.2/32"
	q["dns"] = []any{"1.1.1.1"}
	q["remote-dns-resolve"] = true
	q["mtu"] = 1280
	opts := map[string]any{"version": 3, "header-protection-key": "invented-extension", "content-padding-addition": "10-20", "disable-cookies": true, "random-trailers": true, "keepalive-timeout": 30, "max-handshake-attempts": 5, "reject-after-time": 180, "rekey-after-time": 120, "rekey-timeout": 5, "jc": 4, "jmin": 20, "jmax": 40, "s1": 1, "s2": 2, "s3": 3, "s4": 4, "h1": "1-2", "h2": "3-4", "h3": "5-6", "h4": "7-8", "i1": "<b 0x01>"}
	q["amnezia-wg-option"] = opts
	a := clashStage(t, clashBody(q))
	if a.Accepted != 1 {
		t.Fatalf("%+v", a)
	}
	ar := a.Records[0]
	if ar["protocol"] != "amneziawg" || ar["name"] != "invented-wireguard" {
		t.Fatal("AWG semantics")
	}
	bb, _ := json.Marshal(ar["amneziawg"])
	var profile amnezia.Profile
	if json.Unmarshal(bb, &profile) != nil {
		t.Fatal("profile")
	}
	raw := profile.Parameters["awg"].(map[string]any)["clashOptions"].(map[string]any)
	expectedJSON, _ := json.Marshal(q)
	var expected map[string]any
	json.Unmarshal(expectedJSON, &expected)
	if !reflect.DeepEqual(raw, expected) {
		t.Fatal("AWG options lost")
	}
	if !strings.Contains(profile.NativeConfig, "Jc = 4") || !strings.Contains(profile.NativeConfig, "H1 = 1-2") {
		t.Fatal("native mapping")
	}
	if !strings.Contains(strings.Join(a.Warnings, " "), "Подключение пока не поддерживается") {
		t.Fatal("runtime advisory")
	}
	nodes := []any{}
	for i := 0; i < 3; i++ {
		p2 := map[string]any{}
		for k, v := range p {
			p2[k] = v
		}
		p2["server"] = fmt.Sprintf("node%d.example.invalid", i)
		p2["name"] = fmt.Sprintf("invented-%d", i)
		nodes = append(nodes, p2)
	}
	nodes = append(nodes, q)
	four := clashStage(t, clashBody(nodes...))
	if four.Accepted != 4 || four.Rejected != 0 {
		t.Fatalf("provider shape: %+v", four)
	}

}
func TestClash355Protocols(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "trojan", "ss", "http", "socks5", "hysteria2", "wireguard"} {
		t.Run(kind, func(t *testing.T) {
			p := clashProxy(kind)
			if kind != "vless" && kind != "vmess" {
				delete(p, "uuid")
			}
			switch kind {
			case "trojan", "hysteria2", "ss":
				p["password"] = "invented-password"
			case "http", "socks5":
				p["username"] = "invented-user"
				p["password"] = "invented-password"
			case "wireguard":
				p["private-key"] = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
				p["public-key"] = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
				p["ip"] = "10.0.0.2/32"
			}
			if kind == "ss" {
				p["cipher"] = "aes-128-gcm"
			}
			st := clashStage(t, clashBody(p))
			if st.Accepted != 1 {
				t.Fatalf("%+v", st)
			}
			if e := servers.Validate(st.Records[0], st.Records[0]["id"].(string)+".json"); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestClash355TransportAndRefusals(t *testing.T) {
	for _, network := range []string{"tcp", "ws", "grpc", "xhttp", "httpupgrade"} {
		t.Run(network, func(t *testing.T) {
			p := clashProxy("vless")
			p["network"] = network
			switch network {
			case "ws":
				p["ws-opts"] = map[string]any{"path": "/test", "headers": map[string]any{"Host": "front.example.invalid", "Authorization": "invented-secret"}}
			case "grpc":
				p["grpc-opts"] = map[string]any{"grpc-service-name": "test"}
			case "xhttp":
				p["xhttp-opts"] = map[string]any{"path": "/test", "mode": "auto"}
			case "httpupgrade":
				p["http-upgrade-opts"] = map[string]any{"path": "/test", "host": "front.example.invalid"}
			}
			st := clashStage(t, clashBody(p))
			if st.Accepted != 1 {
				t.Fatalf("%+v", st)
			}
		})
	}
	for _, tc := range []struct {
		name, kind string
		mut        func(map[string]any)
	}{
		{"tuic", "core-unsupported", func(p map[string]any) { p["type"] = "tuic" }},
		{"unknown-root", "unknown-extension", func(p map[string]any) { p["secretExtension"] = "never-display-this" }},
		{"removed-insecure", "core-removed", func(p map[string]any) { p["skip-cert-verify"] = true }},
		{"unknown-xhttp", "unknown-extension", func(p map[string]any) {
			p["xhttp-opts"] = map[string]any{"secretExtension": "never-display-this"}
			p["network"] = "xhttp"
		}},
	} {
		t.Run("retained-"+tc.name, func(t *testing.T) {
			p := clashProxy("vless")
			tc.mut(p)
			st := clashStage(t, clashBody(clashProxy("vless"), p))
			if st.Received != 2 || st.Accepted != 2 || st.Rejected != 0 || len(st.Records) != 2 {
				t.Fatal("valid unavailable node must remain in the mixed subscription")
			}
			clashCapability357Assert(t, p, tc.kind)
			clashCapability357Persist(t, p)
			if !clashFull357ContainsSource(st.Records[1], p) {
				t.Fatal("mixed subscription lost the retained source")
			}
			public, _ := json.Marshal(servers.MaskRecord(st.Records[1]))
			if strings.Contains(strings.Join(st.Warnings, " ")+string(public), "never-display-this") || strings.Contains(string(public), "secretExtension") {
				t.Fatal("secret leak")
			}
		})
	}
	for _, mut := range []func(map[string]any){func(p map[string]any) { p["network"] = "kcp" }, func(p map[string]any) { p["port"] = 65536 }, func(p map[string]any) { p["port"] = 0.5 }, func(p map[string]any) { p["tls"] = "true" }} {
		p := clashProxy("vless")
		mut(p)
		st := clashStage(t, clashBody(clashProxy("vless"), p))
		if st.Accepted != 1 || st.Rejected != 1 {
			t.Fatalf("%+v", st)
		}
		if strings.Contains(strings.Join(st.Warnings, " "), "never-display-this") {
			t.Fatal("secret leak")
		}
		all := clashStage(t, clashBody(p))
		if all.Accepted != 0 || all.Received != 1 || all.Rejected != 1 {
			t.Fatal("all failed")
		}
	}
}
func TestClash355YAMLSafety(t *testing.T) {
	for _, body := range []string{"proxies: []\nproxies: []", "proxies: [!secret private]", "proxies: &a [*a]", "proxies: []\n---\nproxies: []", "proxies: [{<<: {type: vless}}]", "proxies: [" + strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40) + "]", "proxies: [ {name: secret, name: private} ]", strings.Repeat("x", 2*1024*1024+1)} {
		_, e := extractNodes([]byte(body), newMetadata())
		if e == nil {
			t.Fatal("unsafe YAML accepted")
		}
		if strings.Contains(fmt.Sprint(e), "private") {
			t.Fatal("secret decoder error leaked")
		}
	}
	body := "proxies:\n- &node {name: anchor, type: vless, server: example.invalid, port: 443, uuid: 11111111-1111-4111-8111-111111111111}\n- *node\nproxy-groups: [{name: ignored}]\nrules: [MATCH,ignored]\n"
	st := clashStage(t, body)
	if st.Accepted != 1 || st.Rejected != 0 {
		t.Fatalf("anchor not safely handled: %+v", st)
	}
}

func TestClash355StandardUpgradeAndWGBareIPs(t *testing.T) {
	p := clashProxy("vless")
	p["network"] = "ws"
	p["ws-opts"] = map[string]any{"path": "/upgrade", "headers": map[string]any{"Host": "front.example.invalid", "Authorization": "invented-secret"}, "v2ray-http-upgrade": true}
	st := clashStage(t, clashBody(p))
	if st.Accepted != 1 || st.Records[0]["network"] != "httpupgrade" {
		t.Fatalf("%+v", st)
	}
	w := clashProxy("wireguard")
	delete(w, "uuid")
	w["private-key"] = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	w["public-key"] = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	w["ip"] = "10.0.0.2"
	w["ipv6"] = "fd00::2"
	w["dns"] = []any{}
	wg := clashStage(t, clashBody(w))
	if wg.Accepted != 1 {
		t.Fatalf("%+v", wg)
	}
	b, _ := json.Marshal(wg.Records[0])
	for _, v := range []string{"10.0.0.2/32", "fd00::2/128"} {
		if !strings.Contains(string(b), v) {
			t.Fatal("bare IP normalization lost")
		}
	}
}
func TestClash355LimitsAndRanges(t *testing.T) {
	for _, r := range []any{"", "0", "1-2", -1, "2147483648", "2-1", 0.5, true} {
		p := clashProxy("vless")
		p["network"] = "xhttp"
		p["xhttp-opts"] = map[string]any{"reuse-settings": map[string]any{"max-concurrency": r, "h-keep-alive-period": 30}}
		st := clashStage(t, clashBody(p))
		valid := r == "" || r == "0" || r == "1-2"
		if (st.Accepted == 1) != valid {
			t.Fatalf("range %v: %+v", r, st)
		}
	}
	nodes := make([]any, maxNodes+1)
	for i := range nodes {
		nodes[i] = clashProxy("vless")
	}
	if _, e := extractNodes([]byte(clashBody(nodes...)), newMetadata()); e == nil {
		t.Fatal("node bound")
	}
	// Repeated aliases are charged after expansion as well as at AST depth.
	b := "x: &x [0,0,0,0,0,0,0,0,0,0]\ny: &y [*x,*x,*x,*x,*x,*x,*x,*x,*x,*x]\nz: &z [*y,*y,*y,*y,*y,*y,*y,*y,*y,*y]\nproxies: [*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z,*z]\n"
	if _, e := extractNodes([]byte(b), newMetadata()); e == nil {
		t.Fatal("alias expansion bound")
	}
}
func TestClash355FailedUpdatePreservesCatalog(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA)
	id := e.create(t, p.URL, "")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	ids := liveIDs(t, e)
	before, _ := os.ReadFile(servers.RecordPath(e.d, ids[0]))
	// A short VLESS ID is valid upstream. Use a known invalid port so an
	// unknown extension cannot turn this malformed update into retention.
	p.set(clashBody(map[string]any{"name": "invented-secret-name", "type": "vless", "server": "secret.example.invalid", "port": 65536, "uuid": "invented-secret", "secretExtension": "never-display-this"}))
	if err := e.refreshSync(t, id); err == nil {
		t.Fatal("expected failure")
	}
	res := e.record(t, id)["lastUpdateResult"].(map[string]any)
	if !numIs(res["received"], 1) || !numIs(res["rejected"], 1) {
		t.Fatal("counters lost")
	}
	w := fmt.Sprint(res["warnings"])
	if strings.TrimSpace(w) == "" || strings.Contains(w, "secretExtension") || strings.Contains(w, "never-display-this") || strings.Contains(w, "secret.example") || strings.Contains(w, "invented-secret") {
		t.Fatal("unsafe field diagnostic")
	}
	after, _ := os.ReadFile(servers.RecordPath(e.d, ids[0]))
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(ids, liveIDs(t, e)) {
		t.Fatal("catalog changed")
	}
}
func TestClash355OfficialCore(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("official core unavailable")
	}
	for _, network := range []string{"tcp", "ws", "grpc", "xhttp", "httpupgrade"} {
		t.Run(network, func(t *testing.T) {
			p := clashProxy("vless")
			p["network"] = network
			if network == "xhttp" {
				p["xhttp-opts"] = map[string]any{"path": "/test", "mode": "auto", "reuse-settings": map[string]any{"max-concurrency": "18-22", "h-max-request-times": "600-900", "h-max-reusable-secs": "1800-3000"}, "x-padding-bytes": "100-1000", "x-padding-obfs-mode": true, "no-grpc-header": true}
			}
			st := clashStage(t, clashBody(p))
			if st.Accepted != 1 {
				t.Fatalf("%+v", st)
			}
			cfg, err := servers.XrayConfig(st.Records[0], servers.GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "none"}, "clash-test")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "client.json")
			if os.WriteFile(path, cfg, 0600) != nil {
				t.Fatal("write")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, bin, "run", "-test", "-config", path).CombinedOutput()
			if err != nil {
				t.Fatalf("official core rejected synthetic config: %v %s", err, output)
			}
		})
	}
}

func TestClash355PredecodeComplexity(t *testing.T) {
	for _, body := range []string{"proxies: [" + strings.Repeat("0,", 22000) + "0]", "proxies:\n" + strings.Repeat("- a\n", 22000)} {
		_, recognized, e := clashDocument([]byte(body))
		if !recognized || e == nil {
			t.Fatal("dense syntax not refused before decoding")
		}
	}
}

func TestClash355TLSAndLocalPolicy(t *testing.T) {
	p := clashProxy("vless")
	p["tls"] = true
	p["servername"] = "tls.example.invalid"
	p["alpn"] = []any{"h2", "http/1.1"}
	p["client-fingerprint"] = "chrome"
	p["skip-cert-verify"] = false
	st := clashStage(t, clashBody(p))
	if st.Accepted != 1 {
		t.Fatalf("%+v", st)
	}
	out, e := servers.Outbound(st.Records[0])
	if e != nil {
		t.Fatal(e)
	}
	stream := out["streamSettings"].(map[string]any)
	tls := stream["tlsSettings"].(map[string]any)
	if tls["serverName"] != "tls.example.invalid" || !reflect.DeepEqual(tls["alpn"], []any{"h2", "http/1.1"}) {
		t.Fatal("TLS semantics lost")
	}
	// Explicit UDP denial must not silently become an unrestricted outbound.
	p["udp"] = false
	clashCapability357Assert(t, p, "local-policy")
	clashCapability357Persist(t, p)
}

func TestClash355LegacyXrayDepth(t *testing.T) {
	var nested any = "invented"
	for i := 0; i < 40; i++ {
		nested = map[string]any{"x": nested}
	}
	b, _ := json.Marshal(map[string]any{"metadata": nested, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"address": "example.invalid", "port": 443, "id": "11111111-1111-4111-8111-111111111111", "encryption": "none"}}}})
	st := clashStage(t, string(b))
	if st.Accepted != 1 {
		t.Fatalf("legacy JSON changed: %+v", st)
	}
}

func TestClash355AWGStableIdentity(t *testing.T) {
	p := clashProxy("wireguard")
	delete(p, "uuid")
	p["private-key"] = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	p["public-key"] = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	p["ip"] = "10.0.0.2"
	p["amnezia-wg-option"] = map[string]any{"h1": "1-2", "h2": "3-4", "h3": "5-6", "h4": "7-8", "jc": 4, "jmin": 10, "jmax": 20, "s1": 1, "s2": 2}
	body := clashBody(p)
	first := ""
	for i := 0; i < 32; i++ {
		st := clashStage(t, body)
		if st.Accepted != 1 {
			t.Fatalf("%+v", st)
		}
		key, e := servers.CanonicalKey(st.Records[0])
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = key
		} else if key != first {
			t.Fatal("identical AWG import changed CanonicalKey")
		}
	}
}

func TestClash355RawDocumentPriority(t *testing.T) {
	body := "notes: >-\n  vless://22222222-2222-4222-8222-222222222222@note.example.invalid:443#ignored\nproxies:\n- name: actual\n  type: trojan\n  server: actual.example.invalid\n  port: 443\n  password: >-\n    #invented\n    suffix\n  network: ws\n  ws-opts:\n    headers:\n      routing: invented-route-header\n      support-url: invented-support-header\n"
	for _, b := range []string{body, base64.StdEncoding.EncodeToString([]byte(body))} {
		st := clashStage(t, b)
		if st.Accepted != 1 || st.Rejected != 0 || st.Records[0]["address"] != "actual.example.invalid" || st.Records[0]["password"] != "#invented suffix" {
			t.Fatalf("raw YAML was treated as legacy lines: %+v", st)
		}
		out, e := servers.Outbound(st.Records[0])
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(out)
		for _, v := range []string{"invented-route-header", "invented-support-header"} {
			if !strings.Contains(string(raw), v) {
				t.Fatal("YAML header removed as legacy metadata")
			}
		}
	}
}

func TestClash355AWGExactNumericExtension(t *testing.T) {
	p := clashProxy("wireguard")
	delete(p, "uuid")
	p["private-key"] = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	p["public-key"] = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	p["ip"] = "10.0.0.2"
	p["amnezia-wg-option"] = map[string]any{"jc": 4, "test-extension": uint64(18446744073709551615)}
	st := clashStage(t, clashBody(p))
	if st.Accepted != 1 {
		t.Fatalf("%+v", st)
	}
	want, _ := json.Marshal(p)
	checkExact := func(rec servers.Record) {
		t.Helper()
		profile, ok := rec["importProfile"].(map[string]any)
		if !ok || profile["format"] != "clash" {
			t.Fatal("AWG unknown extension must remain in the private Clash source")
		}
		b, err := json.Marshal(profile["node"])
		if err != nil || string(b) != string(want) || !strings.Contains(string(b), `"test-extension":18446744073709551615`) {
			t.Fatal("AWG structured source or uint64 extension precision lost")
		}
		var api *cgi.APIError
		if out, err := servers.Outbound(rec); out != nil || !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" {
			t.Fatal("AWG unknown extension emitted a stripped runnable outbound")
		}
		public, _ := json.Marshal(servers.MaskRecord(rec))
		if strings.Contains(string(public), "test-extension") || strings.Contains(string(public), "18446744073709551615") {
			t.Fatal("AWG private source escaped public masking")
		}
	}
	checkExact(st.Records[0])
	stored, err := servers.Marshal(st.Records[0])
	if err != nil {
		t.Fatal(err)
	}
	restored, err := servers.DecodeBackupRecord(stored, st.Records[0]["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	checkExact(restored)

	// The independently supported numeric option still produces native INI.
	p["amnezia-wg-option"] = map[string]any{"jc": 4}
	known, _ := clashFull357Record(t, p)
	b, _ := json.Marshal(known["amneziawg"])
	if !strings.Contains(string(b), "Jc = 4") {
		t.Fatal("known numeric native option lost")
	}
}

func TestClash355NoGRPCHeader(t *testing.T) {
	// Mihomo's pinned weak decoder accepts integral booleans, not strings.
	for _, tc := range []struct {
		value any
		valid bool
		want  bool
	}{{true, true, true}, {false, true, false}, {"true", false, false}, {1, true, true}, {0, true, false}, {nil, false, false}} {
		t.Run(fmt.Sprint(tc.value), func(t *testing.T) {
			p := clashProxy("vless")
			p["network"] = "xhttp"
			p["xhttp-opts"] = map[string]any{"path": "/invented", "mode": "auto", "no-grpc-header": tc.value}
			st := clashStage(t, clashBody(p))
			if (st.Accepted == 1) != tc.valid {
				t.Fatalf("valid=%v %+v", tc.valid, st)
			}
			if tc.valid {
				out, e := servers.Outbound(st.Records[0])
				if e != nil {
					t.Fatal(e)
				}
				stream := out["streamSettings"].(map[string]any)
				extra := stream["xhttpSettings"].(map[string]any)["extra"].(map[string]any)
				if v, ok := extra["noGRPCHeader"]; !ok || v != tc.want {
					t.Fatal("noGRPCHeader lost or changed")
				}
			}
		})
	}
}
