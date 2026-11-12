package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func sharedAWGINI(fields string) string {
	return "[Interface]\nPrivateKey = " + parserCoverageWGKey + "\nAddress = 192.0.2.2/32\n" + fields +
		"\n[Peer]\nPublicKey = " + parserCoverageWGPeerKey + "\nEndpoint = awg.example.test:443\nAllowedIPs = 0.0.0.0/0, ::/0\n"
}

func sharedAWGAssert(t *testing.T, record servers.Record, original string, wantKey string) {
	t.Helper()
	if record["protocol"] != "amneziawg" || record["network"] != "udp" || record["security"] != "amneziawg" || record["address"] != "awg.example.test" {
		t.Fatal("raw native AWG lost its protocol or endpoint")
	}
	profile, ok := record["amneziawg"].(map[string]any)
	if !ok || profile["nativeConfig"] != original {
		t.Fatal("raw native AWG bytes changed during envelope decoding")
	}
	if _, exists := profile["protocolVersion"]; exists {
		t.Fatal("native INI invented a Mihomo protocol-version selector")
	}
	if _, err := servers.Outbound(record); err == nil {
		t.Fatal("saved AWG unexpectedly generated an Xray outbound")
	}
	key, err := servers.CanonicalKey(record)
	if err != nil || key != wantKey {
		t.Fatal("native AWG envelope changed connection identity")
	}
	masked, err := json.Marshal(servers.MaskRecord(record))
	if err != nil || strings.Contains(string(masked), parserCoverageWGKey) || strings.Contains(string(masked), "synthetic-private") || strings.Contains(string(masked), "[Interface]") {
		t.Fatal("native AWG private source escaped masking")
	}
	backup, err := servers.Marshal(record)
	if err != nil {
		t.Fatal("native AWG record cannot be backed up")
	}
	restored, err := servers.DecodeBackupRecord(backup, "shared-awg")
	if err != nil {
		t.Fatal("native AWG record cannot be restored")
	}
	restoredProfile, ok := restored["amneziawg"].(map[string]any)
	if !ok || restoredProfile["nativeConfig"] != original {
		t.Fatal("backup changed the native AWG bytes")
	}
}

func TestSharedImportAmneziaWGNativeForms(t *testing.T) {
	for _, option := range []struct{ name, field string }{
		{"legacy", "Jc = 4"},
		{"j1-only", "J1 = <b 0xffffffff><c>"},
		{"itime-only", "Itime = 60"},
		{"v3-header-key-only", "HeaderProtectionKey = synthetic-private-header-key"},
		{"v3-timer-only", "RekeyAfterTime = 120"},
		{"v3-boolean-only", "RandomTrailers = false"},
		{"empty-padding-fields", "Jc = 4\nI1 =\nI2 =\nI3 =\nI4 =\nI5 =\nJ1 =\nJ2 =\nJ3 ="},
	} {
		for _, bom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bom=%v", option.name, bom), func(t *testing.T) {
				raw := sharedAWGINI(option.field)
				if bom {
					raw = "\ufeff# synthetic export\r\n; comment\r\n" + strings.ReplaceAll(raw, "\n", "\r\n")
				}
				// The existing vpn:// importer is the endpoint/byte-preservation
				// control. New raw forms must reach the same private profile.
				control, err := servers.ParseLink("vpn://"+base64.RawURLEncoding.EncodeToString([]byte(raw)), "awg-control", nil)
				if err != nil {
					t.Fatalf("existing vpn INI control rejected: %v", err)
				}
				wantKey, err := servers.CanonicalKey(control)
				if err != nil {
					t.Fatal("existing vpn INI control has no identity")
				}
				for _, form := range []struct{ name, body string }{
					{"raw", raw},
					{"base64", base64.StdEncoding.EncodeToString([]byte(raw))},
					{"base64-url-raw", base64.RawURLEncoding.EncodeToString([]byte(raw))},
				} {
					t.Run(form.name, func(t *testing.T) {
						manual, err := servers.ParseLink(form.body, "shared-awg", nil)
						if err != nil {
							t.Fatalf("manual native AWG form rejected: %v", err)
						}
						sharedAWGAssert(t, manual, raw, wantKey)
						st := sharedFormsStage(t, form.body)
						if st.Received != 1 || st.Accepted != 1 || len(st.Records) != 1 {
							t.Fatal("native AWG INI was split or dropped")
						}
						sharedAWGAssert(t, st.Records[0], raw, wantKey)
						warnings := strings.Join(st.Warnings, " ")
						if !strings.Contains(warnings, "AmneziaWG") || strings.Contains(warnings, "synthetic-private") {
							t.Fatal("native AWG availability warning missing or private")
						}
					})
				}
			})
		}
	}
}

func TestSharedImportAmneziaWGDispatchBoundaries(t *testing.T) {
	// Markers in comments do not change an ordinary WireGuard protocol.
	plainWG := "# Jc = 4\n; HeaderProtectionKey = synthetic-private-comment\n" + sharedAWGINI("")
	for _, body := range []string{plainWG, base64.StdEncoding.EncodeToString([]byte(plainWG))} {
		record, err := servers.ParseLink(body, "shared-wg", nil)
		if err != nil || record["protocol"] != "wireguard" {
			t.Fatal("comment marker misclassified ordinary WireGuard")
		}
	}
	for _, raw := range []string{
		sharedAWGINI("Jc = 4\nJc = 5"),
		strings.Replace(sharedAWGINI("Jc = 4"), "[Peer]", "[Peer]\nJ1 =", 1),
		strings.Replace(sharedAWGINI("Jc = 4"), parserCoverageWGKey, "synthetic-private-invalid-key", 1),
		sharedAWGINI("Jc = 4") + "trojan://synthetic-private-appended@other.example.test:443\n",
	} {
		if _, err := servers.ParseLink(raw, "shared-awg", nil); err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("invalid native AWG accepted or private input leaked")
		}
		if ex, err := extractNodes([]byte(raw), newMetadata()); err == nil {
			if st, stageErr := stageNodes("shared-awg", ex, true, nil); stageErr == nil || st.Accepted > 0 {
				t.Fatal("invalid native AWG salvaged as another connection")
			}
		}
	}
}

func TestSharedImportCommentedINIRejectsPartialSalvage(t *testing.T) {
	link := "trojan://synthetic-private-appended@other.example.test:443"
	control, err := servers.ParseLink(link, "ini-salvage-control", nil)
	if err != nil {
		t.Fatal("valid appended URI control rejected")
	}
	envelopes := []struct {
		name string
		wrap func(string) string
	}{
		{"raw", func(s string) string { return s }},
		{"base64", func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }},
		{"base64-url-raw", func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }},
	}
	for _, envelope := range envelopes {
		t.Run(envelope.name+"/uri-comment-control", func(t *testing.T) {
			st := sharedFormsStage(t, envelope.wrap("# synthetic export\n"+link+"\n"))
			if st.Received != 1 || st.Accepted != 1 || len(st.Records) != 1 {
				t.Fatal("comment-prefixed URI subscription lost its server")
			}
			sharedFormsSameConnection(t, st.Records[0], control)
		})
		for _, protocol := range []struct{ name, fields string }{{"wireguard", ""}, {"amneziawg", "Jc = 4"}} {
			for _, prefix := range []struct{ name, text string }{
				{"hash", "# synthetic export\n"},
				{"semicolon", "; synthetic export\n"},
				{"bom-comments", "\ufeff# synthetic export\r\n; comment\r\n"},
			} {
				t.Run(envelope.name+"/"+protocol.name+"/"+prefix.name, func(t *testing.T) {
					raw := prefix.text + strings.Replace(sharedAWGINI(protocol.fields), parserCoverageWGKey, "synthetic-private-invalid-key", 1) + link + "\n"
					if _, err := servers.ParseLink(raw, "ini-salvage-control", nil); err == nil {
						t.Fatal("malformed native INI control unexpectedly accepted")
					}
					ex, err := extractNodes([]byte(envelope.wrap(raw)), newMetadata())
					if err != nil {
						return
					}
					st, stageErr := stageNodes("ini-salvage", ex, true, nil)
					if stageErr == nil || st == nil || st.Accepted != 0 || len(st.Records) != 0 {
						t.Fatal("malformed comment-prefixed INI imported an appended URI")
					}
				})
			}
		}
	}
}
