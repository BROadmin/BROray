package subscriptions

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func sharedAmneziaContainer(t *testing.T, kind, endpoint string) map[string]any {
	t.Helper()
	native := strings.ReplaceAll(sharedAWGINI("Jc = 4"), "awg.example.test:443", endpoint)
	return map[string]any{"container": kind, "awg": map[string]any{"last_config": sharedFormsJSON(t, map[string]any{"config": native})}}
}

func sharedAmneziaForms(t *testing.T, containers []any) map[string]string {
	t.Helper()
	raw := sharedFormsJSON(t, map[string]any{"description": "Synthetic native export", "containers": containers})
	var compressed bytes.Buffer
	if err := binary.Write(&compressed, binary.BigEndian, uint32(len(raw))); err != nil {
		t.Fatal("could not write synthetic compression header")
	}
	z := zlib.NewWriter(&compressed)
	if _, err := z.Write([]byte(raw)); err != nil || z.Close() != nil {
		t.Fatal("could not compress synthetic native document")
	}
	return map[string]string{
		"raw-json":      raw,
		"base64-json":   base64.StdEncoding.EncodeToString([]byte(raw)),
		"vpn-json":      "vpn://" + base64.RawURLEncoding.EncodeToString([]byte(raw)),
		"vpn-qcompress": "vpn://" + base64.RawURLEncoding.EncodeToString(compressed.Bytes()),
	}
}

func TestSharedImportAmneziaContainerSingletonForms(t *testing.T) {
	forms := sharedAmneziaForms(t, []any{sharedAmneziaContainer(t, "amnezia-awg2", "awg.example.test:443")})
	control, err := servers.ParseLink(forms["vpn-json"], "amnezia-control", nil)
	if err != nil {
		t.Fatalf("existing singleton native control rejected: %v", err)
	}
	wantKey, err := servers.CanonicalKey(control)
	if err != nil {
		t.Fatal("native control identity failed")
	}
	for name, body := range forms {
		t.Run(name, func(t *testing.T) {
			manual, err := servers.ParseLink(body, "amnezia-singleton", nil)
			if err != nil {
				t.Fatalf("singleton native document rejected: %v", err)
			}
			st := sharedFormsStage(t, body)
			if st.Received != 1 || st.Accepted != 1 {
				t.Fatal("singleton native document changed node count")
			}
			for _, rec := range []servers.Record{manual, st.Records[0]} {
				key, err := servers.CanonicalKey(rec)
				if err != nil || key != wantKey || rec["protocol"] != "amneziawg" {
					t.Fatal("native envelope changed connection identity")
				}
				if _, err := servers.Outbound(rec); err == nil {
					t.Fatal("native AWG unexpectedly became executable")
				}
			}
		})
	}
}

func TestSharedImportAmneziaContainerEnumeration(t *testing.T) {
	containers := []any{
		sharedAmneziaContainer(t, "amnezia-awg", "first.example.test:443"),
		sharedAmneziaContainer(t, "amnezia-awg2", "second.example.test:8443"),
	}
	for name, body := range sharedAmneziaForms(t, containers) {
		t.Run(name, func(t *testing.T) {
			if _, err := servers.ParseLink(body, "amnezia-many", nil); err == nil {
				t.Fatal("manual singleton silently chose one native container")
			}
			st := sharedFormsStage(t, body)
			if st.Received != 2 || st.Parsed != 2 || st.Accepted != 2 || st.Rejected != 0 {
				t.Fatal("native containers were omitted, merged or counted incorrectly")
			}
			for i, rec := range st.Records {
				want := []string{"first.example.test", "second.example.test"}[i]
				if rec["protocol"] != "amneziawg" || rec["address"] != want {
					t.Fatal("native enumeration lost container order or endpoint")
				}
				private := rec["amneziawg"].(map[string]any)
				if !strings.Contains(private["nativeConfig"].(string), want) {
					t.Fatal("native configuration was discarded")
				}
				encoded := sharedFormsJSON(t, servers.MaskRecord(rec))
				if strings.Contains(encoded, parserCoverageWGKey) || strings.Contains(encoded, "last_config") {
					t.Fatal("private native source escaped public mask")
				}
			}
		})
	}
}

func TestSharedImportAmneziaContainerNoPartialSalvage(t *testing.T) {
	for name, bad := range map[string]any{
		"unknown":   map[string]any{"container": "synthetic-unknown", "url": "https://provider.example.test/synthetic-private"},
		"malformed": map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": "synthetic-private-invalid"}},
		"scalar":    "synthetic-private-container",
	} {
		t.Run(name, func(t *testing.T) {
			for _, body := range sharedAmneziaForms(t, []any{sharedAmneziaContainer(t, "amnezia-awg", "first.example.test:443"), bad}) {
				if _, err := servers.ParseLink(body, "amnezia-invalid", nil); err == nil || strings.Contains(err.Error(), "synthetic-private") {
					t.Fatal("invalid native container accepted or diagnostics exposed private input")
				}
				if ex, err := extractNodes([]byte(body), newMetadata()); err == nil {
					if st, stageErr := stageNodes("amnezia-invalid", ex, true, nil); stageErr == nil || st.Accepted != 0 {
						t.Fatal("invalid native document silently imported a subset")
					}
				}
			}
		})
	}
}
