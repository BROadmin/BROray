package update

import "sync"

// TrustedKey is a release-signing public key built into the binary.
//
// Production keys are generated offline by the owner (tools/release keygen,
// docs/RELEASE-CHANNEL.md) and only their public half is added here.
// Channels limits the channels a key may sign (nil: every channel).
// Test keys are trusted only for the channel "test" and only when
// config.v1.json selects that channel (update.channel = "test").
//
// TODO(key rotation, design §3.2): a rotation statement — a new public key
// signed by a currently trusted key and published next to the manifest —
// is not implemented; rotating today means shipping a release that adds
// the new key here while the old key still signs, then dropping the old.
type TrustedKey struct {
	Name      string
	PublicKey string // minisign public key line (base64)
	Channels  []string
	Test      bool
}

var builtinKeys = []TrustedKey{
	{
		// Private half: owner/Codex test bench only, never in this
		// repository (generated 2026-11-02 with tools/release keygen).
		Name:      "BROray test channel 2026-11",
		PublicKey: testChannelPublicKey,
		Channels:  []string{"test"},
		Test:      true,
	},
	// Production (stable/beta) keys: none yet — the owner adds the public
	// key of the offline release key here (docs/RELEASE-CHANNEL.md). Until
	// then every stable manifest is refused as signed by an unknown key.
}

// Key id 3A5C92992D3BB227.
const testChannelPublicKey = "RWQnsjstmZJcOgoEeGPUqynErXopQEQxuf2BDLfvcIYi5sQjiX05N2ow"

var (
	extraMu   sync.Mutex
	extraKeys []TrustedKey
)

// TrustExtra adds a key at run time. Only test code and the host-test build
// (-tags brorayfake) call it; router builds have no caller. It returns a
// function that removes the key again.
func TrustExtra(k TrustedKey) (remove func()) {
	extraMu.Lock()
	extraKeys = append(extraKeys, k)
	extraMu.Unlock()
	return func() {
		extraMu.Lock()
		defer extraMu.Unlock()
		for i := range extraKeys {
			if extraKeys[i].PublicKey == k.PublicKey && extraKeys[i].Name == k.Name {
				extraKeys = append(extraKeys[:i], extraKeys[i+1:]...)
				return
			}
		}
	}
}

// keysFor returns the parsed keys allowed to sign channel; allowTest is
// true only when the owner's config selects the test channel.
func keysFor(channel string, allowTest bool) []PublicKey {
	extraMu.Lock()
	all := append(append([]TrustedKey(nil), builtinKeys...), extraKeys...)
	extraMu.Unlock()
	var out []PublicKey
	for _, k := range all {
		if k.Test && (!allowTest || channel != "test") {
			continue
		}
		if k.Channels != nil && !contains(k.Channels, channel) {
			continue
		}
		if pk, err := ParsePublicKey(k.PublicKey); err == nil {
			out = append(out, pk)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
