package servers

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func dpiContractRecord(t *testing.T) Record {
	t.Helper()
	rec, err := ParseLink("vless://11111111-2222-4333-8444-555555555555@node.example:443?type=tcp&security=tls&sni=node.example&encryption=none#test", "dpi-record", nil)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}
func dpiContractManaged(rec Record) {
	transport := rec["transport"].(map[string]any)
	old, present := transport["finalMask"]
	rec["dpi"] = map[string]any{"schema": 1, "preset": "tlshello-v1", "originalMaskPresent": present, "originalMask": old}
	transport["finalMask"] = map[string]any{"tcp": []any{map[string]any{"type": "fragment", "settings": map[string]any{"packets": "tlshello", "length": "100-200", "delay": "1-2", "maxSplit": 64}}}}
}
func TestDPIRecordRejectsUnprovenOwnership(t *testing.T) {
	for _, kind := range []string{"null-marker", "wrong-schema", "extra-key", "wrong-preset", "foreign-mask", "custom-original", "absent-with-value", "bad-presence"} {
		t.Run(kind, func(t *testing.T) {
			rec := dpiContractRecord(t)
			dpiContractManaged(rec)
			meta := rec["dpi"].(map[string]any)
			switch kind {
			case "null-marker":
				rec["dpi"] = nil
			case "wrong-schema":
				meta["schema"] = "1"
			case "extra-key":
				meta["enabled"] = true
			case "wrong-preset":
				meta["preset"] = "future"
			case "foreign-mask":
				rec["transport"].(map[string]any)["finalMask"] = map[string]any{}
			case "custom-original":
				meta["originalMaskPresent"] = true
				meta["originalMask"] = map[string]any{"tcp": []any{}}
			case "absent-with-value":
				meta["originalMaskPresent"] = false
				meta["originalMask"] = map[string]any{}
			case "bad-presence":
				meta["originalMaskPresent"] = "true"
			}
			if err := Validate(rec, "dpi-record"); err == nil {
				t.Fatal("unproven DPI metadata accepted")
			}
		})
	}
}
func TestDPIRecordPublicDefault(t *testing.T) {
	rec := dpiContractRecord(t)
	public := MaskRecord(rec)
	state, ok := public["dpi"].(map[string]any)
	if !ok || state["enabled"] != false || state["profileCompatible"] != true || state["requiredCoreVersion"] != "26.11.9" {
		t.Fatal("default-OFF DPI compatibility absent", state)
	}
	if _, leaked := state["originalMask"]; leaked {
		t.Fatal("private mask exposed")
	}
	if _, mutated := rec["dpi"]; mutated {
		t.Fatal("presentation mutated original")
	}
}

func dpiRecordBytes(t *testing.T, rec Record) []byte {
	t.Helper()
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestDPIRecordReversibleProposal(t *testing.T) {
	for _, present := range []bool{false, true} {
		name := "absent"
		if present {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			original := dpiContractRecord(t)
			if present {
				original["transport"].(map[string]any)["finalMask"] = map[string]any{}
			} else {
				delete(original["transport"].(map[string]any), "finalMask")
			}
			original["name"] = "unsaved sibling must remain"
			before := dpiRecordBytes(t, original)
			gs := GenSettings{Listen: "127.0.0.1", SocksPort: 1080, LogLevel: "warning"}
			configBefore, err := XrayConfig(original, gs, "before")
			if err != nil {
				t.Fatal(err)
			}
			on, err := prepareDPIRecord(original, true)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, dpiRecordBytes(t, original)) {
				t.Fatal("input mutated")
			}
			if bytes.Equal(before, dpiRecordBytes(t, on)) {
				t.Fatal("no proposal change")
			}
			if err := Validate(on, "enabled"); err != nil {
				t.Fatal(err)
			}
			state := MaskRecord(on)["dpi"].(map[string]any)
			if state["enabled"] != true || state["profileCompatible"] != true {
				t.Fatal(state)
			}
			repeated, err := prepareDPIRecord(on, true)
			if err != nil || !bytes.Equal(dpiRecordBytes(t, on), dpiRecordBytes(t, repeated)) {
				t.Fatal("ON not idempotent", err)
			}
			env := newEnv(t)
			if err := WriteRecord(env.d, on); err != nil {
				t.Fatal(err)
			}
			saved, err := ReadRecord(env.d, "dpi-record")
			if err != nil {
				t.Fatal(err)
			}
			off, err := prepareDPIRecord(saved, false)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, dpiRecordBytes(t, off)) {
				t.Fatal("original record not exactly restored")
			}
			configAfter, err := XrayConfig(off, gs, "after")
			if err != nil || !bytes.Equal(configBefore, configAfter) {
				t.Fatal("original generated config not restored", err)
			}
			repeated, err = prepareDPIRecord(off, false)
			if err != nil || !bytes.Equal(before, dpiRecordBytes(t, repeated)) {
				t.Fatal("OFF not idempotent", err)
			}
			on["tls"].(map[string]any)["serverName"] = "detached.example"
			if !bytes.Equal(before, dpiRecordBytes(t, original)) {
				t.Fatal("nested input alias")
			}
		})
	}
}
func TestDPIRecordPreservesCustomMasks(t *testing.T) {
	for _, preset := range []bool{false, true} {
		rec := dpiContractRecord(t)
		mask := map[string]any{"tcp": []any{}}
		if preset {
			mask = dpiMask()
		}
		rec["transport"].(map[string]any)["finalMask"] = mask
		before := dpiRecordBytes(t, rec)
		if _, err := prepareDPIRecord(rec, true); err == nil {
			t.Fatal("custom mask adopted or overwritten")
		}
		off, err := prepareDPIRecord(rec, false)
		if err != nil || !bytes.Equal(before, dpiRecordBytes(t, off)) {
			t.Fatal("custom OFF changed record", err)
		}
		state := MaskRecord(rec)["dpi"].(map[string]any)
		if state["enabled"] != false || state["profileCompatible"] != false || state["reason"] == "" {
			t.Fatal(state)
		}
		if !bytes.Equal(before, dpiRecordBytes(t, rec)) {
			t.Fatal("custom input changed")
		}
	}
	rec := dpiContractRecord(t)
	rec["transport"].(map[string]any)["finalMask"] = nil
	if _, err := prepareDPIRecord(rec, true); err == nil {
		t.Fatal("invalid null mask made valid")
	}
}
func TestDPIRecordForeignStateNeverOverwritten(t *testing.T) {
	rec := dpiContractRecord(t)
	dpiContractManaged(rec)
	rec["transport"].(map[string]any)["finalMask"] = map[string]any{}
	rec["dpi"].(map[string]any)["originalMask"] = map[string]any{"secret": "must-not-leak"}
	before := dpiRecordBytes(t, rec)
	for _, enabled := range []bool{false, true} {
		if _, err := prepareDPIRecord(rec, enabled); err == nil {
			t.Fatal("foreign state overwritten")
		}
	}
	public := MaskRecord(rec)
	if strings.Contains(string(dpiRecordBytes(t, public)), "must-not-leak") {
		t.Fatal("private mask leaked")
	}
	state := public["dpi"].(map[string]any)
	if state["enabled"] != nil || state["profileCompatible"] != false {
		t.Fatal("unknown reported confirmed", state)
	}
	if !bytes.Equal(before, dpiRecordBytes(t, rec)) {
		t.Fatal("foreign input changed")
	}
}
func TestDPIRecordUnsupportedProfileRefused(t *testing.T) {
	for _, kind := range []string{"plaintext", "xhttp", "udp443-flow", "reality-grpc", "raw-header"} {
		t.Run(kind, func(t *testing.T) {
			rec := dpiContractRecord(t)
			switch kind {
			case "plaintext":
				rec["security"] = "none"
			case "xhttp":
				rec["network"] = "xhttp"
			case "udp443-flow":
				rec["flow"] = "xtls-rprx-vision-udp443"
			case "reality-grpc":
				rec["security"] = "reality"
				rec["network"] = "grpc"
			case "raw-header":
				rec["transport"].(map[string]any)["headerType"] = "http"
			}
			before := dpiRecordBytes(t, rec)
			if _, err := prepareDPIRecord(rec, true); err == nil {
				t.Fatal("unsupported profile enabled")
			}
			if !bytes.Equal(before, dpiRecordBytes(t, rec)) {
				t.Fatal("unsupported input changed")
			}
		})
	}
}
func TestDPIRecordDisableAfterParameterChange(t *testing.T) {
	rec := dpiContractRecord(t)
	dpiContractManaged(rec)
	rec["flow"] = "xtls-rprx-vision-udp443"
	if Validate(rec, "changed") == nil {
		t.Fatal("unsupported enabled profile accepted")
	}
	off, err := prepareDPIRecord(rec, false)
	if err != nil {
		t.Fatal("cannot disable owned preset after parameter change", err)
	}
	if off["flow"] != "xtls-rprx-vision-udp443" {
		t.Fatal("disable rewrote connection field")
	}
	if _, ok := off["dpi"]; ok {
		t.Fatal("owned marker retained")
	}
}
