package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/keenetic"
)

func TestPremiumDomainRequestGuard(t *testing.T) {
	env := newEnv(t)
	const endpoint = "/api/routes/premium-domain.cgi"
	bad := env.callH("POST", endpoint, `{"action":"check","domain":"https://example.com/path"}`, policyHeaders)
	if bad.status != http.StatusBadRequest {
		t.Fatalf("invalid input returned %d: %s", bad.status, bad.raw)
	}
	noOrigin := env.callH("POST", endpoint, `{"action":"check","domain":"example.com"}`, nil)
	if noOrigin.status != http.StatusForbidden {
		t.Fatalf("unguarded input returned %d: %s", noOrigin.status, noOrigin.raw)
	}
	withoutCheck := env.callH("POST", endpoint, `{"action":"add","token":"invalid"}`, policyHeaders)
	if withoutCheck.status != http.StatusConflict {
		t.Fatalf("unconsented action returned %d: %s", withoutCheck.status, withoutCheck.raw)
	}
}

func TestPremiumRejectsNonPublicDNSAnswers(t *testing.T) {
	lookup := func(_ context.Context, _, _ string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("192.168.1.12")}, nil
	}
	addrs, reason := premiumResolve(context.Background(), "example.com", lookup, nil)
	if len(addrs) != 0 || reason != "non-public" {
		t.Fatalf("expected mixed answer to be rejected: %v %q", addrs, reason)
	}
}

func TestPremiumListCreateAppendDuplicateAndRollback(t *testing.T) {
	testPremiumListCreateAppendDuplicateAndRollback(t, premiumDomainList,
		"orphan.example", "one.example", "two.example", "rejected.example")
}

func TestPremiumIPListCreateAppendDuplicateAndRollback(t *testing.T) {
	testPremiumListCreateAppendDuplicateAndRollback(t, premiumIPList,
		"8.8.4.4", "1.1.1.1", "2606:4700:4700::1111", "8.8.8.8")
}

func testPremiumListCreateAppendDuplicateAndRollback(t *testing.T, spec premiumListSpec, orphan, one, two, rejected string) {
	t.Helper()
	env := newEnv(t)
	const iface = "Proxy0"
	const index = "38dab1834f6f9435395776ea9d6b02a1"
	lists := make(map[string]json.RawMessage)
	routes := make(map[string]keenetic.DomainRouteBinding)
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch []map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil || len(batch) == 0 {
			http.Error(w, "invalid request", 400)
			return
		}
		if batch[0]["show"] != nil {
			bindings := make([]keenetic.DomainRouteBinding, 0, len(routes))
			for _, route := range routes {
				bindings = append(bindings, route)
			}
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"show": map[string]any{"sc": map[string]any{"object-group": map[string]any{"fqdn": lists}}}},
				map[string]any{"show": map[string]any{"sc": map[string]any{"dns-proxy": map[string]any{"route": bindings}}}},
			})
			return
		}
		if _, err := os.Stat(env.e.p(spec.pending)); err != nil {
			t.Error("router write without durable intent", err)
			http.Error(w, "no intent", 409)
			return
		}
		var command string
		if json.Unmarshal(batch[0]["parse"], &command) == nil {
			parts := strings.Fields(command)
			switch {
			case command == "system configuration save":
			case len(parts) == 7 && strings.HasPrefix(command, "dns-proxy route object-group "):
				group := parts[3]
				routes[group] = keenetic.DomainRouteBinding{Group: group, Interface: iface, Index: index, Auto: true, Reject: true}
			case len(parts) == 6 && strings.HasPrefix(command, "no dns-proxy route object-group "):
				delete(routes, parts[4])
			default:
				http.Error(w, "unsupported command", 400)
				return
			}
			_, _ = w.Write([]byte(`[{"parse":{"status":[{"status":"message"}]}}]`))
			return
		}
		var operation struct {
			FQDN json.RawMessage `json:"fqdn"`
		}
		_ = json.Unmarshal(batch[0]["object-group"], &operation)
		var created map[string]json.RawMessage
		if json.Unmarshal(operation.FQDN, &created) == nil {
			for group, list := range created {
				lists[group] = list
			}
		} else {
			var removed []struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(operation.FQDN, &removed) != nil || len(removed) != 1 {
				http.Error(w, "invalid change", 400)
				return
			}
			delete(lists, removed[0].Name)
		}
		_, _ = w.Write([]byte(`[{}]`))
	}))
	defer router.Close()
	rc := keenetic.NewRCI(router.URL)
	valid := func(context.Context) bool { return true }
	reject := func(context.Context) bool { return false }
	apply := func(target string, verify func(context.Context) bool) (nativeDomainRecord, bool, error) {
		return env.e.applyPremiumTargetList(t.Context(), rc, iface, target, verify, spec)
	}
	_, _, err := apply(orphan, reject)
	if err == nil || len(lists) != 0 || len(routes) != 0 {
		t.Fatal("failed first apply did not roll back", err, lists, routes)
	}
	first, duplicate, err := apply(one, valid)
	if err != nil || duplicate || len(lists) != 1 || len(routes) != 1 {
		t.Fatal("first list was not applied", err, first, duplicate, lists, routes)
	}
	second, duplicate, err := apply(two, valid)
	want := []string{one, two}
	if err != nil || duplicate || len(lists) != 1 || len(routes) != 1 || !slices.Equal(second.Preview.Domains, want) {
		t.Fatal("append did not extend the same list", err, second, duplicate)
	}
	_, duplicate, err = apply(one, valid)
	if err != nil || !duplicate {
		t.Fatal("duplicate was not idempotent", err, duplicate)
	}
	_, _, err = apply(rejected, reject)
	snapshot, readErr := rc.ReadDomainRoutes(t.Context())
	persisted, recordErr := readNativeDomainRecord(nativeDomainRecordPath(env.e.root, spec.id), spec.id)
	if err == nil || readErr != nil || recordErr != nil || persisted == nil ||
		!slices.Equal(persisted.Preview.Domains, want) ||
		!snapshot.ConfirmsDomainRoute(persisted.Description, want, *persisted.Binding) {
		t.Fatal("rollback did not restore previous list", err, readErr, recordErr, persisted)
	}
	if _, err := os.Stat(env.e.p(spec.pending)); !os.IsNotExist(err) {
		t.Fatal("recovered list retained a pending record", err)
	}
}
