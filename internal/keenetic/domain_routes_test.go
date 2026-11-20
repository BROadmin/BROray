package keenetic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNativeDomainRouteReadbackAndOwnership(t *testing.T) {
	const group = "broray-fixture"
	const index = "38dab1834f6f9435395776ea9d6b02a1"
	const name = "BROray Fixture"
	var hasList, hasRoute, drift bool
	var writes, saves int
	var creationPending, removalPending, routeRemovalPending bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requests []json.RawMessage
		if r.Method != http.MethodPost || r.URL.Path != "/rci/" ||
			json.NewDecoder(r.Body).Decode(&requests) != nil {
			http.Error(w, "bad fixture request", http.StatusBadRequest)
			return
		}
		switch first := string(requests[0]); {
		case strings.Contains(first, `"show"`):
			groups := map[string]any{}
			if hasList {
				address := "owned.example"
				if drift {
					address = "foreign.example"
				}
				groups[group] = map[string]any{"description": name, "include": []map[string]string{{"address": address}}}
			}
			routes := []DomainRouteBinding{}
			if hasRoute {
				routes = append(routes, DomainRouteBinding{Group: group, Interface: "Proxy1", Index: index, Auto: true, Reject: true})
			}
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"show": map[string]any{"sc": map[string]any{"object-group": map[string]any{"fqdn": groups}}}},
				map[string]any{"show": map[string]any{"sc": map[string]any{"dns-proxy": map[string]any{"route": routes}}}},
			})
		case strings.Contains(first, `"parse":"system configuration save"`):
			saves++
			if creationPending {
				hasList = true
				creationPending = false
			}
			if routeRemovalPending {
				hasRoute = false
				routeRemovalPending = false
			}
			if removalPending {
				hasList = false
				removalPending = false
			}
			_, _ = w.Write([]byte(`[{"parse":{"status":[{"status":"message","message":"saved"}]}}]`))
		case strings.Contains(first, `"parse":"dns-proxy route object-group `+group+` Proxy1 auto reject"`):
			writes++
			hasRoute = true
			_, _ = w.Write([]byte(`[{"parse":{"status":[{"status":"message"}]}}]`))
		case strings.Contains(first, `"parse":"no dns-proxy route object-group `+group+` Proxy1"`):
			writes++
			routeRemovalPending = true
			_, _ = w.Write([]byte(`[{"parse":{"status":[{"status":"message"}]}}]`))
		case strings.Contains(first, `"object-group"`) && strings.Contains(first, group):
			if strings.Contains(first, `"no":true`) {
				if hasRoute {
					t.Error("list deletion preceded route removal")
				}
				// Keenetic projects the removed group through show/sc until save.
				removalPending = true
			} else {
				creationPending = true
			}
			_, _ = w.Write([]byte(`[{}]`))
		default:
			http.Error(w, "unknown command", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	r := NewRCI(server.URL)
	domains := []string{"owned.example"}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	binding, err := r.ChangeDomainRoute(ctx, group, name, "Proxy1", domains, nil)
	if err != nil || binding.Index != index || !binding.Reject {
		t.Fatal("create not confirmed", binding, err)
	}
	if _, err := r.ChangeDomainRoute(t.Context(), group, name, "Proxy1", domains, nil); err == nil {
		t.Fatal("occupied name accepted")
	}
	drift = true
	if _, err := r.ChangeDomainRoute(t.Context(), group, name, "Proxy1", domains, &binding); err == nil || writes != 1 {
		t.Fatal("externally changed list was deleted", err, writes)
	}
	drift = false
	if _, err := r.ChangeDomainRoute(t.Context(), group, name, "Proxy1", domains, &binding); err != nil || writes != 2 || saves != 4 {
		t.Fatal("owned deletion not confirmed", err, writes, saves)
	}
	if _, err := r.ChangeDomainRoute(context.Background(), group, name, "Proxy1", []string{"owned.example\nno interface Proxy1"}, nil); err == nil || writes != 2 {
		t.Fatal("malformed domain sent to router", err)
	}
}
