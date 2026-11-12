package clashimport

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/xrayimport"
	"golang.org/x/crypto/ssh"
)

var remoteBackends = map[string]string{
	"anytls": "core-unsupported", "easytier": "core-unsupported", "gost-relay": "core-unsupported",
	"hysteria": "core-unsupported", "masque": "conversion-unimplemented", "mieru": "core-unsupported",
	"openvpn": "core-unsupported", "shadowquic": "core-unsupported", "snell": "core-unsupported",
	"ssh": "core-unsupported", "ssr": "core-unsupported", "sudoku": "core-unsupported",
	"tailscale": "core-unsupported", "trusttunnel": "core-unsupported", "tuic": "core-unsupported", "zerotier": "core-unsupported",
}

func requireText(p map[string]any, key string) string {
	s := stringField(p, key, "")
	need(s != "")
	return s
}
func enum(p map[string]any, key string, values ...string) {
	if has(p, key) && p[key] != nil {
		need(inList(stringField(p, key, ""), values...))
	}
}
func integerBounds(p map[string]any, key string, lo, hi float64) {
	if has(p, key) && p[key] != nil {
		need(isInt(p[key], lo, hi))
	}
}
func listStrings(v any) []string {
	a, ok := v.([]any)
	need(ok)
	out := make([]string, 0, len(a))
	for _, item := range a {
		need(textValue(item))
		out = append(out, item.(string))
	}
	return out
}
func sourceEndpoint(p map[string]any) {
	host := requireText(p, "server")
	need(!strings.ContainsAny(host, " /?#@") && isInt(p["port"], 1, 65535))
}
func sourceURL(s string, schemes ...string) {
	u, err := url.Parse(s)
	need(err == nil && u.Hostname() != "" && inList(u.Scheme, schemes...))
}
func sourcePrefix(s string) {
	if _, err := netip.ParsePrefix(s); err != nil {
		_, err = netip.ParseAddr(s)
		need(err == nil)
	}
}
func sourcePortRange(s string) {
	for _, part := range strings.Split(s, ",") {
		ends := strings.FieldsFunc(part, func(c rune) bool { return c == '-' || c == ':' })
		need(len(ends) == 1 || len(ends) == 2)
		previous := 0
		for _, end := range ends {
			n, err := strconv.Atoi(end)
			need(err == nil && n >= 1 && n <= 65535 && n >= previous)
			previous = n
		}
	}
}

var bandwidth = regexp.MustCompile(`(?i)^\s*[0-9]+(?:\.[0-9]+)?\s*(?:[kmgt]?bps|[kmgt]?b|[kmgt]bit/s|[kmgt]byte/s)?\s*$`)

func sourceBandwidth(s string, required bool) {
	if s == "" && !required {
		return
	}
	need(bandwidth.MatchString(s))
	if required {
		digits := regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?`).FindString(strings.TrimSpace(s))
		n, err := strconv.ParseFloat(digits, 64)
		need(err == nil && n > 0)
	}
}
func pemCertificates(s string) {
	need(len(s) <= 128*1024)
	count := 0
	for len(strings.TrimSpace(s)) != 0 {
		block, rest := pem.Decode([]byte(s))
		need(block != nil && block.Type == "CERTIFICATE")
		_, err := x509.ParseCertificate(block.Bytes)
		need(err == nil)
		count++
		s = string(rest)
	}
	need(count > 0)
}

// validateRemote validates adapter-level requirements without creating a
// backend, reading referenced files, resolving names, or starting a tunnel.
// Library-private formats that are not implemented here remain explicitly
// unavailable; field_dispositions.json states that validation boundary.
func validateRemote(typ string, p map[string]any, checks xrayimport.Checks) {
	need(textValue(p["name"]))
	if !inList(typ, "easytier", "tailscale", "zerotier") {
		if typ == "mieru" && (!has(p, "port") || isInt(p["port"], 0, 0)) && stringField(p, "port-range", "") != "" {
			requireText(p, "server")
			sourcePortRange(requireText(p, "port-range"))
		} else if typ == "hysteria" && (!has(p, "port") || isInt(p["port"], 0, 0)) && stringField(p, "ports", "") != "" {
			requireText(p, "server")
			sourcePortRange(requireText(p, "ports"))
		} else {
			sourceEndpoint(p)
		}
	}
	enum(p, "ip-version", "dual", "ipv4", "ipv6", "ipv4-prefer", "ipv6-prefer")
	validateSourceSecurity(p, checks)
	validateIPStack(objectField(p, "ip-stack"))
	switch typ {
	case "tuic":
		validateTUIC(p, checks)
	case "anytls":
		requireText(p, "password")
		for _, k := range []string{"idle-session-check-interval", "idle-session-timeout", "min-idle-session"} {
			integerBounds(p, k, 0, 2147483647)
		}
	case "gost-relay", "trusttunnel":
		for _, k := range []string{"username", "password"} {
			stringField(p, k, "")
		}
		for _, k := range []string{"max-connections", "min-streams", "max-streams"} {
			integerBounds(p, k, 0, 2147483647)
		}
	case "hysteria":
		sourceBandwidth(stringField(p, "up", ""), true)
		sourceBandwidth(stringField(p, "down", ""), true)
		enum(p, "protocol", "", "udp", "faketcp", "wechat-video")
		enum(p, "obfs-protocol", "", "udp", "faketcp", "wechat-video")
		if auth := stringField(p, "auth", ""); auth != "" {
			_, err := base64.StdEncoding.DecodeString(auth)
			need(err == nil)
		}
		if ports := stringField(p, "ports", ""); ports != "" {
			sourcePortRange(ports)
		}
	case "masque":
		private, err := base64.StdEncoding.DecodeString(requireText(p, "private-key"))
		need(err == nil)
		_, err = x509.ParseECPrivateKey(private)
		need(err == nil)
		public, err := base64.StdEncoding.DecodeString(requireText(p, "public-key"))
		need(err == nil)
		key, err := x509.ParsePKIXPublicKey(public)
		need(err == nil)
		_, ok := key.(*ecdsa.PublicKey)
		need(ok)
		enum(p, "network", "", "h2", "h3", "h3-l4proxy")
		if p["network"] != "h3-l4proxy" {
			need(stringField(p, "ip", "") != "" || stringField(p, "ipv6", "") != "")
		}
		for _, k := range []string{"ip", "ipv6"} {
			if s := stringField(p, k, ""); s != "" {
				sourcePrefix(s)
			}
		}
		integerBounds(p, "handshake-timeout", 0, 2147483647)
	case "mieru":
		requireText(p, "username")
		requireText(p, "password")
		enum(p, "transport", "TCP", "UDP")
		need(has(p, "transport"))
		if r := stringField(p, "port-range", ""); r != "" {
			need(!has(p, "port") || isInt(p["port"], 0, 0))
			sourcePortRange(r)
		}
		enum(p, "multiplexing", "", "MULTIPLEXING_OFF", "MULTIPLEXING_LOW", "MULTIPLEXING_MIDDLE", "MULTIPLEXING_HIGH")
		if s := stringField(p, "traffic-pattern", ""); s != "" {
			_, err := base64.StdEncoding.DecodeString(s)
			need(err == nil)
		}
	case "openvpn":
		pemCertificates(requirePEMText(p, "ca"))
		if has(p, "cert") || has(p, "key") {
			_, err := tls.X509KeyPair([]byte(requirePEMText(p, "cert")), []byte(requirePEMText(p, "key")))
			need(err == nil)
		}
		enum(p, "proto", "", "udp", "tcp", "tcp-client", "udp4", "udp6", "tcp4", "tcp6")
		enum(p, "dev", "", "tun")
		enum(p, "key-direction", "", "0", "1")
		for _, k := range []string{"tran-window", "handshake-timeout", "ping", "ping-restart"} {
			integerBounds(p, k, 0, 9223372036)
		}
		if stringField(p, "tls-crypt", "") != "" {
			need(stringField(p, "tls-crypt-v2", "") == "" && stringField(p, "tls-auth", "") == "")
		}
	case "shadowquic":
		if has(p, "quic-versions") {
			for _, v := range listStrings(p["quic-versions"]) {
				need(inList(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(v)), "_", "-"), "v1", "v2", "1", "2", "rfc9000", "rfc-9000", "rfc9369", "rfc-9369"))
			}
		}
		for _, k := range []string{"up", "down"} {
			sourceBandwidth(stringField(p, k, ""), false)
		}
	case "snell":
		requireText(p, "psk")
		integerBounds(p, "version", 0, 6)
		version := stringFieldNumber(p, "version", 0)
		if version == 1 || version == 2 {
			need(p["udp"] != true)
		}
		if version != 6 {
			need(stringField(p, "mode", "") == "")
		} else {
			need(len(p["psk"].(string)) >= 12 && len(p["psk"].(string)) <= 255)
			enum(p, "mode", "", "default", "unshaped", "unsafe-raw")
		}
		if has(p, "obfs-opts") {
			o := obj(p["obfs-opts"])
			enum(o, "mode", "", "http", "tls", "shadow-tls", "restls", "jls")
			if version == 6 {
				need(!inList(stringField(o, "mode", ""), "http", "tls"))
			}
			validateSourceSecurity(o, checks)
		}
	case "ssh":
		requireText(p, "username")
		if s, _ := p["private-key"].(string); s != "" && strings.Contains(s, "PRIVATE KEY") {
			need(len(s) <= 128*1024)
			var err error
			if pass := stringField(p, "private-key-passphrase", ""); pass != "" {
				_, err = ssh.ParsePrivateKeyWithPassphrase([]byte(s), []byte(pass))
			} else {
				_, err = ssh.ParsePrivateKey([]byte(s))
			}
			need(err == nil)
		}
		if has(p, "host-key") {
			for _, s := range listStrings(p["host-key"]) {
				_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s))
				need(err == nil)
			}
		}
	case "ssr":
		requireText(p, "password")
		enum(p, "cipher", "none", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb", "aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "rc4", "rc4-md5", "chacha20", "chacha20-ietf", "salsa20", "bf-cfb", "cast5-cfb", "des-cfb", "idea-cfb", "rc2-cfb", "seed-cfb")
		enum(p, "protocol", "origin", "auth_sha1_v4", "auth_aes128_md5", "auth_aes128_sha1", "auth_chain_a", "auth_chain_b")
		enum(p, "obfs", "plain", "http_simple", "http_post", "random_head", "tls1.2_ticket_auth", "tls1.2_ticket_fastauth")
		need(has(p, "cipher") && has(p, "protocol") && has(p, "obfs"))
	case "sudoku":
		requireText(p, "key")
		enum(p, "table-type", "", "prefer_ascii", "prefer_entropy", "up_ascii_down_entropy", "up_entropy_down_ascii")
		enum(p, "aead-method", "", "chacha20-poly1305", "aes-128-gcm", "none")
		for _, k := range []string{"padding-min", "padding-max"} {
			integerBounds(p, k, 0, 100)
		}
		if has(p, "padding-min") && p["padding-min"] != nil && has(p, "padding-max") && p["padding-max"] != nil {
			need(stringFieldNumber(p, "padding-min", 0) <= stringFieldNumber(p, "padding-max", 0))
		}
		enum(p, "multiplex", "", "off", "auto", "on")
		enum(p, "http-mask-multiplex", "", "off", "auto", "on")
		enum(p, "http-mask-mode", "", "legacy", "stream", "poll", "auto", "ws")
		if v := p["httpmask"]; v != nil {
			o := obj(v)
			enum(o, "mode", "", "legacy", "stream", "poll", "auto", "ws")
			enum(o, "multiplex", "", "off", "auto", "on")
		}
	case "tailscale":
		if s := stringField(p, "control-url", ""); s != "" {
			sourceURL(s, "http", "https")
		}
		if s := stringField(p, "exit-node", ""); s != "" {
			need(textValue(s) && !strings.ContainsAny(s, " /?#@"))
		}
	case "easytier":
		if s := stringField(p, "ipv4", ""); s != "" {
			sourcePrefix(s)
		}
		for _, k := range []string{"peers", "listeners", "mapped-listeners"} {
			if has(p, k) {
				for _, s := range listStrings(p[k]) {
					sourceURL(s, "tcp", "udp", "ws", "wss", "quic", "wg", "ring")
				}
			}
		}
		for _, k := range []string{"exit-nodes", "proxy-networks"} {
			if has(p, k) {
				for _, s := range listStrings(p[k]) {
					sourcePrefix(s)
				}
			}
		}
		integerBounds(p, "mtu", 0, 65535)
	case "zerotier":
		hexIdentifier(requireText(p, "network"), 16)
		integerBounds(p, "mtu", 0, 10000)
		integerBounds(p, "physical-mtu", 0, 10000)
		for _, k := range []string{"primary-port", "secondary-port"} {
			integerBounds(p, k, 0, 65535)
		}
		if s := stringField(p, "remote-trace-target", ""); s != "" {
			hexIdentifier(s, 10)
		}
		if has(p, "orbit") {
			seen := map[string]bool{}
			for _, value := range p["orbit"].([]any) {
				o := obj(value)
				world := requireText(o, "world")
				hexIdentifier(world, 16)
				hexIdentifier(requireText(o, "seed"), 10)
				need(!seen[world])
				seen[world] = true
			}
		}
	}
}

func requirePEMText(p map[string]any, key string) string {
	s, ok := p[key].(string)
	need(ok && len(s) > 0 && len(s) <= 128*1024)
	return s
}
func hexIdentifier(s string, length int) {
	need(len(s) == length)
	b, err := hex.DecodeString(s)
	need(err == nil)
	nonzero := false
	for _, v := range b {
		nonzero = nonzero || v != 0
	}
	need(nonzero)
}
func stringFieldNumber(p map[string]any, key string, fallback int) int {
	if !has(p, key) || p[key] == nil {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(p[key])))
	need(err == nil)
	return n
}

func validateIPStack(p map[string]any) {
	need(inList(strings.ToLower(stringField(p, "mode", "")), "", "auto", "gvisor", "mips"))
	need(inList(strings.ToLower(stringField(p, "congestion-controller", "")), "", "cubic", "reno", "bbr", "bbr3"))
}

func validateSourceSecurity(p map[string]any, checks xrayimport.Checks) {
	t := mappedFields(p, map[string]string{"fingerprint": "pinnedPeerCertSha256", "name-cert-verify": "verifyPeerCertByName"})
	if len(t) > 0 {
		need(checks.ValidateClientFields != nil && checks.ValidateClientFields("vless", "raw", "tls", map[string]any{"tls": t}) == nil)
	}
	cert, _ := p["certificate"].(string)
	key, _ := p["private-key"].(string)
	// MASQUE and SSH have different private-key grammars; only a TLS
	// certificate activates this paired PEM validation.
	if cert != "" {
		need(key != "")
		if strings.Contains(cert, "BEGIN") || strings.Contains(key, "BEGIN") {
			_, err := tls.X509KeyPair([]byte(cert), []byte(key))
			need(err == nil)
		} else {
			need(textValue(cert) && textValue(key))
		} // unresolved source-local references, never read here
	}
}
