package servers

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"github.com/BROadmin/broray/internal/xrayimport"
	"net/netip"
	"strings"
)

var clientFieldNames = []string{"tls", "reality", "httpHeaders", "vmessExperiments", "vlessTestpre", "accountEmail", "accountLevel", "proxyAccount", "password", "mux", "outboundOptions", "streamEndpoint", "hysteriaMasquerade"}

var clientTLSExtraNames = []string{
	"certificates", "enableSessionResumption", "disableSystemRoot", "minVersion", "maxVersion",
	"cipherSuites", "curvePreferences", "echSockopt", "useSystemCA",
}

// ValidateClientFields is the side-effect-free callback used by the shared JSON
// converter. It only accepts named, validated fields of a client endpoint.
func ValidateClientFields(protocol, network, security string, fields map[string]any) error {
	if e := nativeKeys(fields, "clientFields", clientFieldNames...); e != nil {
		return e
	}
	if b, err := json.Marshal(fields); err != nil || len(b) > 262144 {
		return nativeError("clientFields")
	}
	if has(fields, "password") && (!in(protocol, "http", "socks") || fields["proxyAccount"] != true || !textValue(fields["password"])) {
		return nativeError("settings.servers.users.pass")
	}
	if has(fields, "tls") && security != "tls" {
		return nativeError("streamSettings.tlsSettings")
	}
	if has(fields, "reality") {
		if _, e := nativeObject(fields["reality"], "streamSettings.realitySettings", "show"); e != nil {
			return e
		}
		if security != "reality" {
			return nativeError("streamSettings.realitySettings")
		}
	}
	r := normalize(fields)
	if r == nil {
		r = Record{}
	}
	r["protocol"], r["network"], r["security"] = protocol, network, security
	if e := validateClientFields(r); e != nil {
		return e
	}
	return nil
}

// ApplyClientFields attaches detached validated fields after the base endpoint
// conversion. The caller still owns source metadata and the record identifier.
func ApplyClientFields(rec Record, fields map[string]any) (Record, error) {
	if err := ValidateClientFields(asString(rec["protocol"]), asString(rec["network"]), asString(rec["security"]), fields); err != nil {
		return nil, err
	}
	r := normalize(rec)
	for k, v := range normalize(fields) {
		if k == "reality" {
			reality, ok := r[k].(map[string]any)
			if !ok {
				return nil, nativeError("streamSettings.realitySettings")
			}
			for name, value := range v.(map[string]any) {
				reality[name] = value
			}
			continue
		}
		r[k] = v
	}
	if e := validate(r, "JSON client fields"); e != nil {
		return nil, e
	}
	addClientRuntimeWarnings(r)
	return r, nil
}

func validateClientFields(r Record) *ParseError {
	if has(r, "hysteriaMasquerade") {
		if !in(asString(r["protocol"]), "hysteria", "hysteria2") || r["network"] != "hysteria" || xrayimport.ValidateHysteriaMasqueradeMetadata(r["hysteriaMasquerade"]) != nil {
			return nativeError("streamSettings.hysteriaSettings.masquerade")
		}
	}
	bounded := map[string]any{}
	for _, field := range clientFieldNames {
		if has(r, field) {
			bounded[field] = r[field]
		}
	}
	if b, err := json.Marshal(bounded); err != nil || len(b) > 262144 {
		return nativeError("clientFields")
	}
	if r["security"] == "tls" && has(r, "tls") {
		if e := clientTLSValid(r["tls"], "streamSettings.tlsSettings"); e != nil {
			return e
		}
	}
	if has(r["reality"], "show") {
		if _, ok := get(r, "reality", "show").(bool); !ok || r["security"] != "reality" {
			return nativeError("streamSettings.realitySettings.show")
		}
	}
	if has(r, "httpHeaders") {
		headers, ok := r["httpHeaders"].(map[string]any)
		if r["protocol"] != "http" || !ok || headers == nil || !stringMapValid(headers, true) {
			return nativeError("settings.headers")
		}
	}
	if has(r, "accountEmail") && (!in(asString(r["protocol"]), "vless", "vmess", "trojan", "shadowsocks", "http", "socks") || !textValue(r["accountEmail"])) {
		return nativeError("settings.email")
	}
	if has(r, "accountLevel") {
		bits := 32
		if in(asString(r["protocol"]), "trojan", "shadowsocks") {
			bits = 8
		}
		if _, ok := freshUnsigned(r["accountLevel"], bits); !ok || !in(asString(r["protocol"]), "vless", "vmess", "trojan", "shadowsocks", "http", "socks") {
			return nativeError("settings.level")
		}
	}
	if has(r, "proxyAccount") {
		if _, ok := r["proxyAccount"].(bool); !ok || !in(asString(r["protocol"]), "http", "socks") {
			return nativeError("settings.servers.users")
		}
	}
	if has(r, "vmessExperiments") {
		if r["protocol"] != "vmess" || !textValue(r["vmessExperiments"]) {
			return nativeError("settings.experiments")
		}
		// These are the exact account flags consumed by Xray 676eaf6.
		// Unknown tokens would be silently ignored by the core.
		for _, flag := range strings.Split(asString(r["vmessExperiments"]), "|") {
			if !in(flag, "", "AuthenticatedLength", "NoTerminationSignal") {
				return nativeError("settings.experiments")
			}
		}
	}
	if has(r, "vlessTestpre") {
		count, ok := freshUnsigned(r["vlessTestpre"], 32)
		if r["protocol"] != "vless" || !ok || r["network"] == "xdrive" && count != 0 {
			return nativeError("settings.testpre")
		}
	}
	if has(r, "mux") {
		m, e := nativeObject(r["mux"], "mux", "enabled", "concurrency", "xudpConcurrency", "xudpProxyUDP443")
		if e != nil {
			return e
		}
		if !in(asString(r["protocol"]), "vless", "vmess", "trojan", "shadowsocks", "socks", "http") {
			return nativeError("mux")
		}
		if has(m, "enabled") {
			if _, ok := m["enabled"].(bool); !ok {
				return nativeError("mux.enabled")
			}
		}
		for _, k := range []string{"concurrency", "xudpConcurrency"} {
			if has(m, k) {
				if _, ok := freshSigned(m[k], 16); !ok {
					return nativeError("mux." + k)
				}
			}
		}
		if has(m, "xudpProxyUDP443") && (!textValue(m["xudpProxyUDP443"]) || !in(asString(m["xudpProxyUDP443"]), "", "reject", "allow", "skip")) {
			return nativeError("mux.xudpProxyUDP443")
		}
	}
	if has(r, "outboundOptions") {
		m, e := nativeObject(r["outboundOptions"], "outbound", "targetStrategy", "sendThrough")
		if e != nil {
			return e
		}
		if r["protocol"] == "amneziawg" {
			return nativeError("outbound")
		}
		if has(m, "targetStrategy") && (!textValue(m["targetStrategy"]) || !clientDomainStrategy(asString(m["targetStrategy"]))) {
			return nativeError("outbound.targetStrategy")
		}
		if has(m, "sendThrough") && !clientSendThrough(m["sendThrough"]) {
			return nativeError("outbound.sendThrough")
		}
	}
	if has(r, "streamEndpoint") {
		m, e := nativeObject(r["streamEndpoint"], "streamSettings", "address", "port")
		if e != nil {
			return e
		}
		if in(asString(r["protocol"]), "wireguard", "amneziawg") {
			return nativeError("streamSettings")
		}
		if has(m, "address") && !serverAddressValid(m["address"]) {
			return nativeError("streamSettings.address")
		}
		if has(m, "port") {
			if _, ok := freshUnsigned(m["port"], 16); !ok {
				return nativeError("streamSettings.port")
			}
		}
	}
	return nil
}

func clientDomainStrategy(s string) bool {
	return in(strings.ToLower(s), "", "asis", "useip", "useipv4", "useipv6", "useipv4v6", "useipv6v4",
		"forceip", "forceipv4", "forceipv6", "forceipv4v6", "forceipv6v4")
}

func clientSendThrough(v any) bool {
	s, ok := v.(string)
	if !ok || !textValue(v) || s == "" {
		return false
	}
	if s == "origin" || s == "srcip" {
		return true
	}
	if ip, err := netip.ParseAddr(s); err == nil {
		return ip.Zone() == ""
	}
	prefix, err := netip.ParsePrefix(s)
	return err == nil && prefix.Addr().Zone() == ""
}

func clientStringList(v any) ([]string, bool) {
	if s, ok := v.(string); ok {
		return strings.Split(s, ","), true
	}
	list, ok := v.([]any)
	if !ok || list == nil {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func clientTLSValid(v any, path string) *ParseError {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nativeError(path)
	}
	if b, err := json.Marshal(m); err != nil || len(b) > 262144 {
		return nativeError(path)
	}
	// Reuse the pinned field schema; nested sockopt must follow the same
	// contract as ordinary stream sockopt, including future validated options.
	schema := make(map[string]any, len(m))
	for k, value := range m {
		if k != "echSockopt" {
			schema[k] = value
		}
	}
	if schema["masterKeyLog"] == "none" {
		schema["masterKeyLog"] = ""
	}
	if e := freshTLS(schema, path, 0); e != nil {
		return e
	}
	for _, k := range []string{"serverName", "minVersion", "maxVersion", "cipherSuites", "fingerprint", "pinnedPeerCertSha256", "verifyPeerCertByName", "echConfigList"} {
		if has(m, k) && !textValue(m[k]) {
			return nativeError(path + "." + k)
		}
	}
	versions := map[string]int{"": 0, "1.0": 10, "1.1": 11, "1.2": 12, "1.3": 13}
	min, minOK := versions[asString(m["minVersion"])]
	max, maxOK := versions[asString(m["maxVersion"])]
	if !minOK || !maxOK || min > 0 && max > 0 && min > max {
		return nativeError(path + ".minVersion.maxVersion")
	}
	if suites := asString(m["cipherSuites"]); suites != "" {
		known := map[string]bool{}
		for _, suite := range append(tls.CipherSuites(), tls.InsecureCipherSuites()...) {
			known[suite.Name] = true
		}
		for _, name := range strings.Split(suites, ":") {
			if !known[strings.TrimSpace(name)] {
				return nativeError(path + ".cipherSuites")
			}
		}
	}
	for _, field := range []string{"alpn", "curvePreferences"} {
		if !has(m, field) {
			continue
		}
		list, valid := clientStringList(m[field])
		if !valid || len(list) > 64 {
			return nativeError(path + "." + field)
		}
		for _, item := range list {
			if item == "" || len(item) > 255 || !isText(item) {
				return nativeError(path + "." + field)
			}
			if field == "curvePreferences" && !in(strings.ToLower(item), "curvep256", "curvep384", "curvep521", "x25519", "x25519mlkem768", "secp256r1mlkem768", "secp384r1mlkem1024") {
				return nativeError(path + "." + field)
			}
			if field == "alpn" && strings.EqualFold(item, "frommitm") && len(list) != 1 {
				return nativeError(path + ".alpn")
			}
		}
	}
	if has(m, "echSockopt") {
		opts, ok := m["echSockopt"].(map[string]any)
		if !ok || opts == nil {
			return nativeError(path + ".echSockopt")
		}
		if err := ValidateStreamOptions("raw", map[string]any{"sockopt": opts}); err != nil {
			return nativeError(path + ".echSockopt")
		}
	}
	if has(m, "certificates") {
		certs, ok := m["certificates"].([]any)
		if !ok || certs == nil || len(certs) > 32 {
			return nativeError(path + ".certificates")
		}
		for _, cert := range certs {
			if e := clientTLSCertificate(cert, path+".certificates"); e != nil {
				return e
			}
		}
	}
	return nil
}

func clientTLSCertificate(v any, path string) *ParseError {
	// freshTLSCert above rejects nonempty file paths and validates every type.
	m := v.(map[string]any)
	usage := strings.ToLower(asString(m["usage"]))
	if !in(usage, "", "encipherment", "verify") {
		return nativeError(path + ".usage")
	}
	pemBytes := func(field string) []byte {
		var lines []string
		for _, line := range m[field].([]any) {
			lines = append(lines, line.(string))
		}
		return []byte(strings.Join(lines, "\n"))
	}
	if !has(m, "certificate") {
		return nativeError(path + ".certificate")
	}
	certPEM := pemBytes("certificate")
	remaining := certPEM
	count := 0
	for len(strings.TrimSpace(string(remaining))) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" {
			return nativeError(path + ".certificate")
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return nativeError(path + ".certificate")
		}
		count++
		remaining = rest
	}
	if count == 0 {
		return nativeError(path + ".certificate")
	}
	if has(m, "key") && len(m["key"].([]any)) > 0 {
		if _, err := tls.X509KeyPair(certPEM, pemBytes("key")); err != nil {
			return nativeError(path + ".key")
		}
	} else if usage != "verify" {
		return nativeError(path + ".key")
	}
	return nil
}

func mergeClientStreamEndpoint(st map[string]any, r Record) {
	if endpoint, ok := r["streamEndpoint"].(map[string]any); ok {
		for k, v := range endpoint {
			st[k] = v
		}
	}
}

func addClientFieldIdentity(key map[string]any, r Record) {
	if has(r["reality"], "show") {
		key["realityShow"] = get(r, "reality", "show")
	}
	for _, field := range []string{"httpHeaders", "vmessExperiments", "vlessTestpre", "accountEmail", "accountLevel", "proxyAccount", "mux", "outboundOptions", "streamEndpoint", "hysteriaMasquerade"} {
		if has(r, field) {
			key[field] = r[field]
		}
	}
	extraTLS := map[string]any{}
	for _, field := range clientTLSExtraNames {
		if has(r["tls"], field) {
			extraTLS[field] = get(r, "tls", field)
		}
	}
	if len(extraTLS) > 0 {
		key["clientTLS"] = extraTLS
	}
}
