package xray

import "testing"

func TestRelease353PinnedCore(t *testing.T) {
	tag, err := ResolveTag("")
	if err != nil || tag != "v26.11.9" {
		t.Fatalf("cold install: %q %v", tag, err)
	}
	want := map[string]string{
		"Xray-linux-arm64-v8a.zip": "64ed6858982a7f5eb85d88b811089063bd4f17561f4c8f44e0f1dc8e6f6430be",
		"Xray-linux-arm32-v7a.zip": "19c51e08a6562705fb1cb032a84fd87df21d7e27b194f035be7e754dbe3a97b3",
		"Xray-linux-mips32le.zip":  "793dc40029283eb9ccead188b05abf0d34b64aab16ad1f6eadd204963a54837b",
		"Xray-linux-mips32.zip":    "4ceb213ad7cd8c1fc9250bb886e730665cddb9dfe891291b09231a204f8ba6ef",
		"Xray-linux-64.zip":        "b651f7e1d9966ee2c1b1ee1bf8b52c72771824d3065dce96bbc8bcc19ed95f82",
	}
	for asset, digest := range want {
		if Pinned["v26.11.9"][asset] != digest {
			t.Errorf("official archive digest mismatch: %s", asset)
		}
	}
	if old, err := ResolveTag("v26.10.31"); err != nil || old != "v26.10.31" {
		t.Fatalf("configured older core changed: %q %v", old, err)
	}
}
