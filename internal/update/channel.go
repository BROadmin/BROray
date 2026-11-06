package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// DefaultBaseURL is the built-in channel location: <base>/<channel>/manifest.json.
// The host is a placeholder (.invalid never resolves) until hosting is
// decided (design §10); release builds may set it with
// -ldflags "-X github.com/BROadmin/broray/internal/update.DefaultBaseURL=https://…".
var DefaultBaseURL = "https://updates.broray.invalid/channels"

// DefaultURL is the built-in manifest URL of channel.
func DefaultURL(channel string) string {
	return strings.TrimRight(DefaultBaseURL, "/") + "/" + channel + "/manifest.json"
}

// Source is where a channel manifest is read from.
type Source struct {
	Channel string
	URL     string
	// AllowTestKeys: the owner's config selects the test channel.
	AllowTestKeys bool
}

// SourceFrom resolves the channel from config.v1.json; channel ("" = the
// configured one) may override the name. The configured URL is used only
// for the configured channel.
func SourceFrom(cfg platform.Config, channel string) (Source, error) {
	conf := cfg.Update.Channel
	if conf == "" {
		conf = "stable"
	}
	if channel == "" {
		channel = conf
	}
	if !ValidChannel(channel) {
		return Source{}, fail(ErrChannelInvalid, nil, "Недопустимое имя канала обновлений «%s».", channel)
	}
	s := Source{Channel: channel, URL: DefaultURL(channel), AllowTestKeys: conf == "test" && channel == "test"}
	if channel == conf && strings.TrimSpace(cfg.Update.ChannelURL) != "" {
		s.URL = strings.TrimSpace(cfg.Update.ChannelURL)
	}
	return s, nil
}

// Fetcher reads https:// and file:// URLs. No other scheme is accepted.
type Fetcher struct {
	Client *http.Client
}

func NewFetcher() *Fetcher {
	// Direct connection (no proxy from the environment), like the Xray
	// installer; the signature and SHA-256 protect the content.
	tr := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 60 * time.Second, TLSHandshakeTimeout: 30 * time.Second}
	return &Fetcher{Client: &http.Client{Transport: tr, Timeout: 30 * time.Minute}}
}

var errNotFound = errors.New("not found")

func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "https":
		if u.Host == "" {
			return nil, errors.New("https URL without host")
		}
	case "file":
		if (u.Host != "" && u.Host != "localhost") || !strings.HasPrefix(u.Path, "/") {
			return nil, errors.New("file URL must be absolute (file:///path)")
		}
	default:
		return nil, fmt.Errorf("scheme %q is not allowed (https:// or file:// only)", u.Scheme)
	}
	if u.User != nil {
		return nil, errors.New("credentials in URL are not allowed")
	}
	return u, nil
}

// open returns the body and the announced length (-1 if unknown).
func (f *Fetcher) open(ctx context.Context, u *url.URL) (io.ReadCloser, int64, error) {
	if u.Scheme == "file" {
		fh, err := os.Open(u.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, -1, errNotFound
		}
		if err != nil {
			return nil, -1, err
		}
		st, err := fh.Stat()
		if err != nil || !st.Mode().IsRegular() {
			fh.Close()
			return nil, -1, fmt.Errorf("%s: not a regular file", u.Path)
		}
		return fh, st.Size(), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	client := f.Client
	if client == nil {
		client = NewFetcher().Client
	}
	// Check each redirect before Client sends the next request. Checking only
	// resp.Request.URL is too late: an HTTPS endpoint may redirect to HTTP.
	guarded := *client
	originalRedirect := client.CheckRedirect
	guarded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.Scheme != "https" || next.URL.Host == "" || next.URL.User != nil {
			return errors.New("redirected away from https")
		}
		if originalRedirect != nil {
			return originalRedirect(next, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	resp, err := guarded.Do(req)
	if err != nil {
		return nil, -1, err
	}
	if resp.Request != nil && resp.Request.URL.Scheme != "https" {
		resp.Body.Close()
		return nil, -1, errors.New("redirected away from https")
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, -1, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, -1, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

// getSmall reads a whole small document (manifest, signature).
func (f *Fetcher) getSmall(ctx context.Context, u *url.URL, max int64) ([]byte, error) {
	body, n, err := f.open(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	if n > max {
		return nil, fmt.Errorf("document larger than %d bytes", max)
	}
	b, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("document larger than %d bytes", max)
	}
	return b, nil
}

// Checked is a manifest whose signature, channel and validity period
// were verified.
type Checked struct {
	Manifest *Manifest
	URL      string // manifest URL (artifact URLs resolve against it)
	SHA256   string // of the exact manifest bytes
	KeyID    string
}

// Check fetches <url> and <url>.sig and verifies them (design §3.1-3.2).
func (f *Fetcher) Check(ctx context.Context, src Source, now time.Time) (*Checked, error) {
	u, err := parseURL(src.URL)
	if err != nil {
		return nil, fail(ErrChannelInvalid, err, "Адрес канала обновлений недопустим: разрешены только https:// и file://.")
	}
	if u.Scheme == "https" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".invalid") {
		return nil, fail(ErrChannelUnavailable, nil, "Канал обновлений не настроен.")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fail(ErrChannelInvalid, nil, "Адрес канала обновлений не должен содержать параметры запроса.")
	}
	body, err := f.getSmall(ctx, u, MaxManifestBytes)
	if err != nil {
		return nil, fail(ErrChannelUnavailable, err, "Канал обновлений «%s» недоступен: не удалось загрузить манифест.", src.Channel)
	}
	su := *u
	su.Path += ".sig"
	sigBytes, err := f.getSmall(ctx, &su, maxSigSize)
	if errors.Is(err, errNotFound) {
		return nil, fail(ErrSignatureMissing, nil, "Подпись манифеста канала не найдена (%s.sig).", src.URL)
	}
	if err != nil {
		return nil, fail(ErrChannelUnavailable, err, "Канал обновлений «%s» недоступен: не удалось загрузить подпись манифеста.", src.Channel)
	}
	sig, err := ParseSignature(sigBytes)
	if err != nil {
		return nil, fail(ErrSignatureInvalid, err, "Подпись манифеста канала повреждена.")
	}
	var key *PublicKey
	for _, k := range keysFor(src.Channel, src.AllowTestKeys) {
		if k.ID == sig.KeyID {
			k := k
			key = &k
			break
		}
	}
	if key == nil {
		return nil, fail(ErrKeyUnknown, nil, "Манифест канала «%s» подписан ключом %s, которому эта версия BROray не доверяет.", src.Channel, keyIDString(sig.KeyID))
	}
	if err := sig.Verify(*key, body); err != nil {
		return nil, fail(ErrSignatureInvalid, err, "Подпись манифеста канала не прошла проверку.")
	}
	m, err := ParseManifest(body)
	if err != nil {
		return nil, fail(ErrIndexInvalid, err, "Манифест канала повреждён или имеет неподдерживаемый формат.")
	}
	if m.Channel != src.Channel {
		return nil, fail(ErrChannelMismatch, nil, "Манифест выпущен для канала «%s», а настроен канал «%s».", m.Channel, src.Channel)
	}
	if err := m.CheckTime(now); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	return &Checked{Manifest: m, URL: u.String(), SHA256: hex.EncodeToString(sum[:]), KeyID: key.IDString()}, nil
}

// resolve turns an artifact URL (possibly relative) into an absolute one.
// file:// artifacts are allowed only from a file:// manifest.
func resolve(manifestURL, ref string) (*url.URL, error) {
	base, err := parseURL(manifestURL)
	if err != nil {
		return nil, err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	u, err := parseURL(base.ResolveReference(r).String())
	if err != nil {
		return nil, err
	}
	if u.Scheme == "file" && base.Scheme != "file" {
		return nil, errors.New("file:// artifact in an https manifest")
	}
	return u, nil
}
