package servers

import "testing"

func TestNativeEndpointContinuity(t *testing.T) {
	for _, kind := range []string{"google-drive", "s3", "template", "masque"} {
		t.Run(kind, func(t *testing.T) {
			r := parseNativeFixture(t, nativeBackendFixture(t, kind))
			changed := Record(normalize(r))
			switch kind {
			case "masque":
				changed["masque"].(map[string]any)["host"] = "other.example.test"
			case "s3":
				get(changed, "xdrive", "s3").(map[string]any)["bucket"] = "other-bucket"
			case "template":
				get(changed, "xdrive", "template", "list").(map[string]any)["url"] = "https://other.example.test/{prefix}"
			default:
				changed["xdrive"].(map[string]any)["remoteFolder"] = "other-folder"
			}
			a, _ := ContinuityKey(r)
			b, _ := ContinuityKey(changed)
			if a == b {
				t.Fatal("different storage endpoint reused continuity identity")
			}
			rotated := Record(normalize(r))
			if kind == "masque" {
				rotated["masque"].(map[string]any)["pass"] = "rotated-synthetic-secret"
			} else {
				rotated["xdrive"].(map[string]any)["secrets"] = []any{"rotated-synthetic-secret"}
			}
			c, _ := ContinuityKey(rotated)
			if a != c {
				t.Fatal("credentials alone changed endpoint continuity")
			}
			rotated = Record(normalize(r))
			rotated["tls"].(map[string]any)["serverName"] = "rotated.example.test"
			a, _ = RotationKey(r)
			b, _ = RotationKey(rotated)
			if a != b {
				t.Fatal("SNI rotation changed endpoint rotation identity")
			}
		})
	}
}
