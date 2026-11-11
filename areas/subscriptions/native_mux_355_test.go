package subscriptions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestYour355SubscriptionMissingXDriveMux(t *testing.T) {
	native := map[string]any{"protocol": "vless", "tag": "invented-xdrive", "settings": map[string]any{"address": "provider.example.invalid", "port": 443, "id": "11111111-1111-4111-8111-111111111111", "encryption": "mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 43), "level": 8}, "streamSettings": map[string]any{"network": "xdrive", "security": "none", "xdriveSettings": map[string]any{"service": "google-drive", "remoteFolder": "invented-folder", "secrets": []any{"invented-client", "invented-secret", "invented-refresh"}}}}
	b, _ := json.Marshal(map[string]any{"outbounds": []any{native}})
	ex, e := extractNodes(b, newMetadata())
	if e != nil {
		t.Fatal(e)
	}
	st, e := stageNodes("test", ex, true, nil)
	if e != nil || st.Accepted != 1 || st.Rejected != 0 {
		t.Fatalf("%+v %v", st, e)
	}
	if !strings.Contains(strings.Join(st.Warnings, " "), "mux.enabled=true") {
		t.Fatal("subscription/UI warning lost")
	}
	for _, secret := range []string{"invented-client", "invented-secret", "invented-refresh"} {
		if strings.Contains(strings.Join(st.Warnings, " "), secret) {
			t.Fatal("secret in warning")
		}
	}
	ordinary := `{"outbounds":[{"protocol":"vless","settings":{"address":"ordinary.example.invalid","port":443,"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}}]}`
	ex, e = extractNodes([]byte(ordinary), newMetadata())
	if e != nil {
		t.Fatal(e)
	}
	st, e = stageNodes("test", ex, true, nil)
	if e != nil || st.Accepted != 1 {
		t.Fatal("ordinary VLESS import")
	}
	if _, has := st.Records[0]["mux"]; has {
		t.Fatal("ordinary VLESS mux invented")
	}
}
