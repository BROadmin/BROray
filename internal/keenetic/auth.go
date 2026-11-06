// Package keenetic verifies credentials on the router's LAN management endpoint.
package keenetic

import (
	"bytes"
	"context"
	"crypto/md5" // Required by Keenetic's NDW2 protocol, not used for app sessions.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidCredentials = errors.New("INVALID_CREDENTIALS")
	ErrUnavailable        = errors.New("KEENETIC_UNAVAILABLE")
	ErrProtocol           = errors.New("KEENETIC_AUTH_PROTOCOL_ERROR")
	ErrInternal           = errors.New("INTERNAL_ERROR")
)

type CredentialVerifier interface {
	Verify(context.Context, string, string) error
}

// Verifier holds configuration only; no credentials, cookies or live HTTP client.
type Verifier struct {
	endpoint        url.URL
	requestTimeout  time.Duration
	connectTimeout  time.Duration
	ndw4Once        sync.Once
	ndw4Slot        chan struct{}          // capacity 1, cancellation-aware memory-hard admission
	memoryAvailable func() (uint64, error) // nil on production: read /proc/meminfo
}

func NewVerifier(endpoint ManagementEndpoint) (*Verifier, error) {
	u, err := endpoint.URL()
	if err != nil {
		return nil, err
	}
	return &Verifier{endpoint: *u, requestTimeout: 10 * time.Second, connectTimeout: 5 * time.Second}, nil
}

// acquireNDW4 serializes Argon2id across authentication attempts in one
// BROray process without trapping disconnected clients in a Mutex.Lock.
// The slot is created lazily so existing fake Verifiers use the same guard.
func (v *Verifier) acquireNDW4(ctx context.Context) (func(), error) {
	v.ndw4Once.Do(func() { v.ndw4Slot = make(chan struct{}, 1) })
	if ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	select {
	case v.ndw4Slot <- struct{}{}:
		return func() { <-v.ndw4Slot }, nil
	case <-ctx.Done():
		return nil, ErrUnavailable
	}
}

// responseDigest is byte-compatible with BROray's printf | md5sum / sha256sum.
// UTF-8 Go strings are used verbatim: no password trim or normalization.
func responseDigest(login, password, realm, challenge string) string {
	input := make([]byte, 0, len(login)+len(realm)+len(password)+2)
	input = append(input, login...)
	input = append(input, ':')
	input = append(input, realm...)
	input = append(input, ':')
	input = append(input, password...)
	a := md5.Sum(input)
	clear(input)
	ah := make([]byte, 32)
	hex.Encode(ah, a[:])
	clear(a[:])
	joined := append([]byte(challenge), ah...)
	b := sha256.Sum256(joined)
	clear(joined)
	clear(ah)
	answer := hex.EncodeToString(b[:])
	clear(b[:])
	return answer
}

func discard(resp *http.Response) {
	// Bound both resource use and retention. No response content is logged/returned.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

func (v *Verifier) Verify(ctx context.Context, login, password string) error {
	login = NormalizeLogin(login)
	if login == "" || password == "" || !utf8.ValidString(login) || !utf8.ValidString(password) ||
		utf8.RuneCountInString(login) > 128 || utf8.RuneCountInString(password) > 512 {
		return ErrInvalidCredentials
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return ErrInternal
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: v.connectTimeout}).DialContext, ResponseHeaderTimeout: v.requestTimeout, MaxResponseHeaderBytes: 32 << 10, MaxIdleConnsPerHost: 1}
	client := &http.Client{Transport: transport, Jar: jar, Timeout: v.requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer func() {
		for _, c := range jar.Cookies(&v.endpoint) {
			c.Value = ""
			c.MaxAge = -1
			c.Expires = time.Unix(1, 0)
			c.Path = "/"
			jar.SetCookies(&v.endpoint, []*http.Cookie{c})
		}
		client.Jar = nil
		transport.CloseIdleConnections()
		password = ""
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.endpoint.String(), nil)
	if err != nil {
		return ErrInternal
	}
	req.Header.Set("Accept", "*/*")
	resp, err := client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	status := resp.StatusCode
	headers := resp.Header.Clone()
	discard(resp)
	if status != http.StatusUnauthorized || len(jar.Cookies(&v.endpoint)) == 0 {
		return ErrProtocol
	}
	mode, modeErr := selectAuthMode(headers)
	if modeErr != nil {
		return modeErr
	}
	if mode == authNDW4 {
		release, err := v.acquireNDW4(ctx)
		if err != nil {
			return err
		}
		err = verifyNDW4(ctx, client, v.endpoint.String(), login, password, v.memoryAvailable)
		release()
		if err != nil {
			return err
		}
		// Password accepted by KeeneticOS; now apply KeeneticOS's own rights.
		return v.authorize(ctx, client)
	}
	realms, challenges := headers.Values("X-NDM-Realm"), headers.Values("X-NDM-Challenge")
	if mode != authLegacy || len(realms) != 1 || len(challenges) != 1 ||
		realms[0] == "" || challenges[0] == "" {
		return ErrProtocol
	}
	digest := responseDigest(login, password, realms[0], challenges[0])
	password = ""
	body, err := json.Marshal(struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}{login, digest})
	digest = ""
	if err != nil {
		return ErrInternal
	}
	defer clear(body)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return ErrInternal
	}
	req.GetBody = nil // Explicitly disallow body replay; no redirects or auth retry.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	resp, err = client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	status = resp.StatusCode
	discard(resp)
	switch status {
	case http.StatusOK:
		// Password accepted by KeeneticOS; now apply KeeneticOS's own rights.
		return v.authorize(ctx, client)
	case http.StatusUnauthorized:
		return ErrInvalidCredentials
	default:
		return ErrProtocol
	}
}
