package subscriptions

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

const (
	linkA = "trojan://pw@a.example:443#A"
	linkB = "vless://0f6c1b2a-1111-2222-3333-444455556666@nl.example.net:443?type=tcp&security=reality&pbk=PBK&sni=a.com&flow=xtls-rprx-vision#NL"
	linkC = "ss://YWVzLTI1Ni1nY206cGFzcw@ss.example:8388#S"
)

func TestPayloadDetection(t *testing.T) {
	plain := linkA + "\n" + linkB + "\n"
	b64 := base64.StdEncoding.EncodeToString([]byte(plain))
	urlsafe := strings.TrimRight(base64.URLEncoding.EncodeToString([]byte(plain+linkC+"\n")), "=")
	wrapped := b64[:10] + "\r\n" + b64[10:20] + "\n\t " + b64[20:]
	cases := []struct {
		name, body string
		lines      int
		json       bool
		code       string
	}{
		{"plain list", plain, 2, false, ""},
		{"BOM CRLF junk and comments", "\xEF\xBB\xBF" + linkA + "\r\n# comment\r\n\r\njunk line\r\n  " + linkB + "  ", 3, false, ""},
		{"base64 std", b64, 2, false, ""},
		{"base64 urlsafe no padding", urlsafe, 3, false, ""},
		{"base64 wrapped", wrapped, 2, false, ""},
		{"base64 with metadata", base64.StdEncoding.EncodeToString([]byte("#profile-title: T\n" + plain)), 2, false, ""},
		{"html", "<!DOCTYPE html>\n<html><body>x</body></html>", 0, false, "UNSUPPORTED_CONTENT"},
		{"html tag", "<html>\n", 0, false, "UNSUPPORTED_CONTENT"},
		{"garbage", "hello world", 0, false, "PARSE_ERROR"},
		{"two json docs", "{\"outbounds\":[]} {\"outbounds\":[]}", 0, false, "PARSE_ERROR"},
		{"json scalar", "[1]", 0, false, "PARSE_ERROR"},
		{"json no outbounds", "{\"inbounds\":[]}", 0, false, "PARSE_ERROR"},
		{"json empty", "{\"outbounds\":[{\"protocol\":\"freedom\"}]}", 0, true, "NO_VALID_NODES"},
		{"only comments", "# a\n#b\n", 0, false, "PARSE_ERROR"},
		{"too many", strings.Repeat(linkA+"\n", 501), 0, false, "CONTENT_TOO_LARGE"},
		{"exactly 500", strings.Repeat(linkA+"\n", 500), 500, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ex, err := extractNodes([]byte(c.body), newMetadata())
			if c.code != "" {
				if err == nil || err.Code != c.code {
					t.Fatalf("want %s, got %v (%v)", c.code, err, ex)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(ex.Lines) != c.lines || ex.JSONImport != c.json {
				t.Fatalf("lines=%d json=%v: %q", len(ex.Lines), ex.JSONImport, ex.Lines)
			}
		})
	}
	ex, _ := extractNodes([]byte("\xEF\xBB\xBF  "+linkA+"  \r\n"), newMetadata())
	if ex.Lines[0] != linkA {
		t.Fatalf("trim: %q", ex.Lines[0])
	}
}

func xrayDoc(outbounds ...string) string {
	return `{"remarks":"Prof","outbounds":[` + strings.Join(outbounds, ",") + `,{"protocol":"freedom","tag":"direct"},{"protocol":"blackhole"}]}`
}

const (
	obVless = `{"protocol":"vless","tag":"nl","settings":{"vnext":[{"address":"nl.example.net","port":443,"users":[{"id":"0f6c1b2a-1111-2222-3333-444455556666","encryption":"none","flow":"xtls-rprx-vision"}]}]},
	 "streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"a.com","publicKey":"PBK","shortId":"01","fingerprint":"chrome"}}}`
	obVmess  = `{"protocol":"vmess","tag":"de","settings":{"vnext":[{"address":"de.example","port":8443,"users":[{"id":"0f6c1b2a-1111-2222-3333-444455556666","security":"auto","alterId":0}]}]},"streamSettings":{"network":"ws","security":"tls","wsSettings":{"path":"/ws","headers":{"Host":"cdn.example"}},"tlsSettings":{"serverName":"cdn.example","alpn":["h2","http/1.1"]}}}`
	obTrojan = `{"protocol":"trojan","settings":{"servers":[{"address":"tr.example","port":443,"password":"p@ss word"}]},"streamSettings":{"network":"grpc","security":"tls","grpcSettings":{"serviceName":"svc","multiMode":true}}}`
	obSS     = `{"protocol":"shadowsocks","tag":"ss","settings":{"servers":[{"address":"2001:4860::8888","port":8388,"method":"chacha20-ietf-poly1305","password":"secret"}]}}`
	obXHTTP  = `{"protocol":"vless","settings":{"address":"x.example","port":443,"id":"abc","encryption":"none"},"streamSettings":{"network":"xhttp","security":"tls","xhttpSettings":{"path":"/x","mode":"packet-up","xPaddingBytes":"100-1000"}}}`
)

func TestXrayJSONConversion(t *testing.T) {
	doc := `[` + xrayDoc(obVless, obVmess, obTrojan, obSS, obXHTTP) + `]`
	lines, ok := xrayJSONToLinks([]byte(doc), maxNodes)
	if !ok || len(lines) != 5 {
		t.Fatalf("ok=%v %q", ok, lines)
	}
	want := []string{
		"vless://0f6c1b2a-1111-2222-3333-444455556666@nl.example.net:443?type=tcp&security=reality&sni=a.com&pbk=PBK&fp=chrome&sid=01&spx=&pqv=&encryption=none&flow=xtls-rprx-vision&fm=%7B%7D#Prof%20%2F%20nl",
		"vmess://0f6c1b2a-1111-2222-3333-444455556666@de.example:8443?type=ws&path=%2Fws&host=cdn.example&security=tls&sni=cdn.example&fp=chrome&alpn=h2%2Chttp%2F1.1&ech=&pcs=&vcn=&fm=%7B%7D&encryption=auto#Prof%20%2F%20de",
		"trojan://p%40ss%20word@tr.example:443?type=grpc&serviceName=svc&host=&mode=multi&security=tls&sni=&fp=chrome&alpn=&ech=&pcs=&vcn=&fm=%7B%7D#Prof%20%2F%203",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:secret")) + "@[2001:4860::8888]:8388#Prof%20%2F%20ss",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d:\n got %s\nwant %s", i, lines[i], w)
		}
	}
	if !strings.HasPrefix(lines[4], "vless://abc@x.example:443?type=xhttp&path=%2Fx&host=&mode=packet-up&extra=") {
		t.Errorf("xhttp: %s", lines[4])
	}
	// Every generated link is a valid BROray server.
	for i, l := range lines {
		if _, err := servers.ParseLink(l, fmt.Sprintf("x-%d", i), nil); err != nil {
			t.Errorf("ParseLink(%s): %v", l, err)
		}
	}

	unsupported := []struct{ name, ob string }{
		{"hysteria2 outbound", `{"protocol":"hysteria2","settings":{"servers":[{"address":"h.example","port":443}]}}`},
		{"allowInsecure", `{"protocol":"trojan","settings":{"address":"h.example","port":443,"password":"x"},"streamSettings":{"security":"tls","tlsSettings":{"allowInsecure":true}}}`},
		{"sockopt", `{"protocol":"trojan","settings":{"address":"h.example","port":443,"password":"x"},"streamSettings":{"sockopt":{"mark":1}}}`},
		{"reality over ws", `{"protocol":"vless","settings":{"address":"h","port":1,"id":"a"},"streamSettings":{"network":"ws","security":"reality","realitySettings":{"serverName":"a","publicKey":"k"}}}`},
		{"vless flow without tls", `{"protocol":"vless","settings":{"address":"h","port":1,"id":"a","flow":"xtls-rprx-vision"}}`},
		{"vmess alterId", `{"protocol":"vmess","settings":{"address":"h","port":1,"id":"a","alterId":4}}`},
		{"ss over ws", `{"protocol":"shadowsocks","settings":{"address":"h","port":1,"method":"m","password":"p"},"streamSettings":{"network":"ws"}}`},
		{"bad port", `{"protocol":"trojan","settings":{"address":"h","port":70000,"password":"x"}}`},
		{"bad address", `{"protocol":"trojan","settings":{"address":"h/x","port":1,"password":"x"}}`},
		{"one bad user", `{"protocol":"vless","settings":{"vnext":[{"address":"h","port":1,"users":[{"id":"ok"},{"id":"bad id"}]}]}}`},
		{"vless fragment ok but unknown key", `{"protocol":"vless","mux":{},"settings":{"address":"h","port":1,"id":"a"}}`},
		{"bad remarks type", `{"protocol":"trojan","tag":5,"settings":{"address":"h","port":1,"password":"x"}}`},
		{"xhttp GET upload not packet-up", `{"protocol":"vless","settings":{"address":"h","port":1,"id":"a"},"streamSettings":{"network":"xhttp","xhttpSettings":{"mode":"stream-up","uplinkHTTPMethod":"get"}}}`},
	}
	for _, u := range unsupported {
		lines, ok := xrayJSONToLinks([]byte(`{"outbounds":[`+u.ob+`]}`), maxNodes)
		if !ok || len(lines) != 1 || !strings.HasPrefix(lines[0], unsupportedMarker) {
			t.Errorf("%s: ok=%v %q", u.name, ok, lines)
		}
	}
	// single outbound: name without suffix; remarks absent → tag; neither → "Сервер"
	l, _ := xrayJSONToLinks([]byte(`{"outbounds":[{"protocol":"trojan","settings":{"address":"h","port":1,"password":"x"}}]}`), maxNodes)
	if l[0] != "trojan://x@h:1?type=tcp&security=none&fm=%7B%7D#"+uriEscape("Сервер") {
		t.Errorf("default name: %s", l[0])
	}
	// limit max+1 lines
	many := strings.Repeat(`{"protocol":"trojan","settings":{"address":"h","port":1,"password":"x"}},`, 600)
	l, _ = xrayJSONToLinks([]byte(`{"outbounds":[`+strings.TrimSuffix(many, ",")+`]}`), maxNodes)
	if len(l) != 501 {
		t.Errorf("limit: %d", len(l))
	}
	if _, err := extractNodes([]byte(`{"outbounds":[`+strings.TrimSuffix(many, ",")+`]}`), newMetadata()); err == nil || err.Code != "CONTENT_TOO_LARGE" {
		t.Errorf("json too many: %v", err)
	}
}

func TestStageNodes(t *testing.T) {
	doc := `{"outbounds":[` + obTrojan + `,{"protocol":"socks","settings":{}}]}`
	ex, err := extractNodes([]byte(doc), newMetadata())
	if err != nil {
		t.Fatal(err)
	}
	st, serr := stageNodes("sub-0123456789abcdef", ex, true, nil)
	if serr != nil || st.Accepted != 1 || st.Rejected != 1 || st.Received != 2 {
		t.Fatalf("%v %+v", serr, st)
	}
	if st.Warnings[0] != jsonImportWarning || st.Warnings[1] != "Узел 2: параметры JSON-конфигурации не поддерживаются." {
		t.Fatalf("%q", st.Warnings)
	}
	lines := &extracted{Lines: []string{linkA, linkA, "tuic://x@h:1", "junk", "vless://bad", "trojan://x@0.0.0.0:1#App not supported"}}
	st, serr = stageNodes("sub-0123456789abcdef", lines, false, nil)
	if serr != nil || st.Accepted != 1 || st.Rejected != 5 || st.Parsed != 3 {
		t.Fatalf("%v %+v", serr, st)
	}
	want := []string{
		"Узел 2: дубликат уже полученного сервера.",
		"Узел 3: Ошибка: парсер TUIC ещё не реализован",
		"Узел 4: Ошибка: для протокола  отсутствует импортёр",
	}
	for i, w := range want {
		if st.Warnings[i] != w {
			t.Errorf("warning %d: %q", i, st.Warnings[i])
		}
	}
	if st.Warnings[4] != "Узел 6: провайдер вернул служебный отказ вместо сервера." {
		t.Errorf("denial: %q", st.Warnings[4])
	}
	rec := st.Records[0]
	src := rec["source"].(map[string]any)
	key, _ := servers.CanonicalKey(rec)
	if rec["id"] != "subscription-sub-0123456789abcdef-"+key[:16] || src["importKey"] != key || src["nodeIndex"] != 1 || src["enabled"] != false {
		t.Fatalf("record: %v", rec)
	}
	_, serr = stageNodes("sub-x", &extracted{Lines: []string{"trojan://x@0.0.0.0:1#Приложение не поддерживается"}}, true, nil)
	if serr == nil || serr.Code != "SUBSCRIPTION_ACCESS_DENIED" {
		t.Fatalf("access denied: %v", serr)
	}
	_, serr = stageNodes("sub-x", &extracted{Lines: []string{"junk"}}, true, nil)
	if serr == nil || serr.Code != "NO_VALID_NODES" || serr.Msg != "Подписка не содержит ни одного поддерживаемого и валидного узла." {
		t.Fatalf("no nodes: %v", serr)
	}
}

func TestProviderMetadata(t *testing.T) {
	b64 := func(s string) string { return "base64:" + base64.StdEncoding.EncodeToString([]byte(s)) }
	md := newMetadata()
	md.apply(metaRecordsFromLines([]string{
		"Profile-Title: " + b64("Премиум"),
		"subscription-userinfo: upload=1; download=2; total=107374182400; expire=1767225600",
		"profile-update-interval: 12",
		"support-url: https://t.me/support",
		"profile-web-page-url: https://user@x.example/",
		"announce: hello",
		"routing: something",
		"announce: hello ",
	}))
	want := map[string]any{"schemaVersion": 1, "title": "Премиум",
		"usage":                  map[string]any{"upload": int64(1), "download": int64(2), "total": int64(107374182400), "expire": int64(1767225600)},
		"suggestedUpdateMinutes": int64(720), "supportUrl": "https://t.me/support", "announcement": "hello",
		"invalidFields": []string{"profile-web-page-url"}, "ignoredDirectives": []string{"routing"}}
	if !reflect.DeepEqual(md.object(), want) {
		t.Fatalf("got %#v", md.object())
	}
	// body lines: '#' prefix, conflicting duplicate → invalid; later sources override
	md.apply(metaRecordsFromLines([]string{"# profile-title: A", "#profile-title: B", "  #  announce-url: http://a.example/x"}))
	o := md.object()
	if o["title"] != "Премиум" || o["announcementUrl"] != "http://a.example/x" || !reflect.DeepEqual(o["invalidFields"], []string{"profile-title", "profile-web-page-url"}) {
		t.Fatalf("merge: %#v", o)
	}
	bad := []struct{ key, value string }{
		{"profile-update-interval", "0"}, {"profile-update-interval", "169"}, {"profile-update-interval", "1.5"},
		{"subscription-userinfo", "upload=1;upload=2"}, {"subscription-userinfo", "foo=1"}, {"subscription-userinfo", "upload=-1"},
		{"subscription-userinfo", ""}, {"profile-title", strings.Repeat("x", 129)}, {"profile-title", "base64:@@@"},
		{"profile-title", "base64:YQ"}, {"profile-title", "base64:YQ=="[:9] + "="}, {"support-url", "ftp://x"}, {"support-url", "https://"},
		{"support-url", "https://a.example/<x>"}, {"profile-title", "a\tb"},
	}
	for _, b := range bad {
		md := newMetadata()
		md.apply([]metaRecord{{b.key, " " + b.value}})
		if b.key == "profile-title" && b.value == "base64:YQ" {
			// unpadded canonical form is valid
			if md.object()["title"] != "a" {
				t.Errorf("base64 unpadded: %#v", md.object())
			}
			continue
		}
		if !reflect.DeepEqual(md.object()["invalidFields"], []string{b.key}) {
			t.Errorf("%s=%q: %#v", b.key, b.value, md.object())
		}
	}
	empty := newMetadata()
	empty.apply(metaRecordsFromLines([]string{linkA, "x-other: 1"}))
	if !reflect.DeepEqual(empty.object(), map[string]any{"schemaVersion": 1}) {
		t.Fatalf("empty: %#v", empty.object())
	}
	if got := stripMetadata([]string{"#c", "Profile-Title: x", linkA, "routing"}); !reflect.DeepEqual(got, []string{linkA}) {
		t.Fatalf("strip: %q", got)
	}
}

func TestURLRules(t *testing.T) {
	masks := map[string]string{
		"https://example.com/sub/AbCdEf123?x=1": "https://example.com/sub/***?***",
		"https://h.io/api/v1/abcdefghijkl":      "https://h.io/api/v1/***",
		"https://h.io/x":                        "https://h.io/x",
		"https://u:p@h.io/x#frag":               "https://***@h.io/x",
		"ftp://h.io/x":                          "***",
		"https://h.io":                          "https://h.io",
		"https://h.io/sub/":                     "https://h.io/sub/",
		"http://h.io/s/k?":                      "http://h.io/s/***?***",
	}
	for in, want := range masks {
		if got := maskURL(in); got != want {
			t.Errorf("mask %s = %s, want %s", in, got, want)
		}
	}
	errs := map[string]string{
		"ftp://x":             "URL подписки должен использовать HTTP или HTTPS.",
		"https://a b":         "URL подписки содержит пробельные символы.",
		"https://a'b":         "URL подписки содержит недопустимые символы.",
		"https:///x":          "В URL подписки отсутствует адрес сервера.",
		"https://u@h/x":       "Данные пользователя в URL подписки запрещены.",
		"https://:80/":        "В URL подписки отсутствует имя хоста.",
		"https://LOCALHOST/":  "Локальные и служебные адреса запрещены.",
		"https://router.lan/": "Локальные и служебные адреса запрещены.",
		"https://metadata/":   "Локальные и служебные адреса запрещены.",
		"https://h:x/":        "Порт в URL подписки имеет неправильный формат.",
		"https://h:70000/":    "Порт в URL подписки находится вне допустимого диапазона.",
		"https://h:0/":        "Порт в URL подписки находится вне допустимого диапазона.",
	}
	for in, want := range errs {
		if _, e := parseURL(in); e == nil || e.Msg != want {
			t.Errorf("%s: %v", in, e)
		}
	}
	if p, e := parseURL("https://[2001:db8::1]:8443/x"); e != nil || p.Host != "2001:db8::1" || p.Port != "8443" {
		t.Errorf("v6: %+v %v", p, e)
	}
	if p, _ := parseURL("https://h:/x"); p.Port != "443" {
		t.Errorf("empty port: %+v", p)
	}
	if p, _ := parseURL("http://H.Example/x"); p.Host != "h.example" || p.Port != "80" {
		t.Errorf("default port: %+v", p)
	}
	public := map[string]bool{"8.8.8.8": true, "1.1.1.1": true, "10.0.0.1": false, "127.0.0.1": false, "169.254.1.1": false,
		"100.64.0.1": false, "100.128.0.1": true, "172.16.0.1": false, "172.32.0.1": true, "192.168.1.1": false, "224.0.0.1": false,
		"255.255.255.255": false, "0.0.0.0": false, "198.18.0.1": false, "192.0.2.1": false, "203.0.113.5": false,
		"2001:4860::8888": true, "2001:db8::1": false, "::1": false, "::": false, "fd00::1": false, "fe80::1": false, "ff02::1": false,
		"::ffff:8.8.8.8": false, "3fff::1": true, "4000::1": false}
	for ip, want := range public {
		if got := ipPublic(netip.MustParseAddr(ip)); got != want {
			t.Errorf("ipPublic(%s)=%v", ip, got)
		}
	}
	lit := map[string]bool{"1.2.3.4": true, "01.2.3.4": false, "1.2.3": false, "256.1.1.1": false, "::1": true, "fe80::1%eth0": false}
	for h, valid := range lit {
		a, isLit := literalIP(h)
		if !isLit || a.IsValid() != valid {
			t.Errorf("literalIP(%s) = %v %v", h, a, isLit)
		}
	}
	if _, isLit := literalIP("sub.example"); isLit {
		t.Error("name taken as literal")
	}
	redirects := [][3]string{
		{"https://a/x/y?q", "https://b/z", "https://b/z"},
		{"https://a/x/y?q", "?p=1", "https://a/x/y?p=1"},
		{"https://a/x/y", "//b/c", "https://b/c"},
		{"https://a:8443/x/y", "/c", "https://a:8443/c"},
		{"https://a/x/y?q=/z", "c", "https://a/x/c"},
	}
	for _, r := range redirects {
		if got, _ := resolveRedirect(r[0], r[1]); got != r[2] {
			t.Errorf("redirect %s + %s = %s", r[0], r[1], got)
		}
	}
}

func TestDeviceFilter(t *testing.T) {
	got := deviceFilter(map[string]any{"hw_id": "KN-1010", "model": "Keenetic Giga (KN-1010)", "title": "4.2.3", "release": "4.02.C.3.0-0",
		"vendor": "Keenetic", "serial": "S123"})
	if !reflect.DeepEqual(got, map[string]string{"os": "KeeneticOS", "model": "KN-1010", "osVersion": "4.2.3"}) {
		t.Fatalf("%v", got)
	}
	if got := deviceFilter(map[string]any{"hw_id": "KN-1010", "model": "X (KN-2010)"}); !reflect.DeepEqual(got, map[string]string{"os": "KeeneticOS"}) {
		t.Fatalf("disagree: %v", got)
	}
	if got := deviceFilter(map[string]any{"hw_id": "KN-1010", "vendor": "Other"}); len(got) != 0 {
		t.Fatalf("other vendor: %v", got)
	}
	if got := deviceFilter(map[string]any{"vendor": "Keenetic", "title": "beta", "release": "4.1"}); got["osVersion"] != "4.1" {
		t.Fatalf("release: %v", got)
	}
}
