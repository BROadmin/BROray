package servers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrUnsupported = errors.New("SERVER_PROTOCOL_UNSUPPORTED")
	ErrInvalid     = errors.New("SERVER_LINK_INVALID")
	reUUID         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

var validNetworks = map[string]bool{"raw": true, "tcp": true, "ws": true, "grpc": true, "httpupgrade": true, "xhttp": true, "kcp": true, "h2": false}
var validSecurity = map[string]bool{"none": true, "tls": true, "reality": true}

// ParseLink parses one share link: vless://, vmess://, trojan://, ss://, hysteria2:// / hy2://.
// tuic/socks/http are recognized and refused, like 3.x.
func ParseLink(link string) (Server, error) {
	link = strings.TrimSpace(link)
	if link == "" || len(link) > 8192 || !utf8.ValidString(link) {
		return Server{}, ErrInvalid
	}
	lower := strings.ToLower(link)
	switch {
	case strings.HasPrefix(lower, "vless://"):
		return parseVLESS(link)
	case strings.HasPrefix(lower, "vmess://"):
		return parseVMess(link)
	case strings.HasPrefix(lower, "trojan://"):
		return parseTrojan(link)
	case strings.HasPrefix(lower, "ss://"):
		return parseShadowsocks(link)
	case strings.HasPrefix(lower, "hysteria2://"), strings.HasPrefix(lower, "hy2://"):
		return parseHysteria2(link)
	case strings.HasPrefix(lower, "tuic://"), strings.HasPrefix(lower, "socks://"), strings.HasPrefix(lower, "socks5://"), strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return Server{}, fmt.Errorf("%w: %s", ErrUnsupported, strings.SplitN(lower, ":", 2)[0])
	}
	return Server{}, ErrInvalid
}

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("%w: host/port", ErrInvalid)
	}
	if strings.ContainsAny(host, " \"'") {
		return "", 0, fmt.Errorf("%w: host", ErrInvalid)
	}
	return host, port, nil
}

func fragmentName(u *url.URL, fallback string) string {
	n := strings.TrimSpace(u.Fragment)
	if n == "" {
		n = fallback
	}
	if len([]rune(n)) > 96 {
		n = string([]rune(n)[:96])
	}
	return n
}

func q(v url.Values, keys ...string) string {
	for _, k := range keys {
		if s := v.Get(k); s != "" {
			return s
		}
	}
	return ""
}

func parseStream(s *Server, v url.Values, defaultNetwork string) error {
	network := q(v, "type", "net")
	if network == "" {
		network = defaultNetwork
	}
	if network == "tcp" {
		network = "raw"
	}
	if !validNetworks[network] {
		return fmt.Errorf("%w: transport %s", ErrUnsupported, network)
	}
	s.Network = network
	sec := q(v, "security")
	if sec == "" {
		sec = "none"
	}
	if !validSecurity[sec] {
		return fmt.Errorf("%w: security %s", ErrUnsupported, sec)
	}
	s.Security = sec
	switch strings.ToLower(q(v, "allowInsecure", "insecure")) {
	case "", "0", "false", "no":
	default:
		return fmt.Errorf("%w: allowInsecure is not supported", ErrUnsupported)
	}
	sni := q(v, "sni", "peer")
	fp := q(v, "fp")
	if fp == "" {
		fp = "chrome"
	}
	if sec == "tls" && sni == "" {
		sni = s.Address
	}
	s.TLS.ServerName, s.TLS.Fingerprint = sni, fp
	if alpn := q(v, "alpn"); alpn != "" {
		for _, a := range strings.Split(alpn, ",") {
			if a = strings.TrimSpace(a); a != "" {
				s.TLS.ALPN = append(s.TLS.ALPN, a)
			}
		}
	}
	if s.TLS.ALPN == nil {
		s.TLS.ALPN = []string{}
	}
	s.Reality.ServerName, s.Reality.Fingerprint = sni, fp
	s.Reality.PublicKey, s.Reality.ShortID, s.Reality.SpiderX = q(v, "pbk"), q(v, "sid"), q(v, "spx")
	if sec == "reality" {
		if sni == "" || s.Reality.PublicKey == "" {
			return fmt.Errorf("%w: reality needs sni and pbk", ErrInvalid)
		}
		if network != "raw" && network != "grpc" && network != "xhttp" {
			return fmt.Errorf("%w: reality over %s", ErrUnsupported, network)
		}
	}
	s.Transport.Host = q(v, "host")
	s.Transport.Path = q(v, "path")
	if s.Transport.Path == "" {
		s.Transport.Path = "/"
	}
	s.Transport.ServiceName = q(v, "serviceName", "service_name")
	s.Transport.Mode = q(v, "mode")
	if s.Transport.Mode == "" {
		s.Transport.Mode = "auto"
	}
	if network == "grpc" && s.Transport.Mode != "auto" && s.Transport.Mode != "gun" && s.Transport.Mode != "multi" {
		return fmt.Errorf("%w: grpc mode", ErrUnsupported)
	}
	s.Transport.HeaderType = q(v, "headerType")
	if s.Transport.HeaderType == "" {
		s.Transport.HeaderType = "none"
	}
	if extra := q(v, "extra"); extra != "" {
		var obj map[string]any
		if json.Unmarshal([]byte(extra), &obj) != nil {
			return fmt.Errorf("%w: extra json", ErrInvalid)
		}
		s.Transport.Extra = json.RawMessage(extra)
	}
	return nil
}

func parseVLESS(link string) (Server, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil {
		return Server{}, ErrInvalid
	}
	s := Server{SchemaVersion: 2, Protocol: "vless", URI: link}
	s.Address, s.Port, err = hostPort(u)
	if err != nil {
		return Server{}, err
	}
	s.UUID = u.User.Username()
	if s.UUID == "" {
		return Server{}, fmt.Errorf("%w: uuid", ErrInvalid)
	}
	v := u.Query()
	if err := parseStream(&s, v, "raw"); err != nil {
		return Server{}, err
	}
	s.Encryption = q(v, "encryption")
	if s.Encryption == "" {
		s.Encryption = "none"
	}
	s.Flow = q(v, "flow")
	switch s.Flow {
	case "", "xtls-rprx-vision", "xtls-rprx-vision-udp443":
	default:
		return Server{}, fmt.Errorf("%w: flow %s", ErrUnsupported, s.Flow)
	}
	if s.Flow != "" && (s.Network != "raw" || (s.Security != "tls" && s.Security != "reality")) && s.Encryption == "none" {
		return Server{}, fmt.Errorf("%w: vision needs raw + tls/reality", ErrInvalid)
	}
	s.Name = fragmentName(u, s.Address+":"+strconv.Itoa(s.Port))
	return s, nil
}

func parseTrojan(link string) (Server, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil {
		return Server{}, ErrInvalid
	}
	s := Server{SchemaVersion: 2, Protocol: "trojan", URI: link}
	s.Address, s.Port, err = hostPort(u)
	if err != nil {
		return Server{}, err
	}
	s.Password = u.User.Username()
	if p, ok := u.User.Password(); ok && p != "" {
		s.Password = s.Password + ":" + p
	}
	if s.Password == "" {
		return Server{}, fmt.Errorf("%w: password", ErrInvalid)
	}
	v := u.Query()
	if v.Get("security") == "" {
		v.Set("security", "tls")
	}
	if err := parseStream(&s, v, "raw"); err != nil {
		return Server{}, err
	}
	s.Name = fragmentName(u, s.Address+":"+strconv.Itoa(s.Port))
	return s, nil
}

func b64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, ErrInvalid
}

func parseVMess(link string) (Server, error) {
	payload := strings.TrimPrefix(strings.TrimPrefix(link, "vmess://"), "VMESS://")
	raw, err := b64(payload)
	if err != nil {
		return Server{}, fmt.Errorf("%w: vmess base64", ErrInvalid)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return Server{}, fmt.Errorf("%w: vmess json", ErrInvalid)
	}
	str := func(k string) string {
		switch x := m[k].(type) {
		case string:
			return x
		case float64:
			return strconv.Itoa(int(x))
		}
		return ""
	}
	s := Server{SchemaVersion: 2, Protocol: "vmess", URI: link}
	s.Address = str("add")
	s.Port, _ = strconv.Atoi(str("port"))
	s.UUID = str("id")
	if s.Address == "" || s.Port < 1 || s.Port > 65535 || s.UUID == "" {
		return Server{}, fmt.Errorf("%w: vmess fields", ErrInvalid)
	}
	s.AlterID, _ = strconv.Atoi(str("aid"))
	s.Encryption = str("scy")
	if s.Encryption == "" {
		s.Encryption = "auto"
	}
	v := url.Values{}
	v.Set("type", str("net"))
	v.Set("security", map[bool]string{true: "tls", false: "none"}[str("tls") == "tls"])
	v.Set("sni", str("sni"))
	v.Set("fp", str("fp"))
	v.Set("host", str("host"))
	v.Set("path", str("path"))
	v.Set("alpn", str("alpn"))
	v.Set("headerType", str("type"))
	if str("net") == "grpc" {
		v.Set("serviceName", str("path"))
		v.Set("mode", str("mode"))
	}
	if err := parseStream(&s, v, "raw"); err != nil {
		return Server{}, err
	}
	s.Name = strings.TrimSpace(str("ps"))
	if s.Name == "" {
		s.Name = s.Address + ":" + strconv.Itoa(s.Port)
	}
	return s, nil
}

func parseShadowsocks(link string) (Server, error) {
	// SIP002: ss://BASE64(method:password)@host:port#name  or legacy ss://BASE64(method:password@host:port)#name
	s := Server{SchemaVersion: 2, Protocol: "shadowsocks", URI: link, Network: "raw", Security: "none"}
	body := link[len("ss://"):]
	frag := ""
	if i := strings.Index(body, "#"); i >= 0 {
		frag, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	if i := strings.Index(body, "?"); i >= 0 {
		body = body[:i] // plugins are not supported; ignore query
	}
	var userinfo, hp string
	if i := strings.LastIndex(body, "@"); i >= 0 {
		userinfo, hp = body[:i], body[i+1:]
		if dec, err := b64(userinfo); err == nil && strings.Contains(string(dec), ":") {
			userinfo = string(dec)
		} else if u, err := url.PathUnescape(userinfo); err == nil {
			userinfo = u
		}
	} else {
		dec, err := b64(body)
		if err != nil {
			return Server{}, fmt.Errorf("%w: ss base64", ErrInvalid)
		}
		at := strings.LastIndex(string(dec), "@")
		if at < 0 {
			return Server{}, fmt.Errorf("%w: ss legacy", ErrInvalid)
		}
		userinfo, hp = string(dec)[:at], string(dec)[at+1:]
	}
	mp := strings.SplitN(userinfo, ":", 2)
	if len(mp) != 2 || mp[0] == "" || mp[1] == "" {
		return Server{}, fmt.Errorf("%w: ss method:password", ErrInvalid)
	}
	s.Method, s.Password = mp[0], mp[1]
	host, portS, err := net.SplitHostPort(hp)
	if err != nil {
		return Server{}, fmt.Errorf("%w: ss host:port", ErrInvalid)
	}
	s.Address = host
	s.Port, err = strconv.Atoi(portS)
	if err != nil || s.Port < 1 || s.Port > 65535 {
		return Server{}, fmt.Errorf("%w: ss port", ErrInvalid)
	}
	switch s.Method {
	case "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305", "none", "plain":
	default:
		return Server{}, fmt.Errorf("%w: ss method %s", ErrUnsupported, s.Method)
	}
	s.TLS.ALPN = []string{}
	s.Transport.Path, s.Transport.Mode, s.Transport.HeaderType = "/", "auto", "none"
	s.Name = frag
	if s.Name == "" {
		s.Name = s.Address + ":" + strconv.Itoa(s.Port)
	}
	return s, nil
}

func parseHysteria2(link string) (Server, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil {
		return Server{}, ErrInvalid
	}
	s := Server{SchemaVersion: 2, Protocol: "hysteria2", URI: link, Network: "hysteria", Security: "tls"}
	s.Address, s.Port, err = hostPort(u)
	if err != nil {
		return Server{}, err
	}
	s.Auth = u.User.Username()
	if p, ok := u.User.Password(); ok && p != "" {
		s.Auth += ":" + p
	}
	if s.Auth == "" {
		return Server{}, fmt.Errorf("%w: hysteria2 auth", ErrInvalid)
	}
	v := u.Query()
	switch strings.ToLower(q(v, "insecure", "allowInsecure")) {
	case "", "0", "false":
	default:
		return Server{}, fmt.Errorf("%w: insecure is not supported", ErrUnsupported)
	}
	s.TLS.ServerName = q(v, "sni")
	if s.TLS.ServerName == "" {
		s.TLS.ServerName = s.Address
	}
	s.TLS.Fingerprint = "chrome"
	s.TLS.ALPN = []string{"h3"}
	s.Hysteria.Obfs = q(v, "obfs")
	s.Hysteria.ObfsPassword = q(v, "obfs-password")
	if s.Hysteria.Obfs != "" && s.Hysteria.Obfs != "salamander" && s.Hysteria.Obfs != "gecko" {
		return Server{}, fmt.Errorf("%w: obfs %s", ErrUnsupported, s.Hysteria.Obfs)
	}
	if s.Hysteria.Obfs != "" && s.Hysteria.ObfsPassword == "" {
		return Server{}, fmt.Errorf("%w: obfs-password", ErrInvalid)
	}
	s.Hysteria.Ports = q(v, "mport", "ports")
	s.Transport.Path, s.Transport.Mode, s.Transport.HeaderType = "/", "auto", "none"
	s.Name = fragmentName(u, s.Address+":"+strconv.Itoa(s.Port))
	return s, nil
}

// Validate checks a stored record (own or imported from 3.x).
func Validate(s Server) error {
	if !ValidID(s.ID) || s.Address == "" || s.Port < 1 || s.Port > 65535 {
		return ErrInvalid
	}
	switch s.Protocol {
	case "vless", "vmess":
		if s.UUID == "" {
			return ErrInvalid
		}
	case "trojan":
		if s.Password == "" {
			return ErrInvalid
		}
	case "shadowsocks":
		if s.Password == "" || s.Method == "" {
			return ErrInvalid
		}
	case "hysteria2":
		if s.Auth == "" {
			return ErrInvalid
		}
	default:
		return ErrUnsupported
	}
	return nil
}
