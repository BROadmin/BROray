package servers

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVLESSReality(t *testing.T) {
	s, err := ParseLink("vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?security=reality&encryption=none&pbk=PUBKEY&fp=chrome&sni=www.example.com&sid=abcd&spx=%2F&type=tcp&flow=xtls-rprx-vision#NL%20%F0%9F%87%B3%F0%9F%87%B1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Protocol != "vless" || s.Network != "raw" || s.Security != "reality" || s.Reality.PublicKey != "PUBKEY" || s.Flow != "xtls-rprx-vision" || s.Name != "NL 🇳🇱" {
		t.Fatalf("%+v", s)
	}
	out := Outbound(s)
	b, _ := json.Marshal(out)
	js := string(b)
	for _, want := range []string{`"protocol":"vless"`, `"flow":"xtls-rprx-vision"`, `"realitySettings"`, `"publicKey":"PUBKEY"`, `"rawSettings"`} {
		if !strings.Contains(js, want) {
			t.Errorf("missing %s in %s", want, js)
		}
	}
}

func TestParseVMessSSTrojanHy2(t *testing.T) {
	vm := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"VM","add":"h.example","port":"8080","id":"11111111-2222-3333-4444-555555555555","aid":"0","scy":"auto","net":"ws","type":"none","host":"h.example","path":"/ws","tls":"tls","sni":"h.example"}`))
	s, err := ParseLink("vmess://" + vm)
	if err != nil || s.Network != "ws" || s.Security != "tls" || s.Transport.Path != "/ws" || s.Name != "VM" {
		t.Fatalf("vmess: %v %+v", err, s)
	}
	ss, err := ParseLink("ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:secret")) + "@5.6.7.8:8388#SS")
	if err != nil || ss.Method != "chacha20-ietf-poly1305" || ss.Password != "secret" || ss.Port != 8388 {
		t.Fatalf("ss: %v %+v", err, ss)
	}
	tr, err := ParseLink("trojan://pw@9.9.9.9:443?sni=t.example&type=grpc&serviceName=svc#TR")
	if err != nil || tr.Security != "tls" || tr.Network != "grpc" || tr.Transport.ServiceName != "svc" {
		t.Fatalf("trojan: %v %+v", err, tr)
	}
	hy, err := ParseLink("hysteria2://pass@7.7.7.7:443?sni=h.example&obfs=salamander&obfs-password=ob&mport=20000-30000#HY")
	if err != nil || hy.Protocol != "hysteria2" || hy.Auth != "pass" || hy.Hysteria.Ports != "20000-30000" {
		t.Fatalf("hy2: %v %+v", err, hy)
	}
	b, _ := json.Marshal(Outbound(hy))
	if !strings.Contains(string(b), `"protocol":"hysteria"`) || !strings.Contains(string(b), `"udphop"`) || !strings.Contains(string(b), `"salamander"`) {
		t.Fatalf("hy2 outbound: %s", b)
	}
}

func TestParseRefusals(t *testing.T) {
	for _, l := range []string{"tuic://x@1.2.3.4:1?x=y", "socks://a@1.2.3.4:1", "vless://u@1.2.3.4:443?allowInsecure=1", "vless://u@1.2.3.4:443?security=reality", "ss://bad", "", "vless://u@host:99999"} {
		if _, err := ParseLink(l); err == nil {
			t.Errorf("%q accepted", l)
		}
	}
}
