package update

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

//go:embed testdata/production35-identity.txt
var production35Identity []byte

//go:embed testdata/production35-identity.txt.sig
var production35IdentitySignature []byte

// Public proof produced with the dedicated server key and independently
// verified by minisign 0.12. No production private key is a test input.
func production35Proof(t *testing.T) ([]byte, Signature) {
	t.Helper()
	message := append([]byte(nil), production35Identity...)
	sig, err := ParseSignature(production35IdentitySignature)
	if err != nil {
		t.Fatal(err)
	}
	return message, sig
}

func TestProduction35TrustedIdentity(t *testing.T) {
	message, sig := production35Proof(t)
	for _, channel := range []string{"stable", "beta"} {
		t.Run(channel, func(t *testing.T) {
			for _, key := range keysFor(channel, false) {
				if key.IDString() == "16FE8A3DA9ED98DC" {
					if err := sig.Verify(key, message); err != nil {
						t.Fatalf("dedicated production proof rejected: %v", err)
					}
					return
				}
			}
			t.Fatal("dedicated 3.5 production key is not trusted")
		})
	}
}

func TestProduction35KeyIsolation(t *testing.T) {
	for _, channel := range []string{"stable", "beta"} {
		for _, allowTest := range []bool{false, true} {
			for _, key := range keysFor(channel, allowTest) {
				if key.IDString() == "3A5C92992D3BB227" || key.IDString() == "998C1F512E0D35E5" {
					t.Fatalf("test or 3.2 key trusted for production channel %s", channel)
				}
			}
		}
	}
	for _, channel := range []string{"test", "custom"} {
		for _, allowTest := range []bool{false, true} {
			for _, key := range keysFor(channel, allowTest) {
				if key.IDString() == "16FE8A3DA9ED98DC" {
					t.Fatalf("production key escaped its channel allowlist: %s", channel)
				}
			}
		}
	}
}

func TestProduction35TamperedProofRejected(t *testing.T) {
	message, sig := production35Proof(t)
	for _, key := range keysFor("stable", false) {
		if key.IDString() != "16FE8A3DA9ED98DC" {
			continue
		}
		message[0] ^= 1
		if err := sig.Verify(key, message); err == nil {
			t.Fatal("tampered production message accepted")
		}
		sig.TrustedComment += " modified"
		message[0] ^= 1
		if err := sig.Verify(key, message); err == nil {
			t.Fatal("tampered trusted comment accepted")
		}
		return
	}
	t.Fatal("dedicated 3.5 production key is not trusted")
}

func TestProduction35DefaultChannel(t *testing.T) {
	cfg := platform.DefaultConfig()
	source, err := SourceFrom(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if source.Channel != "stable" || source.AllowTestKeys || source.URL != "https://api.brovibe.cloud/releases/broray/channels/stable/manifest.json" {
		t.Fatalf("unexpected production channel: %+v", source)
	}
	if got := DefaultURL("beta"); got != "https://api.brovibe.cloud/releases/broray/channels/beta/manifest.json" || strings.Contains(got, ".invalid") {
		t.Fatalf("unexpected beta production URL: %s", got)
	}
}
