package routes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/BROadmin/broray/internal/keenetic"
)

func TestPremiumIPRequestGuard(t *testing.T) {
	env := newEnv(t)
	path := "/api/routes/premium-ip.cgi"
	env.wantErr(env.callH("POST", path, `{"action":"check","address":"127.0.0.1"}`, policyHeaders),
		http.StatusBadRequest, "PREMIUM_IP_INVALID")
	env.wantErr(env.callH("POST", path, `{"action":"check","address":"1.1.1.1"}`, nil),
		http.StatusForbidden, "CSRF_REJECTED")
	env.wantErr(env.callH("POST", path, `{"action":"add","token":"absent"}`, policyHeaders),
		http.StatusConflict, "PREMIUM_CHECK_EXPIRED")
}

func TestNativeDNSDraftRevisionAndMixedPreview(t *testing.T) {
	env := newEnv(t)
	revision := policyView(env)["revision"].(string)
	const path = "/api/routes/native-dns-draft.cgi"
	id := "own-mixed"
	post := func(fields map[string]string) resp {
		fields["id"], fields["policyRevision"] = id, revision
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return env.callH("POST", path, string(body), policyHeaders)
	}
	input := map[string]string{"action": "save", "expectedHash": "", "name": "Mixed",
		"targets": "domain:example.com\n1.1.1.1\n2606:4700::1111\n2606:4700:0:0:0:0:0:1111"}
	got := env.mustOK(post(input))
	preview, enabled, err := env.e.nativeOwnPreview(t.Context(), id)
	if err != nil || enabled || len(preview.Domains) != 3 || preview.Domains[1] != "2606:4700::1111" || preview.Hash != got["sourceSHA256"] {
		t.Fatalf("unexpected mixed preview: %+v %t, %v", preview, enabled, err)
	}
	env.wantErr(post(input), 409, "NATIVE_DNS_DRAFT_CHANGED")
	env.mustOK(post(map[string]string{"action": "delete", "expectedHash": preview.Hash}))
	env.wantErr(env.callH("POST", "/api/routes/native-domain-preview.cgi", `{"id":"own-mixed"}`, policyHeaders),
		400, "DOMAIN_PRESET_INVALID")
}

func TestPremiumIPSocksRequiresRemoteResponse(t *testing.T) {
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()
	address := netip.MustParseAddr("2606:4700::1111")
	for _, withTLS := range []bool{false, true} {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			var greeting [3]byte
			var request [22]byte
			if _, err := io.ReadFull(conn, greeting[:]); err != nil || greeting != [3]byte{5, 1, 0} {
				return
			}
			if _, err := conn.Write([]byte{5, 0}); err != nil {
				return
			}
			if _, err := io.ReadFull(conn, request[:]); err != nil || request[3] != 4 ||
				[16]byte(request[4:20]) != address.As16() || request[20] != 1 || request[21] != 187 {
				t.Error("wrong numeric SOCKS target")
				return
			}
			if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
				return
			}
			if withTLS {
				server := tls.Server(conn, fixture.TLS)
				_ = server.Handshake()
			}
		}()
		result := premiumTCPSOCKS(context.Background(), listener.Addr().String(), address)
		listener.Close()
		if (result.State == "available") != withTLS {
			t.Fatalf("TLS response=%t, got %+v", withTLS, result)
		}
	}
}

func TestPremiumIPConflictHonorsDifferentInterface(t *testing.T) {
	var snapshot keenetic.DomainRouteSnapshot
	if err := json.Unmarshal([]byte(`{"Lists":{"group":{"include":[{"address":"2606:4700::/32"}]}},"Bindings":[{"group":"group","interface":"Proxy9","reject":true}]}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("2606:4700::1111")
	if !premiumIPConflict(snapshot, "Proxy0", address) || premiumIPConflict(snapshot, "Proxy9", address) {
		t.Fatal("conflict failed to distinguish assigned interface")
	}
	snapshot.Bindings[0].Disabled = true
	if premiumIPConflict(snapshot, "Proxy0", address) {
		t.Fatal("disabled route should not block")
	}
}
