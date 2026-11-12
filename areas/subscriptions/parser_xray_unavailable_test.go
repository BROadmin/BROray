package subscriptions

import (
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func subscriptionUnavailableXray(level bool) map[string]any {
	settings := map[string]any{"address": "source.example.test", "port": 443, "id": parserCoverageID, "encryption": "none"}
	out := map[string]any{"protocol": "vless", "tag": "Synthetic source", "settings": settings,
		"streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": "source.example.test"}}}
	if level {
		settings["level"] = 4
		out["synthetic-private-extension-name"] = map[string]any{"value": "synthetic-private-value"}
	} else {
		settings["reverse"] = map[string]any{"tag": "synthetic-private-bridge"}
	}
	return out
}

func TestSharedImportXrayUnavailableSubscription(t *testing.T) {
	first := subscriptionUnavailableXray(false)
	renamed := subscriptionUnavailableXray(false)
	renamed["tag"] = "Another display label"
	for _, body := range []string{
		sharedFormsJSON(t, []any{"vless://" + parserCoverageID + "@ordinary.example.test:443#Ordinary", first, subscriptionUnavailableXray(true), renamed}),
		sharedFormsJSON(t, map[string]any{"outbounds": []any{first, subscriptionUnavailableXray(true), renamed}}),
	} {
		st := sharedFormsStage(t, body)
		if st.Parsed != st.Received || st.Rejected != 0 || st.Accepted != st.Received-1 {
			t.Fatal("valid unavailable Xray nodes dropped or duplicate labels changed identity")
		}
		unavailable := 0
		for _, record := range st.Records {
			if record["importProfile"] == nil {
				continue
			}
			unavailable++
			if err := servers.Validate(record, "xray-source"); err != nil {
				t.Fatal("subscription source produced invalid private record")
			}
			if _, err := servers.Outbound(record); err == nil {
				t.Fatal("subscription unavailable source generated a reduced outbound")
			}
		}
		if unavailable != 2 || strings.Contains(strings.Join(st.Warnings, " "), "synthetic-private") {
			t.Fatal("private source count changed or warning exposed source")
		}
	}
}

func TestSharedImportXrayUnavailableNoEndpointSelection(t *testing.T) {
	for _, users := range []bool{false, true} {
		out := subscriptionUnavailableXray(false)
		endpoint := map[string]any{"address": "source.example.test", "port": 443, "users": []any{map[string]any{"id": parserCoverageID, "encryption": "none", "level": 4}}}
		endpoints := []any{endpoint}
		if users {
			endpoint["users"] = append(endpoint["users"].([]any), map[string]any{"id": "00000000-0000-4000-8000-000000000002", "encryption": "none"})
		} else {
			endpoints = append(endpoints, map[string]any{"address": "second.example.test", "port": 443, "users": []any{map[string]any{"id": parserCoverageID, "encryption": "none"}}})
		}
		out["settings"] = map[string]any{"vnext": endpoints}
		body := sharedFormsJSON(t, out)
		if _, err := servers.ParseLink(body, "xray-multiple", nil); err == nil {
			t.Fatal("manual unavailable source selected one expanded endpoint")
		}
		if ex, err := extractNodes([]byte(body), newMetadata()); err == nil {
			if st, stageErr := stageNodes("xray-multiple", ex, true, nil); stageErr == nil || st.Accepted != 0 {
				t.Fatal("unavailable source was duplicated across selected endpoints")
			}
		}
	}
}
