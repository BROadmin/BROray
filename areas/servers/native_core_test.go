package servers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func nativeBackendFixture(t *testing.T, kind string) map[string]any {
	t.Helper()
	if kind == "masque" || kind == "masque-warp" {
		out := nativeTestOutbound("masque")
		if kind == "masque-warp" {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			private, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			m := get(out, "streamSettings", "masqueSettings").(map[string]any)
			delete(m, "user")
			delete(m, "pass")
			m["warp"] = map[string]any{"privateKey": base64.StdEncoding.EncodeToString(private), "publicKey": base64.StdEncoding.EncodeToString(public), "address": []any{"192.0.2.2/32", "2001:db8::2/128"}}
		}
		return out
	}
	out := nativeTestOutbound("xdrive")
	x := get(out, "streamSettings", "xdriveSettings").(map[string]any)
	x["service"] = kind
	switch kind {
	case "local":
		x["remoteFolder"] = xdriveLocalDirectory
		delete(x, "secrets")
		st := out["streamSettings"].(map[string]any)
		st["security"] = "none"
		delete(st, "tlsSettings")
	case "s3":
		x["remoteFolder"] = "profiles/test/"
		x["secrets"] = []any{"synthetic-private-access", "synthetic-private-secret", "synthetic-private-token"}
		x["s3"] = map[string]any{"endpoint": "https://storage.example.test", "region": "us-east-1", "bucket": "synthetic-bucket", "pathStyle": false}
	case "template":
		x["secrets"] = []any{"synthetic-private-token"}
		x["template"] = map[string]any{
			"flatten": false, "concurrency": 0, "auth": map[string]any{"type": "static", "header": map[string]any{"Authorization": "Bearer {secret0}"}},
			"put":    map[string]any{"url": "https://storage.example.test/{folder}/{name}", "method": "PUT"},
			"get":    map[string]any{"url": "https://storage.example.test/{folder}/{name}", "method": "GET"},
			"delete": map[string]any{"url": "https://storage.example.test/{folder}/{name}", "method": "DELETE"},
			"list":   map[string]any{"url": "https://storage.example.test/{folder}", "method": "GET", "namesRegex": "\"name\":\"([^\"]+)\""},
			"retry":  map[string]any{"status": []any{429, 503}, "rateReason": ""},
		}
	}
	return out
}

func TestNativeBackendRoundTrip(t *testing.T) {
	for _, kind := range []string{"google-drive", "s3", "template", "local", "masque", "masque-warp"} {
		t.Run(kind, func(t *testing.T) {
			fixture := nativeBackendFixture(t, kind)
			r := parseNativeFixture(t, fixture)
			b, _ := Marshal(r)
			restored, err := DecodeBackupRecord(b, "native-test")
			if err != nil {
				t.Fatal(err)
			}
			out, err := Outbound(restored)
			fixture["tag"] = "proxy"
			if err != nil || !jsonValueEqual(out, fixture) {
				t.Fatal("backend data changed", err)
			}
			// Identity changes for storage path/host, not just synthetic server address.
			changed := normalize(r)
			if r["protocol"] == "masque" {
				changed["masque"].(map[string]any)["host"] = "different.example.test"
			} else {
				changed["xdrive"].(map[string]any)["remoteFolder"] = "different-folder"
			}
			a, _ := CanonicalKey(r)
			bkey, _ := CanonicalKey(changed)
			if a == bkey {
				t.Fatal("backend endpoint omitted from identity")
			}
		})
	}
}

func TestNativeCoreConfig(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	for _, kind := range []string{"google-drive", "s3", "template", "local", "masque", "masque-warp"} {
		t.Run(kind, func(t *testing.T) {
			r := parseNativeFixture(t, nativeBackendFixture(t, kind))
			cfg, err := XrayConfig(r, GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "none"}, "native-test")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "client.json")
			if err := os.WriteFile(path, cfg, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, bin, "run", "-test", "-config", path).CombinedOutput()
			if err != nil {
				t.Fatalf("official core rejected synthetic fixture: %v\n%s", err, output)
			}
			t.Log("official 26.11.9 accepted BROray configuration")
		})
	}
}

func TestNativeXMuxScalarString(t *testing.T) {
	out := nativeTestOutbound("masque")
	get(out, "streamSettings", "masqueSettings", "xmux").(map[string]any)["maxConnections"] = "1"
	r := parseNativeFixture(t, out)
	want, _ := json.Marshal("1")
	got, _ := json.Marshal(get(r, "masque", "xmux", "maxConnections"))
	if string(want) != string(got) {
		t.Fatal("explicit scalar range changed")
	}
}
