package subscriptions

import (
	"encoding/base64"
	"testing"
)

func TestDecode(t *testing.T) {
	plain := "vless://u@1.2.3.4:443#a\n# comment\ntrojan://p@5.6.7.8:443#b\n"
	l, err := Decode([]byte(plain))
	if err != nil || len(l) != 2 {
		t.Fatalf("plain: %v %v", err, l)
	}
	l, err = Decode([]byte(base64.StdEncoding.EncodeToString([]byte(plain))))
	if err != nil || len(l) != 2 {
		t.Fatalf("base64: %v %v", err, l)
	}
	l, err = Decode([]byte(`["ss://x@1.2.3.4:1#a","vmess://abc"]`))
	if err != nil || len(l) != 2 {
		t.Fatalf("json: %v %v", err, l)
	}
	if _, err := Decode([]byte("<!DOCTYPE html><html></html>")); err == nil {
		t.Fatal("html accepted")
	}
	if _, err := Decode([]byte("just text")); err == nil {
		t.Fatal("garbage accepted")
	}
}
