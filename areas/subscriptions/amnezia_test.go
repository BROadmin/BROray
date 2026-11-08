package subscriptions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func subscriptionAWG(h string) string {
	client, _ := json.Marshal(map[string]any{"config": "[Interface]\nPrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress = 10.0.0.2/32\nH1 = " + h + "\n[Peer]\nPublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nEndpoint = awg.example:443\nAllowedIPs = 0.0.0.0/0\n"})
	envelope, _ := json.Marshal(map[string]any{"description": "AWG", "containers": []any{map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(client)}}}})
	return "vpn://" + base64.RawURLEncoding.EncodeToString(envelope)
}

func TestAmneziaSubscriptionAcceptedWithWarning(t *testing.T) {
	for _, body := range []string{subscriptionAWG("123"), "trojan://synthetic@normal.example:443\n" + subscriptionAWG("123")} {
		ex, err := extractNodes([]byte(body), newMetadata())
		if err != nil {
			t.Fatal(err)
		}
		st, e := stageNodes(subX, ex, true, nil)
		if e != nil {
			t.Fatal(e)
		}
		if st.Rejected != 0 || st.Accepted != len(ex.Lines) {
			t.Fatalf("valid AWG rejected: %+v", st)
		}
		if !strings.Contains(strings.Join(st.Warnings, " "), "Подключение пока не поддерживается") {
			t.Fatal("AWG missing explicit runtime warning")
		}
	}
}

func TestAmneziaBase64INISubscription(t *testing.T) {
	ini := "[Interface]\nPrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress = 10.0.0.2/32\nJc = 4\n[Peer]\nPublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nEndpoint = awg.example:443\nAllowedIPs = 0.0.0.0/0\n"
	body := "trojan://synthetic@normal.example:443\nvpn://" + base64.RawURLEncoding.EncodeToString([]byte(ini))
	ex, err := extractNodes([]byte(body), newMetadata())
	if err != nil {
		t.Fatal(err)
	}
	st, err := stageNodes(subX, ex, true, nil)
	if err != nil || st.Accepted != 2 || st.Rejected != 0 {
		t.Fatalf("native AWG made subscription partial: %+v %v", st, err)
	}
	e := newEnv(t)
	r, syncErr := e.s.syncCatalog(context.Background(), subX, st.Records, true, "manual", "replace")
	if syncErr != nil || r.Total != 2 || r.Added != 2 {
		t.Fatalf("native AWG not persisted: %+v %v", r, syncErr)
	}
}

func TestAmneziaSubscriptionRotationAndRemoval(t *testing.T) {
	e := newEnv(t)
	links := []string{"trojan://synthetic@normal.example:443", subscriptionAWG("123")}
	first, err := e.sync(t, "replace", links...)
	if err != nil || first.Added != 2 {
		t.Fatalf("initial: %v", err)
	}
	var oldID string
	for _, id := range liveIDs(t, e) {
		r, _ := servers.ReadRecord(e.d, id)
		if r["protocol"] == "amneziawg" {
			oldID = id
		}
	}
	second, err := e.sync(t, "replace", links[0], subscriptionAWG("456"))
	if err != nil || second.Updated != 1 || second.Added != 0 {
		t.Fatalf("rotation: %v %+v", err, second)
	}
	if !servers.RecordExists(e.d, oldID) {
		t.Fatal("rotation changed AWG ID")
	}
	third, err := e.sync(t, "replace", links[0])
	if err != nil || third.Removed != 1 || servers.RecordExists(e.d, oldID) {
		t.Fatalf("removed node remains: %v %+v", err, third)
	}
	if len(e.reapplied) != 0 {
		t.Fatal("parse-only AWG touched active connection")
	}
}

func TestAmneziaCorruptionKeepsPartialProtection(t *testing.T) {
	e := newEnv(t)
	_, err := e.sync(t, "replace", "trojan://synthetic@old.example:443", subscriptionAWG("123"))
	if err != nil {
		t.Fatal(err)
	}
	st, se := stageNodes(subX, &extracted{Lines: []string{subscriptionAWG("123"), "vpn://corrupted"}}, true, nil)
	if se != nil || st.Rejected != 1 || st.Accepted != 1 {
		t.Fatalf("bad container not rejected: %v", se)
	}
	res, err := e.s.syncCatalog(context.Background(), subX, st.Records, true, "partial", "retain-unmatched")
	if err != nil || res.Retained != 1 || res.Removed != 0 {
		t.Fatalf("partial response deleted old server: %v %+v", err, res)
	}
}
