package servers

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/cgi"
)

const awgPrivate = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
const awgPublic = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="

func TestAmneziaBase64INIImportAndBackup(t *testing.T) {
	ini := "[Interface]\nPrivateKey = " + awgPrivate + "\nAddress = 10.0.0.2/32\nJc = 4\nHeaderProtectionKey = synthetic-private-extension\n[Peer]\nPublicKey = " + awgPublic + "\nEndpoint = awg.example:443\nAllowedIPs = 0.0.0.0/0\n"
	link := "vpn://" + base64.RawURLEncoding.EncodeToString([]byte(ini))
	rec, err := ParseLink(link, "manual-native-awg", nil)
	if err != nil || Validate(rec, "test") != nil {
		t.Fatalf("native AWG import: %v", err)
	}
	b, _ := Marshal(rec)
	restored, err := DecodeBackupRecord(b, "test")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := Marshal(restored)
	if !bytes.Equal(b, b2) || get(rec, "amneziawg", "nativeConfig") != ini {
		t.Fatal("INI profile changed in backup")
	}
	masked, _ := Marshal(MaskRecord(rec))
	if bytes.Contains(masked, []byte(awgPrivate)) || bytes.Contains(masked, []byte("synthetic-private-extension")) || bytes.Contains(masked, []byte("vpn://")) {
		t.Fatal("private INI leaked")
	}
	if connectionSupport(rec)["supported"] != false {
		t.Fatal("INI AWG enabled for connection")
	}
	if _, err := XrayConfig(rec, GenSettings{}, "test"); err == nil {
		t.Fatal("INI AWG generated Xray config")
	}
}

func TestAmneziaPreviewRejectsUnpersistableProfile(t *testing.T) {
	for _, fields := range []int{10, 12} {
		link := awgLink(t, true, func(c map[string]any) {
			var extra strings.Builder
			for i := 0; i < fields; i++ {
				fmt.Fprintf(&extra, "Extension%d = %s\n", i, strings.Repeat("x", 15000))
			}
			c["config"] = strings.Replace(c["config"].(string), "[Peer]", extra.String()+"[Peer]", 1)
		})
		parsed, err := amnezia.ParseLink(link)
		if err != nil {
			t.Fatalf("fixture must fit the decoded input limit: %v", err)
		}
		model, _ := json.Marshal(parsed.Profile)
		rec, parseErr := ParseLink(link, "large", nil)
		if len(model) > 2*amnezia.MaxProfileBytes {
			if parseErr == nil {
				t.Fatal("preview accepted profile that persistence rejects")
			}
		} else if parseErr != nil || Validate(rec, "test") != nil {
			t.Fatal("profile within both limits must remain usable")
		}
	}
}

func TestAmneziaIsNotMissingQualityMeasurement(t *testing.T) {
	e := newEnv(t)
	e.addServer(t, awgLink(t, true, nil))
	summary, err := e.s.summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	health := summary["health"].(map[string]any)
	for _, r := range health["reasons"].([]any) {
		if r.(map[string]any)["code"] == "QUALITY_INCOMPLETE" {
			t.Fatal("uncheckable profile requires an impossible quality check")
		}
	}
	row := summary["servers"].([]any)[0].(map[string]any)
	if get(row, "quality", "status") != "unsupported" {
		t.Fatal("uncheckable quality must be explicit")
	}
}

func awgLink(t *testing.T, compressed bool, change func(map[string]any)) string {
	t.Helper()
	client := map[string]any{"config": "[Interface]\nPrivateKey = " + awgPrivate + "\nAddress = 10.0.0.2/32\nDNS = 1.1.1.1\nJc = 4\nS1 = 20\nH1 = 12345\nI1 = <b 0123><r 20>\nFutureOption = preserved\n[Peer]\nPublicKey = " + awgPublic + "\nPresharedKey = " + awgPrivate + "\nEndpoint = vpn.example:443\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\n", "future_secret": "never-public"}
	if change != nil {
		change(client)
	}
	c, _ := json.Marshal(client)
	envelope := map[string]any{"description": "AWG example", "containers": []any{map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(c), "protocol_version": "2"}}}}
	b, _ := json.Marshal(envelope)
	if compressed {
		var out bytes.Buffer
		_ = binary.Write(&out, binary.BigEndian, uint32(len(b)))
		zw := zlib.NewWriter(&out)
		_, _ = zw.Write(b)
		_ = zw.Close()
		b = out.Bytes()
	}
	return "vpn://" + base64.RawURLEncoding.EncodeToString(b)
}

func TestAmneziaImportPreservesProfileAndMasksSecrets(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		rec, err := ParseLink(awgLink(t, compressed, nil), "awg-test", nil)
		if err != nil {
			t.Fatalf("AWG import compressed=%v: %v", compressed, err)
		}
		if rec["protocol"] != "amneziawg" || rec["address"] != "vpn.example" || !jsonEq(rec["port"], 443) {
			t.Fatal("wrong AWG endpoint/protocol")
		}
		if err := Validate(rec, "test"); err != nil {
			t.Fatal(err)
		}
		encoded, _ := Marshal(rec)
		restored, restoreErr := DecodeBackupRecord(encoded, "synthetic")
		if restoreErr != nil {
			t.Fatalf("backup rejected AWG: %v", restoreErr)
		}
		restoredBytes, _ := Marshal(restored)
		if !bytes.Equal(encoded, restoredBytes) {
			t.Fatal("backup changed AWG profile")
		}
		for _, want := range []string{"FutureOption", "never-public", awgPrivate, "I1"} {
			if !bytes.Contains(encoded, []byte(want)) {
				t.Fatalf("lost %s", want)
			}
		}
		masked, _ := Marshal(MaskRecord(rec))
		for _, secret := range []string{awgPrivate, "never-public", "vpn://", "PrivateKey", "PresharedKey"} {
			if bytes.Contains(masked, []byte(secret)) {
				t.Fatalf("public record leaks %s", secret)
			}
		}
		if _, err := XrayConfig(rec, GenSettings{}, "test"); err == nil {
			t.Fatal("AWG rendered as Xray configuration")
		}
	}
}

func TestAmneziaKeyTracksParametersWithoutCreatingNewIdentity(t *testing.T) {
	a, err := ParseLink(awgLink(t, true, nil), "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseLink(awgLink(t, false, func(c map[string]any) {
		c["config"] = strings.ReplaceAll(c["config"].(string), "H1 = 12345", "H1 = 54321")
	}), "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	ak, _ := CanonicalKey(a)
	bk, _ := CanonicalKey(b)
	ac, _ := ContinuityKey(a)
	bc, _ := ContinuityKey(b)
	if ak == bk || ac != bc {
		t.Fatal("AWG parameter rotation must change configuration key but preserve continuity")
	}
	c, _ := ParseLink(awgLink(t, false, nil), "c", nil)
	ck, _ := CanonicalKey(c)
	if ak != ck {
		t.Fatal("packaging changed connection identity")
	}
}

func TestAmneziaCannotConnectOrEnterQualityQueue(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, awgLink(t, true, nil))
	for _, action := range []func() (any, error){func() (any, error) { return e.s.activate(id) }, func() (any, error) { return e.s.directCheck(id) }} {
		_, err := action()
		var api *cgi.APIError
		if !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" {
			t.Fatalf("expected explicit unsupported error, got %v", err)
		}
	}
	if len(e.applied) != 0 || e.ensured.Load() != 0 || ActiveID(e.d) != "" {
		t.Fatal("AWG action changed connection")
	}
	_, _, queuedErr := e.s.resolveCheck(id)
	if queuedErr == nil || queuedErr.Code != "PROTOCOL_NOT_SUPPORTED" {
		t.Fatalf("queued check lacks explicit unsupported status: %v", queuedErr)
	}
	normal := e.addServer(t, "trojan://synthetic@other.example:443#Other")
	snap, err := e.s.qualitySnapshot("all")
	if err != nil || len(snap) != 1 || snap[0].ID != normal {
		t.Fatalf("quality/failover candidates include AWG: %v", err)
	}
	summary, err := e.s.summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range summary["servers"].([]any) {
		m := row.(map[string]any)
		if m["id"] == id && get(m, "connectionSupport", "supported") != false {
			t.Fatal("summary marks AWG connectable")
		}
	}
}

func TestAmneziaInvalidContainerDoesNotLeakInput(t *testing.T) {
	valid := awgLink(t, false, nil)
	data, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(valid, "vpn://"))
	for _, body := range []string{strings.ReplaceAll(string(data), "amnezia-awg", "amnezia-openvpn"), `{}`, `{"containers":[]}`, string(data) + `{}`} {
		_, err := ParseLink("vpn://"+base64.RawURLEncoding.EncodeToString([]byte(body)), "bad", nil)
		if err == nil {
			t.Fatal("invalid container accepted")
		}
		if strings.Contains(err.Error(), awgPrivate) || strings.Contains(err.Error(), "never-public") {
			t.Fatal("error leaked source")
		}
	}
}

func TestAmneziaRejectsMalformedOrOversizedCompression(t *testing.T) {
	valid := awgLink(t, true, nil)
	b, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(valid, "vpn://"))
	badSize := append([]byte{}, b...)
	binary.BigEndian.PutUint32(badSize, 1)
	oversized := append([]byte{}, b...)
	binary.BigEndian.PutUint32(oversized, 16*1024*1024)
	for _, data := range [][]byte{b[:8], badSize, oversized, append(b, 'x')} {
		if _, err := ParseLink("vpn://"+base64.RawURLEncoding.EncodeToString(data), "bad", nil); err == nil {
			t.Fatal("malformed compression accepted")
		}
	}
}

func TestAmneziaUnsupportedPreferredServerRefused(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, awgLink(t, true, nil))
	body := `{"enabled":true,"failureThreshold":3,"cooldownMinutes":10,"minimumRating":"acceptable","selectionRule":"preferred","preferredServerId":"` + id + `"}`
	r := call(t, "POST", "/api/servers/auto-switch-save.cgi", body, nil)
	if r.Code != 400 || errCode(r) != "PROTOCOL_NOT_SUPPORTED" {
		t.Fatalf("unsupported preferred server accepted: %d %s", r.Code, errCode(r))
	}
}

func TestAmneziaIPv6AndMalformedClient(t *testing.T) {
	link := awgLink(t, true, func(c map[string]any) {
		c["config"] = strings.ReplaceAll(c["config"].(string), "vpn.example:443", "[2001:db8::1]:443")
	})
	rec, err := ParseLink(link, "ipv6", nil)
	if err != nil || rec["address"] != "2001:db8::1" {
		t.Fatalf("IPv6: %v", err)
	}
	for _, pair := range [][2]string{{"PrivateKey = " + awgPrivate, "PrivateKey = malformed"}, {"Address = 10.0.0.2/32", "Address = bad"}, {"Endpoint = vpn.example:443", "Endpoint = vpn.example:0"}, {"[Peer]", "[Unsupported]"}, {"H1 = 12345", "H1 = 12345\nH1 = 54321"}} {
		bad := awgLink(t, true, func(c map[string]any) { c["config"] = strings.ReplaceAll(c["config"].(string), pair[0], pair[1]) })
		if _, err := ParseLink(bad, "bad", nil); err == nil {
			t.Fatalf("malformed client accepted: %s", pair[0])
		}
	}
}
