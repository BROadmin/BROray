package xrayschema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func object(t *testing.T, s string) map[string]any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPreserveTypedClientFields(t *testing.T) {
	s := `{"protocol":"vless","settings":{"address":"example.invalid","port":443,"id":"test","encryption":"none","testpre":4294967295},"streamSettings":{"network":"xhttp","security":"tls","tlsSettings":{"disableSystemRoot":false,"alpn":"h2,http/1.1","certificates":[{"certificate":["private-ca"],"usage":"verify"}]},"sockopt":{"tcpFastOpen":-1,"mark":42,"interface":"owned-interface","customSockopt":[{"system":"linux","network":"tcp","level":"6","opt":"1","value":"1","type":"int"}],"happyEyeballs":{"tryDelayMs":18446744073709551615,"prioritizeIPv6":false}},"xhttpSettings":{"extra":{"scMaxBufferedPosts":9223372036854775807,"downloadSettings":{"address":"download.invalid","port":443,"network":"xhttp","xhttpSettings":{"path":"/d"}}}}}}`
	in := object(t, s)
	got, e := Parse(in)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(in, got.Fields) || len(got.UnknownPaths) != 0 {
		t.Fatal("declared values were lost or misclassified")
	}
	got.Fields["protocol"] = "changed"
	if in["protocol"] != "vless" {
		t.Fatal("result aliases caller")
	}
}

func TestDynamicClientFieldTypes(t *testing.T) {
	for _, s := range []string{
		`{"protocol":"vless","settings":{"vnext":[{"address":"example.invalid","port":443,"users":[{"id":"id","testseed":[4294967295],"level":0}]}]}}`,
		`{"protocol":"vmess","settings":{"vnext":[{"address":"example.invalid","port":443,"users":[{"id":"id","experiments":"AuthenticatedLength","email":"private"}]}]}}`,
		`{"protocol":"http","settings":{"address":"example.invalid","port":443,"headers":{"Private-Header":"secret"}},"streamSettings":{"tcpSettings":{"header":{"type":"http","request":{"path":["/a","/b"],"headers":{"Host":"example.invalid"}}}},"finalmask":{"udp":[{"type":"realm","settings":{"tlsConfig":{"disableSystemRoot":false}}}]}}}`,
	} {
		if _, e := Parse(object(t, s)); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{
		`{"protocol":"vless","settings":{"testpre":4294967296}}`,
		`{"protocol":"vless","settings":{"testpre":"1"}}`,
		`{"protocol":"vless","settings":{"port":1.5}}`,
		`{"protocol":"vless","settings":{"vnext":[{"users":[{"testseed":[-1]}]}]}}`,
		`{"protocol":"http","settings":{"headers":{"X":false}}}`,
		`{"protocol":"http","streamSettings":{"tlsSettings":{"certificates":[{"certificate":[false]}]}}}`,
		`{"protocol":"http","streamSettings":{"xhttpSettings":{"extra":{"downloadSettings":{"port":65536}}}}}`,
		`{"protocol":"http","streamSettings":{"finalmask":{"udp":[{"type":"realm","settings":{"tlsConfig":{"disableSystemRoot":"false"}}}]}}}`,
	} {
		if _, e := Parse(object(t, s)); e == nil {
			t.Fatal("malformed known field accepted")
		}
	}
}

func TestUnknownFieldsRemainPrivate(t *testing.T) {
	in := object(t, `{"protocol":"vless","settings":{"password-private-extension":{"secret":"keep"}},"streamSettings":{"tlsSettings":{"private-extension":"keep"}}}`)
	n, e := Parse(in)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(in, n.Fields) || len(n.UnknownPaths) != 2 {
		t.Fatal("extension lost")
	}
	for _, p := range n.UnknownPaths {
		if strings.Contains(p, "private") || strings.Contains(p, "keep") {
			t.Fatal("private key escaped into diagnostics")
		}
	}
}

func TestAliasesAndBounds(t *testing.T) {
	n, e := Parse(object(t, `{"PROTOCOL":"http","Settings":{"port":443}}`))
	if e != nil || n.Fields["protocol"] != "http" {
		t.Fatal("Go JSON field matching lost", e)
	}
	if _, e = Parse(object(t, `{"protocol":"http","PROTOCOL":"socks"}`)); e == nil {
		t.Fatal("conflicting aliases accepted")
	}
	m := map[string]any{"protocol": "http"}
	m["extension"] = m
	if _, e = Parse(m); e == nil {
		t.Fatal("unbounded recursion")
	}
	if _, e = Parse(map[string]any{"protocol": "http", "extension": strings.Repeat("s", 262145)}); e == nil {
		t.Fatal("unbounded source")
	}
	if _, e = Parse(object(t, `{"protocol":"vless","streamSettings":{"xhttpSettings":{"xPaddingBytes":"2147483648"}}}`)); e == nil {
		t.Fatal("range overflow")
	}
}
