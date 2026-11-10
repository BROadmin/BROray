package servers

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMLKEMMetadataWarning(t *testing.T) {
	base := "vless://test@example.com:443?type=xhttp&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0102"
	plain, err := ParseLink(base, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOutbound, err := Outbound(plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"true", "false"} {
		t.Run(value, func(t *testing.T) {
			r, err := ParseLink(base+"&support-x25519mlkem768="+value, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			warnings, _ := r["importWarnings"].([]any)
			if len(warnings) != 1 || !strings.Contains(warnings[0].(string), "Mihomo") || !strings.Contains(warnings[0].(string), "TLS-профилем и сервером") {
				t.Fatalf("warning must explain the foreign option without claiming negotiated crypto: %v", warnings)
			}
			out, err := Outbound(r)
			if err != nil || !reflect.DeepEqual(out, wantOutbound) {
				t.Fatalf("metadata changed outbound: %v", err)
			}
		})
	}
}

func TestSavedMLKEMWarningPresentation(t *testing.T) {
	old := "Поле URI support-x25519mlkem768 не применяется; параметры соединения берутся из поддерживаемых полей."
	var saved Record
	if err := json.Unmarshal([]byte(`{"id":"fixture","importWarnings":[]}`), &saved); err != nil {
		t.Fatal(err)
	}
	saved["importWarnings"] = []any{old, "Другое предупреждение"}
	before, _ := json.Marshal(saved)
	shown := MaskRecord(saved)["importWarnings"].([]any)
	if len(shown) != 2 || !strings.Contains(shown[0].(string), "Mihomo") || shown[1] != "Другое предупреждение" {
		t.Fatalf("stale stored warning shown: %v", shown)
	}
	after, _ := json.Marshal(saved)
	if string(before) != string(after) {
		t.Fatal("presentation rewrote saved data")
	}
}
