package servers

import (
	"encoding/json"
	"strings"
	"testing"
)

func nativeParityJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal("could not encode synthetic native fixture")
	}
	return string(b)
}

func nativeParityProfile(out map[string]any) map[string]any {
	return map[string]any{
		"remarks":   "Synthetic native profile",
		"dns":       map[string]any{"servers": []any{"synthetic-private-profile-marker"}},
		"outbounds": []any{out},
	}
}

func nativeParityWarnings(t *testing.T, rec Record) []string {
	t.Helper()
	values, ok := rec["importWarnings"].([]any)
	if !ok || len(values) == 0 {
		t.Fatal("expected native import warnings")
	}
	var warnings []string
	for _, value := range values {
		warning, ok := value.(string)
		if !ok || warning == "" {
			t.Fatal("invalid native import warning")
		}
		if strings.Contains(warning, "synthetic-private") {
			t.Fatal("native warning exposes fixture data")
		}
		warnings = append(warnings, warning)
	}
	return warnings
}

func nativeParityBackup(t *testing.T, rec Record) Record {
	t.Helper()
	raw, err := Marshal(rec)
	if err != nil {
		t.Fatal("could not encode backup fixture")
	}
	restored, err := DecodeBackupRecord(raw, "native-input-parity")
	if err != nil {
		t.Fatal("native fixture rejected by backup restore")
	}
	want, err := Outbound(rec)
	if err != nil {
		t.Fatal("native fixture could not generate outbound")
	}
	got, err := Outbound(restored)
	if err != nil || !jsonValueEqual(got, want) {
		t.Fatal("backup changed native connection settings")
	}
	if !jsonValueEqual(restored["importWarnings"], rec["importWarnings"]) {
		t.Fatal("backup changed native import warnings")
	}
	masked, err := Marshal(MaskRecord(restored))
	if err != nil || strings.Contains(string(masked), "synthetic-private") {
		t.Fatal("restored public record exposes fixture data")
	}
	return restored
}

func TestNativeInputBOMParity(t *testing.T) {
	for _, kind := range []string{"xdrive", "masque"} {
		for _, shape := range []string{"bare", "profile"} {
			t.Run(kind+"/"+shape, func(t *testing.T) {
				out := nativeTestOutbound(kind)
				var document any = out
				if shape == "profile" {
					document = nativeParityProfile(out)
				}
				plain := nativeParityJSON(t, document)
				want, err := ParseLink(plain, "native-input-parity", nil)
				if err != nil {
					t.Fatal("plain native control was rejected")
				}
				for _, prefix := range []string{"\ufeff", "\ufeff \r\n\t"} {
					got, err := ParseLink(prefix+plain, "native-input-parity", nil)
					if err != nil {
						t.Fatal("UTF-8 BOM changed native document acceptance")
					}
					if !jsonValueEqual(got, want) {
						t.Fatal("UTF-8 BOM changed native record")
					}
					nativeParityBackup(t, got)
				}
			})
		}
	}
}

func TestNativeInputPreservesEmbeddedBOM(t *testing.T) {
	out := nativeTestOutbound("masque")
	want := "synthetic-private-\ufeff-token"
	get(out, "streamSettings", "masqueSettings").(map[string]any)["pass"] = want
	plain := nativeParityJSON(t, out)
	for _, prefix := range []string{"", "\ufeff"} {
		rec, err := ParseLink(prefix+plain, "native-input-parity", nil)
		if err != nil {
			t.Fatal("native string containing a BOM was rejected")
		}
		generated, err := Outbound(rec)
		if err != nil || get(generated, "streamSettings", "masqueSettings", "pass") != want {
			t.Fatal("BOM normalization changed private field data")
		}
		nativeParityBackup(t, rec)
	}
	for _, invalid := range []string{" \ufeff" + plain, "\ufeff\ufeff" + plain, plain + "\ufeff"} {
		if _, err := ParseLink(invalid, "native-input-parity", nil); err == nil {
			t.Fatal("BOM outside the initial framing position accepted")
		}
	}
}

func TestNativeInputRejectsInvalidNameTypes(t *testing.T) {
	values := []struct {
		name  string
		value any
	}{
		{"null", nil},
		{"number", json.Number("7")},
		{"boolean", false},
		{"array", []any{"synthetic-private-name"}},
		{"object", map[string]any{"value": "synthetic-private-name"}},
	}
	for _, kind := range []string{"xdrive", "masque"} {
		for _, location := range []string{"bare-tag", "profile-tag", "profile-remarks"} {
			for _, value := range values {
				t.Run(kind+"/"+location+"/"+value.name, func(t *testing.T) {
					out := nativeTestOutbound(kind)
					profile := nativeParityProfile(out)
					var document any = profile
					switch location {
					case "bare-tag":
						out["tag"] = value.value
						document = out
					case "profile-tag":
						// A valid remarks must not hide an invalid outbound tag.
						out["tag"] = value.value
					case "profile-remarks":
						profile["remarks"] = value.value
					}
					_, err := ParseLink(nativeParityJSON(t, document), "native-input-parity", nil)
					if err == nil {
						t.Fatal("non-string native name metadata accepted")
					}
					if strings.Contains(err.Error(), "synthetic-private") {
						t.Fatal("invalid metadata error exposes fixture data")
					}
				})
			}
		}
	}
}

func TestNativeInputWarningsMergeAndSurviveBackup(t *testing.T) {
	out := nativeTestOutbound("xdrive")
	user := get(out, "settings", "vnext").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
	user["level"] = 0
	user["email"] = "synthetic-private-email"
	bare, err := ParseLink(nativeParityJSON(t, out), "native-input-parity", nil)
	if err != nil {
		t.Fatal("bare native metadata control rejected")
	}
	wantWarnings := nativeParityWarnings(t, bare)
	full, err := ParseLink(nativeParityJSON(t, nativeParityProfile(out)), "native-input-parity", nil)
	if err != nil {
		t.Fatal("full native profile rejected")
	}
	gotWarnings := nativeParityWarnings(t, full)
	for _, want := range wantWarnings {
		found := false
		for _, got := range gotWarnings {
			found = found || got == want
		}
		if !found {
			t.Fatal("profile warning replaced native endpoint metadata warning")
		}
	}
	if len(gotWarnings) <= len(wantWarnings) {
		t.Fatal("full profile exclusion warning missing")
	}
	bareOutbound, err := Outbound(bare)
	if err != nil {
		t.Fatal("bare native outbound generation failed")
	}
	fullOutbound, err := Outbound(full)
	if err != nil || !jsonValueEqual(fullOutbound, bareOutbound) {
		t.Fatal("full profile changed native endpoint settings")
	}
	for _, field := range []string{"dns", "routing", "inbounds", "api"} {
		if has(full, field) || has(fullOutbound, field) {
			t.Fatal("foreign profile configuration escaped endpoint extraction")
		}
	}
	nativeParityBackup(t, bare)
	nativeParityBackup(t, full)
}
