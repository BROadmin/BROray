package subscriptions

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/app"
)

// Download (broray_subscription_fetch, svc:609-819; lib/subscription-http.sh).
// Every hop re-validates the URL, resolves the host, refuses the hop if ANY
// resolved address is local/private/special, and connects only to the
// checked address (curl --resolve pinning). No proxy, ≤3 redirects,
// connect 10 s, 35 s per hop, body ≤2 MiB, headers ≤64 KiB.

var (
	// lookupIP resolves a host name (tests replace it).
	lookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	// addrAllowed is the public-address rule; tests replace it to reach httptest on 127.0.0.1.
	addrAllowed    = ipPublic
	connectTimeout = 10 * time.Second
	hopTimeout     = 35 * time.Second
	// tlsConfig is nil (system roots) in production; tests trust their own CA.
	tlsConfig *tls.Config
)

// defaultUserAgent is broray_subscription_default_user_agent.
func defaultUserAgent() string { return "BROray/" + appVersion() }

func appVersion() string {
	v := app.Version
	if v == "" || len(v) > 64 {
		return "unknown"
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '+' || c == '_' || c == '-') {
			return "unknown"
		}
	}
	return v
}

// headerValue is broray_subscription_header_value: ok=false when the header
// is repeated with different values (ambiguous).
func headerValue(h http.Header, name string) (string, bool) {
	vals := h.Values(name)
	if len(vals) == 0 {
		return "", true
	}
	first := strings.TrimSpace(vals[0])
	for _, v := range vals[1:] {
		if strings.TrimSpace(v) != first {
			return "", false
		}
	}
	return first, true
}

func headerTrue(h http.Header, name string) bool {
	v, ok := headerValue(h, name)
	return ok && strings.ToLower(v) == "true"
}

// httpDenial is broray_subscription_http_denial.
func httpDenial(h http.Header) *subErr {
	for _, f := range []string{"x-hwid-max-devices-reached", "x-hwid-limit", "x-hwid-not-supported"} {
		if _, ok := headerValue(h, f); !ok {
			return fail("HTTP_ERROR", "Провайдер вернул противоречивые служебные заголовки.")
		}
	}
	if headerTrue(h, "x-hwid-max-devices-reached") || headerTrue(h, "x-hwid-limit") {
		return fail("SUBSCRIPTION_DEVICE_LIMIT_REACHED", "Провайдер отклонил подписку: достигнут лимит зарегистрированных устройств.")
	}
	if headerTrue(h, "x-hwid-not-supported") {
		return fail("SUBSCRIPTION_DEVICE_ID_REJECTED", "Провайдер не принял анонимный идентификатор клиента подписки.")
	}
	return nil
}

// headerLines rebuilds "name: value" lines of the final response block.
func headerLines(h http.Header) []string {
	var out []string
	for k, vs := range h {
		for _, v := range vs {
			out = append(out, k+": "+v)
		}
	}
	return out
}

var (
	errTooLarge = fail("CONTENT_TOO_LARGE", "Ответ подписки превышает допустимый размер.")
	errDownload = fail("HTTP_ERROR", "Не удалось скачать подписку.")
	errTimeout  = fail("DOWNLOAD_TIMEOUT", "Сервер подписки не ответил вовремя.")
)

// resolvePublic is broray_subscription_resolve_public_ip: the checked address to dial.
func resolvePublic(ctx context.Context, host string) (netip.Addr, error) {
	if lit, isLiteral := literalIP(host); isLiteral {
		if !lit.IsValid() || !addrAllowed(lit) {
			return netip.Addr{}, fail("DOWNLOAD_SECURITY", "Локальный или служебный IP-адрес запрещён.")
		}
		return lit, nil
	}
	addrs, err := lookupIP(ctx, host)
	if ctx.Err() != nil {
		return netip.Addr{}, ctx.Err()
	}
	if err != nil || len(addrs) == 0 {
		return netip.Addr{}, fail("HTTP_ERROR", "Не удалось определить IP-адрес сервера подписки.")
	}
	for _, a := range addrs {
		if !addrAllowed(a.Unmap()) {
			return netip.Addr{}, fail("DOWNLOAD_SECURITY", "Имя подписки разрешается в локальный или служебный адрес.")
		}
	}
	return addrs[0].Unmap(), nil
}

// fetchInput is the part of the record the download uses.
type fetchInput struct {
	URL, Hwid, UserAgent string
	SendDevice           bool
	// Accept and Origin constrain representation discovery and advertised
	// exports. FinalURL is transient: private export URLs are never persisted.
	Accept, Origin string
	FinalURL       *string
}

// fetch downloads the subscription body. Provider metadata of the final
// response headers goes into md. Cancellation / the caller's deadline are
// returned as ctx.Err().
func (s *svc) fetch(ctx context.Context, in fetchInput, md *metadata) ([]byte, error) {
	ua := in.UserAgent
	if ua == "" {
		ua = defaultUserAgent()
	}
	if !printableASCII(ua, 128) {
		return nil, fail("INVALID_USER_AGENT", "User-Agent должен содержать не более 128 печатных ASCII-символов.")
	}
	if in.Hwid != "" && !hwidValid(in.Hwid) {
		return nil, fail("CLIENT_ID_ERROR", "Анонимный идентификатор клиента подписки повреждён.")
	}
	current, redirects := in.URL, 0
	share, deviceOrigin := in.SendDevice, ""
	var device map[string]string
	for {
		p, perr := parseURL(current)
		if perr != nil {
			return nil, perr
		}
		origin := normalizedOrigin(p)
		if in.Origin != "" && origin != in.Origin {
			return nil, exportError()
		}
		ip, err := resolvePublic(ctx, p.Host)
		if err != nil {
			return nil, err
		}
		if deviceOrigin == "" {
			deviceOrigin = origin
			if in.SendDevice {
				device = s.deviceInfo(ctx)
			}
		}
		hwid := in.Hwid
		if origin != deviceOrigin {
			share, hwid = false, ""
		}
		port, _ := strconv.Atoi(p.Port)
		resp, body, err := s.hop(ctx, current, netip.AddrPortFrom(ip, uint16(port)), ua, hwid, share, device, in.Accept)
		if err != nil {
			return nil, err
		}
		status := resp.StatusCode
		if c := status / 100; c == 2 || c == 4 || c == 5 {
			if e := httpDenial(resp.Header); e != nil {
				return nil, e
			}
		}
		encoding, encOK := headerValue(resp.Header, "Content-Encoding")
		encoding = strings.ToLower(encoding)
		if !encOK || !(encoding == "" || encoding == "identity" || encoding == "gzip" || encoding == "x-gzip" || encoding == "deflate") {
			return nil, fail("UNSUPPORTED_CONTENT", "Сервер прислал сжатый ответ в неподдерживаемом формате.")
		}
		switch {
		case status >= 200 && status <= 299:
			body, err := decodeBody(body, encoding)
			if err != nil {
				return nil, err
			}
			md.apply(metaRecordsFromLines(headerLines(resp.Header)))
			if in.FinalURL != nil {
				*in.FinalURL = current
			}
			return body, nil
		case status == 301 || status == 302 || status == 303 || status == 307 || status == 308:
			redirects++
			if redirects > 3 {
				return nil, fail("HTTP_ERROR", "Сервер подписки выполнил слишком много перенаправлений.")
			}
			location, _ := headerValue(resp.Header, "Location")
			if location == "" {
				return nil, fail("HTTP_ERROR", "Сервер подписки вернул перенаправление без адреса.")
			}
			next, rerr := resolveRedirect(current, location)
			if rerr != nil {
				return nil, rerr
			}
			if strings.HasPrefix(current, "https://") && strings.HasPrefix(next, "http://") {
				return nil, fail("HTTP_ERROR", "Переход с HTTPS на незащищённый HTTP отклонён.")
			}
			current = next
		default:
			return nil, fail("HTTP_ERROR", fmt.Sprintf("Сервер подписки вернул HTTP %03d.", status))
		}
	}
}

// hop performs one request to the pinned address.
func (s *svc) hop(ctx context.Context, rawURL string, addr netip.AddrPort, ua, hwid string, share bool, device map[string]string, accept string) (*http.Response, []byte, error) {
	hctx, cancel := context.WithTimeout(ctx, hopTimeout)
	defer cancel()
	dialer := &net.Dialer{Timeout: connectTimeout}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", addr.String())
		},
		TLSHandshakeTimeout:    connectTimeout,
		MaxResponseHeaderBytes: maxHeaderBytes,
		DisableCompression:     true,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      true,
	}
	if tlsConfig != nil {
		tr.TLSClientConfig = tlsConfig.Clone()
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, errDownload
	}
	req.Header.Set("User-Agent", ua)
	if accept == "" {
		accept = "application/json, text/plain, */*"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Encoding", "gzip")
	if hwid != "" {
		req.Header["x-hwid"] = []string{hwid}
	}
	if share {
		for _, h := range [][2]string{{"X-Device-OS", "os"}, {"X-Device-Model", "model"}, {"X-Ver-OS", "osVersion"}, {"X-App-Version", "appVersion"}} {
			if v := device[h[1]]; v != "" {
				req.Header[h[0]] = []string{v}
			}
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, s.netError(ctx, hctx, err)
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxBodyBytes {
		return nil, nil, errTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, nil, s.netError(ctx, hctx, err)
	}
	if len(body) > maxBodyBytes {
		return nil, nil, errTooLarge
	}
	return resp, body, nil
}

func (s *svc) netError(ctx, hctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if strings.Contains(err.Error(), "server response headers exceeded") {
		return fail("CONTENT_TOO_LARGE", "Заголовки ответа подписки превышают допустимый размер.")
	}
	var ne net.Error
	if hctx.Err() != nil || (errors.As(err, &ne) && ne.Timeout()) {
		return errTimeout
	}
	return errDownload
}

// decodeBody undoes gzip/deflate content coding with the same 2 MiB bound.
func decodeBody(body []byte, encoding string) ([]byte, error) {
	var r io.Reader
	switch encoding {
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, errDownload
		}
		r = zr
	case "deflate":
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, errDownload
		}
		r = zr
	default:
		return body, nil
	}
	out, err := io.ReadAll(io.LimitReader(r, maxBodyBytes+1))
	if err != nil {
		return nil, errDownload
	}
	if len(out) > maxBodyBytes {
		return nil, errTooLarge
	}
	return out, nil
}

// ---------- device information (lib/subscription-device-info.{sh,jq}) ----------

// showVersion reads `show version` (tests replace it).
var showVersion = func(ctx context.Context, s *svc) (map[string]any, bool) {
	if s.d.App == nil || s.d.App.T == nil {
		return nil, false
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var raw map[string]any
	if err := s.d.App.T.Show(cctx, "show/version", &raw); err != nil || raw == nil {
		return nil, false
	}
	return raw, true
}

func (s *svc) deviceInfo(ctx context.Context) map[string]string {
	out := map[string]string{}
	if raw, ok := showVersion(ctx, s); ok {
		out = deviceFilter(raw)
	}
	if v := appVersion(); v != "unknown" {
		out["appVersion"] = v
	}
	return out
}

func printable(v any, n int) bool {
	s, ok := v.(string)
	return ok && s != "" && printableASCII(s, n)
}

func modelCode(v any) string {
	s, ok := v.(string)
	if !ok || len(s) != 7 || !strings.HasPrefix(s, "KN-") || !allDigits(s[3:]) {
		return ""
	}
	return s
}

func versionText(v any) string {
	if !printable(v, 64) {
		return ""
	}
	s := v.(string)
	if s[0] < '0' || s[0] > '9' {
		return ""
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || strings.ContainsRune(" .-+_()", r)) {
			return ""
		}
	}
	return s
}

// deviceFilter is lib/subscription-device-info.jq: model family and OS
// version only, never unique identifiers.
func deviceFilter(m map[string]any) map[string]string {
	hardware := modelCode(m["hw_id"])
	model := ""
	if printable(m["model"], 128) {
		parts := strings.Split(m["model"].(string), "(")
		last := parts[len(parts)-1]
		last, _, _ = strings.Cut(last, ")")
		model = modelCode(last)
	}
	vendor := m["vendor"]
	brand := vendor == "Keenetic" || m["manufacturer"] == "Keenetic Ltd." || m["manufacturer"] == "Keenetic Limited"
	known := (brand || hardware != "" || model != "") && (vendor == nil || vendor == "" || vendor == "Keenetic" || vendor == "Zyxel")
	if !known {
		return map[string]string{}
	}
	code := model
	if hardware != "" && model != "" && hardware != model {
		code = ""
	} else if hardware != "" {
		code = hardware
	}
	out := map[string]string{"os": "KeeneticOS"}
	if code != "" {
		out["model"] = code
	}
	if t := versionText(m["title"]); t != "" {
		out["osVersion"] = t
	} else if r := versionText(m["release"]); r != "" {
		out["osVersion"] = r
	}
	return out
}
