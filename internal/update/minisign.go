package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// Signatures are minisign-compatible (https://jedisct1.github.io/minisign/):
// the detached file <manifest>.sig is a minisign signature, so a manifest
// can be checked with `minisign -Vm manifest.json -P <public key>` and
// signed either with tools/release or with minisign itself (unencrypted or
// encrypted key; tools/release reads only unencrypted keys).
//
//	untrusted comment: <text>
//	base64(alg[2] | keyID[8] | ed25519 signature[64])
//	trusted comment: <text>
//	base64(ed25519 signature over signature[64] | trusted comment text)
//
// alg "ED" signs BLAKE2b-512(message) (minisign default), "Ed" signs the
// message itself (minisign -l, legacy). Both are verified; tools/release
// writes "ED". The public key is base64("Ed" | keyID[8] | key[32]).

const (
	algEd      = "Ed" // key algorithm, legacy (non-prehashed) signature
	algED      = "ED" // prehashed signature
	maxSigSize = 4096
)

// PublicKey is a parsed minisign public key.
type PublicKey struct {
	ID  [8]byte
	Key ed25519.PublicKey
}

// IDString is the key id as minisign prints it (little-endian uint64, hex).
func (k PublicKey) IDString() string { return keyIDString(k.ID) }

func keyIDString(id [8]byte) string {
	return fmt.Sprintf("%016X", binary.LittleEndian.Uint64(id[:]))
}

// ParsePublicKey accepts the base64 line or a whole minisign .pub file.
func ParsePublicKey(s string) (PublicKey, error) {
	var k PublicKey
	line := ""
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "untrusted comment:") {
			continue
		}
		line = l
		break
	}
	b, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(b) != 2+8+32 || string(b[:2]) != algEd {
		return k, errors.New("invalid minisign public key")
	}
	copy(k.ID[:], b[2:10])
	k.Key = ed25519.PublicKey(append([]byte(nil), b[10:]...))
	return k, nil
}

// Encode returns the base64 line of a minisign public key.
func (k PublicKey) Encode() string {
	b := append([]byte(algEd), k.ID[:]...)
	return base64.StdEncoding.EncodeToString(append(b, k.Key...))
}

// Signature is a parsed minisign signature file.
type Signature struct {
	Alg            string
	KeyID          [8]byte
	Sig            []byte
	TrustedComment string
	GlobalSig      []byte
}

func ParseSignature(b []byte) (Signature, error) {
	var s Signature
	if len(b) > maxSigSize {
		return s, errors.New("signature file too large")
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "untrusted comment:") || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return s, errors.New("not a minisign signature")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 2+8+64 {
		return s, errors.New("malformed signature line")
	}
	s.Alg = string(raw[:2])
	if s.Alg != algEd && s.Alg != algED {
		return s, errors.New("unsupported signature algorithm")
	}
	copy(s.KeyID[:], raw[2:10])
	s.Sig = raw[10:]
	s.TrustedComment = strings.TrimPrefix(lines[2], "trusted comment: ")
	s.GlobalSig, err = base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(s.GlobalSig) != 64 {
		return s, errors.New("malformed global signature")
	}
	return s, nil
}

// Verify checks both the message signature and the trusted-comment
// signature with key k (the key id must match).
func (s Signature) Verify(k PublicKey, msg []byte) error {
	if s.KeyID != k.ID {
		return errors.New("signature key id does not match")
	}
	signed := msg
	if s.Alg == algED {
		h := blake2b.Sum512(msg)
		signed = h[:]
	}
	if !ed25519.Verify(k.Key, signed, s.Sig) {
		return errors.New("signature does not match the manifest")
	}
	if !ed25519.Verify(k.Key, append(append([]byte(nil), s.Sig...), s.TrustedComment...), s.GlobalSig) {
		return errors.New("trusted comment signature does not match")
	}
	return nil
}

// SecretKey is an unencrypted minisign secret key (release tooling only;
// never part of a router installation).
type SecretKey struct {
	ID  [8]byte
	Key ed25519.PrivateKey
}

func (s SecretKey) Public() PublicKey {
	return PublicKey{ID: s.ID, Key: s.Key.Public().(ed25519.PublicKey)}
}

// GenerateKey creates a new key pair with a random key id.
func GenerateKey() (SecretKey, error) {
	var sk SecretKey
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return sk, err
	}
	if _, err := rand.Read(sk.ID[:]); err != nil {
		return sk, err
	}
	sk.Key = priv
	return sk, nil
}

// Sign produces a minisign "ED" (prehashed) signature file.
func (s SecretKey) Sign(msg []byte, trustedComment, untrustedComment string) []byte {
	if strings.ContainsAny(trustedComment, "\r\n") || strings.ContainsAny(untrustedComment, "\r\n") {
		panic("minisign comments must be single lines")
	}
	h := blake2b.Sum512(msg)
	sig := ed25519.Sign(s.Key, h[:])
	global := ed25519.Sign(s.Key, append(append([]byte(nil), sig...), trustedComment...))
	line := append([]byte(algED), s.ID[:]...)
	line = append(line, sig...)
	var out bytes.Buffer
	fmt.Fprintf(&out, "untrusted comment: %s\n%s\ntrusted comment: %s\n%s\n", untrustedComment,
		base64.StdEncoding.EncodeToString(line), trustedComment, base64.StdEncoding.EncodeToString(global))
	return out.Bytes()
}

// Secret key file layout (minisign, kdf "\0\0" = unencrypted):
// alg "Ed" | kdf[2] | cksum "B2" | salt[32] | opslimit[8] | memlimit[8] |
// keyID[8] | secret[64] | blake2b-256(alg | keyID | secret)[32]
const skLen = 2 + 2 + 2 + 32 + 8 + 8 + 8 + 64 + 32

func (s SecretKey) checksum() [32]byte {
	b := append([]byte(algEd), s.ID[:]...)
	return blake2b.Sum256(append(b, s.Key...))
}

// EncodeSecretKey writes the minisign unencrypted secret key file.
func (s SecretKey) EncodeFile() []byte {
	b := make([]byte, 0, skLen)
	b = append(b, algEd...)
	b = append(b, 0, 0)
	b = append(b, "B2"...)
	b = append(b, make([]byte, 32+8+8)...)
	b = append(b, s.ID[:]...)
	b = append(b, s.Key...)
	c := s.checksum()
	b = append(b, c[:]...)
	return []byte("untrusted comment: minisign secret key (unencrypted) " + keyIDString(s.ID) + "\n" + base64.StdEncoding.EncodeToString(b) + "\n")
}

// ParseSecretKeyFile reads an unencrypted minisign secret key file.
func ParseSecretKeyFile(file []byte) (SecretKey, error) {
	var s SecretKey
	lines := strings.Split(strings.TrimSpace(string(file)), "\n")
	if len(lines) < 2 {
		return s, errors.New("not a minisign secret key")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(b) != skLen || string(b[:2]) != algEd || string(b[4:6]) != "B2" {
		return s, errors.New("not a minisign secret key")
	}
	if b[2] != 0 || b[3] != 0 {
		return s, errors.New("encrypted minisign keys are not supported by tools/release; sign with minisign itself")
	}
	off := 6 + 32 + 8 + 8
	copy(s.ID[:], b[off:off+8])
	s.Key = ed25519.PrivateKey(append([]byte(nil), b[off+8:off+8+64]...))
	if c := s.checksum(); !bytes.Equal(c[:], b[off+72:]) {
		return s, errors.New("secret key checksum mismatch")
	}
	return s, nil
}
