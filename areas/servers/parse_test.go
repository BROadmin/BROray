package servers

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

type refCase struct {
	uri, err, record, key, config string
	ok                            bool
}

var reStructLabel = regexp.MustCompile(`неправильную структуру: .*`)

func canonJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var x any
	_ = json.Unmarshal(b, &x)
	out, _ := json.Marshal(x)
	return string(out)
}

func reparse(t *testing.T, s string) string {
	t.Helper()
	var x any
	if err := json.Unmarshal([]byte(s), &x); err != nil {
		t.Fatalf("bad fixture %q: %v", s, err)
	}
	out, _ := json.Marshal(x)
	return string(out)
}

// TestParseLinkReference compares every share-link variant (vless, vmess
// modern + legacy base64, trojan, ss SIP002/legacy/2022, hysteria2/hy2, ECH,
// pcs, vcn, pqv, fm, mKCP, xhttp extra, ML-KEM, REALITY) with the record,
// error text, import key and Xray config the 3.x shell code produced.
func TestParseLinkReference(t *testing.T) {
	gs := GenSettings{Listen: "192.168.1.1", SocksPort: 2080, LogLevel: "warning",
		AccessLog: "/tmp/claude-0/ref/app/logs/access.log", ErrorLog: "/tmp/claude-0/ref/app/logs/error.log"}
	for _, c := range refCases {
		// 3.5.3 intentionally rejects SNI without TLS/REALITY. The old
		// reference accepted this value without applying it. Independent
		// TestURIFieldContext reproduced the loss before the fix.
		if c.uri == "vless://0f6c1b2a-1111-2222-3333-444455556666@h.example:443?sni=a+b#x+y" {
			c.ok = false
			c.err = "Ошибка: поле sni неприменимо к выбранным протоколу, транспорту или защите"
		}
		// This legacy reference had ALPN but no TLS, so its generated
		// outbound discarded ALPN. TestLegacyVMessFieldContext independently
		// checks the corrected fresh-import contract; stored models still load.
		if c.uri == "vmess://eyJhZGQiOiAibHcuZXhhbXBsZSIsICJwb3J0IjogNDQzLCAiaWQiOiAiMGY2YzFiMmEtMTExMS0yMjIyLTMzMzMtNDQ0NDU1NTU2NjY2IiwgImFscG4iOiBbImgyIiwgImgzIl19" {
			c.ok = false
			c.err = "Ошибка: неприменимое поле VMess JSON: alpn"
		}
		rec, err := ParseLink(c.uri, "manual-x", nil)
		name := c.uri
		if len(name) > 80 {
			name = name[:80]
		}
		if !c.ok {
			if err == nil {
				t.Errorf("%s: expected error %q, got record", name, c.err)
				continue
			}
			got := reStructLabel.ReplaceAllString(err.(*ParseError).Text(), "неправильную структуру: X")
			if got != c.err {
				t.Errorf("%s: error\n got %q\nwant %q", name, got, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
			continue
		}
		if rec["id"] != "manual-x" {
			t.Errorf("%s: id %v", name, rec["id"])
		}
		cp := map[string]any{}
		for k, v := range rec {
			if k != "id" {
				cp[k] = v
			}
		}
		if got, want := canonJSON(t, cp), reparse(t, c.record); got != want {
			t.Errorf("%s: record\n got %s\nwant %s", name, got, want)
		}
		key, _ := CanonicalKey(rec)
		if key != c.key {
			t.Errorf("%s: key %s want %s", name, key, c.key)
		}
		cfg, err := XrayConfig(rec, gs, "x")
		if err != nil {
			t.Errorf("%s: config error %v", name, err)
			continue
		}
		var x any
		_ = json.Unmarshal(cfg, &x)
		if got, want := canonJSON(t, x), reparse(t, c.config); got != want {
			t.Errorf("%s: config\n got %s\nwant %s", name, got, want)
		}
	}
}

func TestParseLinkSourceAndID(t *testing.T) {
	src := map[string]any{"type": "subscription", "subscriptionId": "sub1", "nodeIndex": 3}
	rec, err := ParseLink("trojan://pw@t.example:443", "subscription-sub1-0003", src)
	if err != nil {
		t.Fatal(err)
	}
	if get(rec, "source", "subscriptionId") != "sub1" || !jsonEq(get(rec, "source", "nodeIndex"), 3) {
		t.Fatalf("source %v", rec["source"])
	}
	for _, tc := range []struct{ id, want string }{
		{"", "Ошибка: не указан идентификатор сервера"},
		{"a/b", "Ошибка: идентификатор сервера содержит недопустимые символы"},
	} {
		_, err := ParseLink("trojan://pw@t.example:443", tc.id, nil)
		if err == nil || err.Error() != tc.want {
			t.Errorf("id %q: %v", tc.id, err)
		}
	}
	// a parse error wins over an invalid id (3.x parses first)
	if _, err := ParseLink("trojan://t.example:443", "", nil); err == nil || !strings.Contains(err.Error(), "пароль") {
		t.Errorf("order: %v", err)
	}
}

func TestKeysIgnoreNameAndSource(t *testing.T) {
	a, _ := ParseLink("vless://u@H.Example:443?security=tls&sni=s#One", "a", nil)
	b, _ := ParseLink("vless://u@h.example:443?security=tls&sni=s#Two", "b", map[string]any{"type": "subscription", "subscriptionId": "s", "nodeIndex": 1})
	ka, _ := CanonicalKey(a)
	kb, _ := CanonicalKey(b)
	if ka != kb || len(ka) != 64 {
		t.Fatalf("canonical keys differ: %s %s", ka, kb)
	}
	c, _ := ParseLink("vless://other@h.example:443?security=tls&sni=s#Two", "c", nil)
	kc, _ := CanonicalKey(c)
	if kc == ka {
		t.Fatal("different uuid must give a different key")
	}
	ca, _ := ContinuityKey(a)
	cc, _ := ContinuityKey(c)
	if ca != cc {
		t.Fatal("continuity key must ignore credentials")
	}
}

func TestVlessEncryption(t *testing.T) {
	k := strings.Repeat("A", 43)
	for _, tc := range []struct {
		v  string
		ok bool
	}{
		{"none", true},
		{"mlkem768x25519plus.native.0rtt." + k, true},
		{"mlkem768x25519plus.random.1rtt." + k + "." + k, true},
		{"mlkem768x25519plus.xorpub.1rtt.100-111-1111.75-0-111.50-0-3333." + k, true},
		{"mlkem768x25519plus.xorpub.1rtt.99-111-1111." + k, false},
		{"mlkem768x25519plus.xorpub.1rtt.100-200-100." + k, false},
		{"mlkem768x25519plus.native.2rtt." + k, false},
		{"mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 42), false},
		{"mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 1579), true},
		{"aes", false},
	} {
		if VlessEncryptionValid(tc.v) != tc.ok {
			t.Errorf("%.60s: want %v", tc.v, tc.ok)
		}
	}
}
