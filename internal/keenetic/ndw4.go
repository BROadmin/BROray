package keenetic

// KeeneticOS 5.2 NDW4 password-interactive adapter, distinct from the
// X-NDMA-TKN RCI access-token mechanism. The wire transcript was independently
// compared in BROray to the native Keenetic WebUI's 2026-10-27 JS bundle.
import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha3"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	ndw4MaxMemoryKiB        = 65536
	ndw4MaxIterations       = 10
	ndw4MaxData             = 8192
	ndw4MaxResponse         = 16384
	ndw4MemorySafetyReserve = 32 << 20 // avoid router OOM, particularly 128 MiB
)

var (
	ErrUnsupported        = errors.New("KEENETIC_AUTH_UNSUPPORTED")
	ErrResource           = errors.New("KEENETIC_AUTH_RESOURCE_LIMIT")
	ndw4EndpointPattern   = regexp.MustCompile(`(?i)^x-ndw4-interactive(?:[ \t]+endpoint[ \t]*=[ \t]*"/auth"(?:[ \t]+data[ \t]*=[ \t]*"([A-Za-z0-9+/]{1,512}={0,2})")?)?$`)
	ndw2CapabilityPattern = regexp.MustCompile(`(?i)^x-ndw2-interactive(?:[ \t]+[^,\r\n]+)?$`)
	ndw4Base64Pattern     = regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`)
)

type authMode uint8

const (
	authUnsupported authMode = iota
	authLegacy
	authNDW4
)

// matchNDW4Capability allows only the actual password-interactive protocol.
// KeeneticOS 5.2 Beta 1 may append data="e30=" (canonical base64 of {}).
// Reject any nonempty/future capabilities instead of ignoring unknown security
// semantics or silently downgrading to NDW2. This is not x-ndw4-authenticate,
// the separate access-token bootstrap protocol.
func matchNDW4Capability(raw string) bool {
	matches := ndw4EndpointPattern.FindStringSubmatch(raw)
	if matches == nil {
		return false
	}
	if matches[1] == "" {
		return true
	}
	decoded, ok := decodeNDW4Base64(matches[1])
	defer clear(decoded)
	return ok && bytes.Equal(decoded, []byte("{}"))
}

// selectAuthMode uses actual /auth capabilities, never a firmware version.
// Actual KN-2710 5.2 Beta1 advertises both modes in separate WWW fields.
// Prefer NDW4 without retry/downgrade to legacy if NDW4 fails.
func selectAuthMode(h http.Header) (authMode, error) {
	fields := h.Values("WWW-Authenticate")
	seen4, seen2 := false, false
	unknown := false
	for _, field := range fields {
		for _, raw := range strings.Split(field, ",") {
			c := strings.TrimSpace(raw)
			if c == "" {
				return authUnsupported, ErrProtocol
			}
			if matchNDW4Capability(c) {
				if seen4 {
					return authUnsupported, ErrProtocol
				}
				seen4 = true
				continue
			}
			if ndw2CapabilityPattern.MatchString(c) {
				if seen2 {
					return authUnsupported, ErrProtocol
				}
				seen2 = true
				continue
			}
			switch strings.ToLower(c) {
			case "x-ndw2-account-sso", "x-ndmp-tkn":
				continue
			default:
				unknown = true
			}
		}
	}
	if unknown {
		return authUnsupported, ErrUnsupported
	}
	if seen4 {
		return authNDW4, nil
	}
	if seen2 {
		return authLegacy, nil
	}
	if len(fields) == 0 && len(h.Values("X-NDM-Realm")) == 1 &&
		len(h.Values("X-NDM-Challenge")) == 1 {
		return authLegacy, nil
	}
	return authUnsupported, ErrUnsupported
}
func decodeNDW4Base64(s string) ([]byte, bool) {
	if len(s) == 0 || len(s) > ndw4MaxData || !ndw4Base64Pattern.MatchString(s) {
		return nil, false
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(s)
	return raw, e == nil && base64.StdEncoding.EncodeToString(raw) == s
}
func decodeNDW4Data(h http.Header, fields []string) (map[string]json.RawMessage, error) {
	vals := h.Values("X-NDM-Data")
	if len(vals) != 1 {
		return nil, ErrProtocol
	}
	b, ok := decodeNDW4Base64(vals[0])
	if !ok {
		return nil, ErrProtocol
	}
	defer clear(b)
	if len(b) > ndw4MaxData {
		return nil, ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(b))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ErrProtocol
	}
	allowed := map[string]bool{}
	for _, s := range fields {
		allowed[s] = true
	}
	result := map[string]json.RawMessage{}
	for d.More() {
		t, e = d.Token()
		name, isName := t.(string)
		if e != nil || !isName || !allowed[name] || result[name] != nil {
			return nil, ErrProtocol
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(v, []byte("null")) {
			return nil, ErrProtocol
		}
		result[name] = v
	}
	t, e = d.Token()
	if e != nil || t != json.Delim('}') || len(result) != len(fields) {
		return nil, ErrProtocol
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrProtocol
	}
	return result, nil
}
func ndw4MAC(k, b []byte) []byte {
	h := hmac.New(func() hash.Hash { return sha3.New512() }, k)
	_, _ = h.Write(b)
	return h.Sum(nil)
}
func ndw4XOR(k, v []byte) []byte {
	for i := range v {
		v[i] ^= k[i]
	}
	return v
}
func memoryAvailableLinux() (uint64, error) {
	if runtime.GOOS != "linux" {
		return 0, ErrResource
	}
	data, e := os.ReadFile("/proc/meminfo")
	if e != nil || len(data) > 1<<20 {
		return 0, ErrResource
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		p := bytes.Fields(line)
		if len(p) == 3 && string(p[0]) == "MemAvailable:" && string(p[2]) == "kB" {
			n, e := strconv.ParseUint(string(p[1]), 10, 64)
			if e != nil {
				return 0, ErrResource
			}
			return n * 1024, nil
		}
	}
	return 0, ErrResource
}
func checkNDW4Memory(memcost uint32, available func() (uint64, error)) error {
	if available == nil {
		available = memoryAvailableLinux
	}
	free, e := available()
	if e != nil || free < uint64(memcost)*1024+ndw4MemorySafetyReserve {
		return ErrResource
	}
	return nil
}
func ndw4Request(ctx context.Context, c *http.Client, url string, data map[string]string) (int, http.Header, error) {
	method := http.MethodGet
	var b []byte
	var e error
	if data != nil {
		method = http.MethodPost
		b, e = json.Marshal(data)
		if e != nil {
			return 0, nil, ErrProtocol
		}
		defer clear(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if e != nil {
		return 0, nil, ErrProtocol
	}
	req.Header.Set("Accept", "*/*")
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := c.Do(req)
	if e != nil {
		return 0, nil, ErrUnavailable
	}
	defer resp.Body.Close()
	n, e := io.Copy(io.Discard, io.LimitReader(resp.Body, ndw4MaxResponse+1))
	if e != nil || n > ndw4MaxResponse {
		return 0, nil, ErrProtocol
	}
	return resp.StatusCode, resp.Header.Clone(), nil
}

// verifyNDW4 runs only after GET 401, validated capability and fresh cookie.
// At most one active KDF per device process, with RAM reserve for Mihomo.
func verifyNDW4(ctx context.Context, c *http.Client, url, login, password string,
	available func() (uint64, error)) error {
	nonce := make([]byte, 16)
	if _, e := rand.Read(nonce); e != nil {
		return ErrInternal
	}
	clientNonce := base64.StdEncoding.EncodeToString(nonce)
	clear(nonce)
	status, headers, e := ndw4Request(ctx, c, url, map[string]string{"login": login, "nonce": clientNonce})
	if e != nil {
		return e
	}
	if status != http.StatusUnauthorized {
		return ErrProtocol
	}
	challenge, e := decodeNDW4Data(headers, []string{"salt", "nonce", "iter", "memcost"})
	if e != nil {
		return e
	}
	var saltText, serverNonce string
	var iter, memory uint32
	if json.Unmarshal(challenge["salt"], &saltText) != nil ||
		json.Unmarshal(challenge["nonce"], &serverNonce) != nil ||
		json.Unmarshal(challenge["iter"], &iter) != nil ||
		json.Unmarshal(challenge["memcost"], &memory) != nil {
		return ErrProtocol
	}
	salt, ok := decodeNDW4Base64(saltText)
	if !ok || len(salt) != 16 || len(serverNonce) == 0 || len(serverNonce) > 1024 ||
		!utf8.ValidString(serverNonce) || iter == 0 || iter > ndw4MaxIterations ||
		memory < 8 || memory > ndw4MaxMemoryKiB {
		clear(salt)
		return ErrProtocol
	}
	defer clear(salt)
	for _, b := range []byte(serverNonce) {
		if b < 32 || b == 127 {
			return ErrProtocol
		}
	}
	if e = checkNDW4Memory(memory, available); e != nil {
		return e
	}
	passwordBytes := []byte(password)
	key := argon2.IDKey(passwordBytes, salt, iter, memory, 1, 64)
	clear(passwordBytes)
	defer clear(key)
	clientKey := ndw4MAC(key, []byte("NDW4 Interactive Client Key"))
	defer clear(clientKey)
	stored := sha3.Sum512(clientKey)
	defer clear(stored[:])
	serverKey := ndw4MAC(key, []byte("NDW4 Interactive Server Key"))
	defer clear(serverKey)
	transcript := []byte("login1=" + login + ",nonce1=" + clientNonce +
		";iter2=" + strconv.FormatUint(uint64(iter), 10) + ",memcost2=" + strconv.FormatUint(uint64(memory), 10) +
		",nonce2=" + serverNonce + ",salt2=" + saltText + ";login3=" + login + ",nonce3=" + serverNonce)
	defer clear(transcript)
	proof := ndw4XOR(clientKey, ndw4MAC(stored[:], transcript))
	defer clear(proof)
	status, headers, e = ndw4Request(ctx, c, url, map[string]string{
		"login": login, "nonce": serverNonce, "proof": base64.StdEncoding.EncodeToString(proof),
	})
	if e != nil {
		return e
	}
	if status != http.StatusUnauthorized {
		return ErrProtocol
	}
	// A password mismatch is deliberately indistinguishable from an unknown
	// username; the native response contains no verified identity field.
	if len(headers.Values("X-NDM-Data")) == 0 {
		return ErrInvalidCredentials
	}
	confirmation, e := decodeNDW4Data(headers, []string{"signature"})
	if e != nil {
		return e
	}
	var signatureText string
	if json.Unmarshal(confirmation["signature"], &signatureText) != nil {
		return ErrProtocol
	}
	signature, ok := decodeNDW4Base64(signatureText)
	if !ok || len(signature) != 64 {
		clear(signature)
		return ErrProtocol
	}
	defer clear(signature)
	expected := ndw4MAC(serverKey, transcript)
	defer clear(expected)
	if subtle.ConstantTimeCompare(expected, signature) != 1 {
		return ErrProtocol
	}
	finalTranscript := append(append([]byte(nil), transcript...), []byte(";signature4="+signatureText)...)
	defer clear(finalTranscript)
	finalProof := ndw4XOR(clientKey, ndw4MAC(stored[:], finalTranscript))
	defer clear(finalProof)
	status, _, e = ndw4Request(ctx, c, url, map[string]string{
		"login": login, "nonce": serverNonce, "signature-proof": base64.StdEncoding.EncodeToString(finalProof),
	})
	if e != nil {
		return e
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrInvalidCredentials
	}
	if status != http.StatusOK {
		return ErrProtocol
	}
	status, _, e = ndw4Request(ctx, c, url, nil)
	if e != nil {
		return e
	}
	if status != http.StatusOK {
		return ErrProtocol
	}
	return nil
}
