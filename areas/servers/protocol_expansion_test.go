package servers

import (
	"bytes"
	"strings"
	"testing"
)

const testWG = "[Interface]\nPrivateKey = " + awgPrivate + "\nAddress = 10.10.0.2/32, fd00::2/128\nDNS = 1.1.1.1\nMTU = 1420\nReserved = 1, 2, 3\n[Peer]\nPublicKey = " + awgPublic + "\nPresharedKey = " + awgPrivate + "\nEndpoint = [2001:db8::1]:51820\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\n"

func TestWireGuardINIAndBackup(t *testing.T) {
	r, err := ParseLink(testWG, "wg-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r["protocol"] != "wireguard" || r["address"] != "2001:db8::1" {
		t.Fatal("wrong WG endpoint")
	}
	ob, err := Outbound(r)
	if err != nil {
		t.Fatal(err)
	}
	if ob["protocol"] != "wireguard" || has(ob, "streamSettings") || get(ob, "settings", "noKernelTun") != true {
		t.Fatal("WG must use userspace without stream overrides")
	}
	reserved, ok := get(ob, "settings", "reserved").([]any)
	if !ok || len(reserved) != 3 || !jsonEq(reserved[0], 1) || !jsonEq(reserved[1], 2) || !jsonEq(reserved[2], 3) || !jsonEq(get(ob, "settings", "mtu"), 1420) {
		t.Fatal("WG options lost")
	}
	b, _ := Marshal(r)
	restored, err := DecodeBackupRecord(b, "test")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := Marshal(restored)
	if !bytes.Equal(b, b2) {
		t.Fatal("WG backup changed fields")
	}
	masked, _ := Marshal(MaskRecord(r))
	if bytes.Contains(masked, []byte(awgPrivate)) || bytes.Contains(masked, []byte("PrivateKey")) {
		t.Fatal("WG secret leaked")
	}
	for _, inject := range []string{"PostUp = echo should-not-run", "Jc = 4", "MTU = 1400"} {
		bad := strings.Replace(testWG, "[Peer]", inject+"\n[Peer]", 1)
		if _, err := ParseLink(bad, "bad", nil); err == nil {
			t.Fatal("unsupported/duplicate WG setting accepted")
		}
	}
}

func TestHysteriaIdleTimeoutPreserved(t *testing.T) {
	r, err := ParseLink("hy2://synthetic@hy.example:443?udpIdleTimeout=41", "hy", nil)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := Outbound(r)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEq(get(ob, "streamSettings", "hysteriaSettings", "udpIdleTimeout"), 41) {
		t.Fatal("Hysteria UDP timeout lost")
	}
}

func TestProxyImportAndGenerate(t *testing.T) {
	for _, tc := range []struct {
		uri, protocol, host, security, user, password string
		port                                          int
	}{
		{"socks5://alice:p%40ss%3Aword@[2001:db8::1]:1081#IPv6", "socks", "2001:db8::1", "none", "alice", "p@ss:word", 1081},
		{"socks://proxy.example", "socks", "proxy.example", "none", "", "", 1080},
		{"http://alice:secret@proxy.example", "http", "proxy.example", "none", "alice", "secret", 80},
		{"https://proxy.example?sni=front.example&alpn=h2,http%2F1.1", "http", "proxy.example", "tls", "", "", 443},
	} {
		t.Run(tc.protocol+tc.security+tc.host, func(t *testing.T) {
			rec, err := ParseLink(tc.uri, "proxy-test", nil)
			if err != nil {
				t.Fatal(err)
			}
			if rec["protocol"] != tc.protocol || rec["address"] != tc.host || !jsonEq(rec["port"], tc.port) || rec["security"] != tc.security {
				t.Fatal("wrong endpoint")
			}
			if err := Validate(rec, "test"); err != nil {
				t.Fatal(err)
			}
			ob, err := Outbound(rec)
			if err != nil {
				t.Fatal(err)
			}
			sv := get(ob, "settings", "servers").([]any)[0].(map[string]any)
			if tc.user != "" {
				u := sv["users"].([]any)[0].(map[string]any)
				if u["user"] != tc.user || u["pass"] != tc.password {
					t.Fatal("credentials lost")
				}
			}
			if get(ob, "streamSettings", "security") != tc.security {
				t.Fatal("protection lost")
			}
			data, _ := Marshal(rec)
			back, err := DecodeBackupRecord(data, "test")
			if err != nil {
				t.Fatal(err)
			}
			data2, _ := Marshal(back)
			if !bytes.Equal(data, data2) {
				t.Fatal("backup changed proxy")
			}
			masked, _ := Marshal(MaskRecord(rec))
			if tc.password != "" && bytes.Contains(masked, []byte(tc.password)) {
				t.Fatal("password leaked")
			}
		})
	}
}

func TestProxyRejectsUnsupportedAndConflictingSettings(t *testing.T) {
	for _, uri := range []string{"https://proxy.example?security=none", "socks://:secret@proxy.example", "socks://proxy.example:0", "http://proxy.example/not-a-proxy-path", "http://proxy.example?unknownSecret=do-not-echo", "http://proxy.example?sni=a&sni=b", "socks://proxy.example?type=ws"} {
		if _, err := ParseLink(uri, "bad", nil); err == nil {
			t.Fatal("invalid proxy accepted")
		} else if bytes.Contains([]byte(err.Error()), []byte("do-not-echo")) {
			t.Fatal("error leaked value")
		}
	}
}

func TestProxyIdentityIncludesCredentials(t *testing.T) {
	a, err := ParseLink("socks5://alice:pass@proxy.example", "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ParseLink("socks5://bob:pass@proxy.example", "b", nil)
	ak, _ := CanonicalKey(a)
	bk, _ := CanonicalKey(b)
	if ak == bk {
		t.Fatal("different proxy accounts merged")
	}
}

// These two cases deliberately supersede the old 3.x unsupported-protocol fixtures.
func TestPreviouslyUnsupportedProxyReferences(t *testing.T) {
	for _, uri := range []string{"socks5://x@y:1", "https://x"} {
		r, e := ParseLink(uri, "manual-x", nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := XrayConfig(r, GenSettings{Listen: "127.0.0.1", SocksPort: 2080}, "test"); e != nil {
			t.Fatal(e)
		}
	}
}

func TestNewProtocolsPreviewCommit(t *testing.T) {
	e := newEnv(t)
	for _, link := range []string{"socks5://alice:secret@example.com:1080", "http://example.com:8080", "https://example.com", testWG} {
		preview := call(t, "POST", "/api/servers/import.cgi", `{"preview":true,"uri":`+jsonString(link)+`}`, nil)
		if preview.Code != 200 {
			t.Fatalf("preview status %d", preview.Code)
		}
		saved := call(t, "POST", "/api/servers/import.cgi", `{"uri":`+jsonString(link)+`}`, nil)
		id := asString(get(saved.Body, "data", "id"))
		if saved.Code != 200 || id == "" || !RecordExists(e.d, id) {
			t.Fatalf("save status %d", saved.Code)
		}
		details := call(t, "GET", "/api/servers/details.cgi?id="+id, "", nil)
		if strings.Contains(details.Raw, "secret") || strings.Contains(details.Raw, awgPrivate) {
			t.Fatal("details exposed credential")
		}
	}
}
