package servers

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Imported local backends cannot select arbitrary filesystem paths. Runtime
// activation also requires an explicitly prepared, owned test directory.
const xdriveLocalDirectory = "/opt/broray/run/xdrive-local"

func validateXDrive(r Record) *ParseError {
	if e := nativeStreamValid(r); e != nil {
		return e
	}
	if r["protocol"] != "vless" || r["network"] != "xdrive" {
		return nativeError("protocol.network")
	}
	if !nonEmptyString(r["uuid"]) || !textValue(r["uuid"]) {
		return nativeError("settings.vnext.users.id")
	}
	enc, ok := r["encryption"].(string)
	if !ok || enc == "none" || !VlessEncryptionValid(enc) {
		return nativeError("settings.vnext.users.encryption")
	}
	if has(r, "flow") && r["flow"] != "" {
		return nativeError("settings.vnext.users.flow")
	}
	mux, e := nativeObject(r["mux"], "mux", "enabled", "concurrency", "xudpConcurrency", "xudpProxyUDP443")
	if e != nil {
		return e
	}
	if mux["enabled"] != true {
		return nativeError("mux.enabled")
	}
	for _, k := range []string{"concurrency", "xudpConcurrency"} {
		if has(mux, k) && !nativeInt(mux[k], 32767) {
			return nativeError("mux." + k)
		}
	}
	if has(mux, "xudpProxyUDP443") && (!textValue(mux["xudpProxyUDP443"]) || !in(asString(mux["xudpProxyUDP443"]), "", "reject", "allow")) {
		return nativeError("mux.xudpProxyUDP443")
	}
	x, e := nativeObject(r["xdrive"], "xdriveSettings", "service", "remoteFolder", "secrets", "segmentBytes", "flushIntervalMs", "pollIntervalMs", "maxPollIntervalMs", "sessionTtlSeconds", "concurrency", "eagerWindowMs", "holeTimeoutMs", "template", "s3", "xmux")
	if e != nil {
		return e
	}
	for _, k := range []string{"segmentBytes", "flushIntervalMs", "pollIntervalMs", "maxPollIntervalMs", "sessionTtlSeconds", "concurrency", "eagerWindowMs", "holeTimeoutMs"} {
		if has(x, k) && !nativeInt(x[k], 4294967295) {
			return nativeError("xdriveSettings." + k)
		}
	}
	if !textValue(x["remoteFolder"]) || x["remoteFolder"] == "" {
		return nativeError("xdriveSettings.remoteFolder")
	}
	secrets := []any{}
	if has(x, "secrets") {
		var ok bool
		secrets, ok = x["secrets"].([]any)
		if !ok || len(secrets) > 32 {
			return nativeError("xdriveSettings.secrets")
		}
		for _, s := range secrets {
			if !textValue(s) {
				return nativeError("xdriveSettings.secrets")
			}
		}
	}
	if has(x, "xmux") {
		if e := nativeXMux(x["xmux"], "xdriveSettings.xmux"); e != nil {
			return e
		}
	}
	service := asString(x["service"])
	if has(x, "template") && service != "template" {
		return nativeError("xdriveSettings.template")
	}
	if has(x, "s3") && service != "s3" {
		return nativeError("xdriveSettings.s3")
	}
	switch service {
	case "google-drive", "Google Drive":
		if len(secrets) != 3 {
			return nativeError("xdriveSettings.secrets")
		}
	case "s3":
		if e := validateXDriveS3(x, secrets); e != nil {
			return e
		}
	case "template":
		if e := validateXDriveTemplate(x["template"]); e != nil {
			return e
		}
	case "local":
		if x["remoteFolder"] != xdriveLocalDirectory || len(secrets) != 0 {
			return nativeError("xdriveSettings.remoteFolder")
		}
	default:
		return nativeError("xdriveSettings.service")
	}
	// Official core rejects non-noise UDP masks for Drive over H3.
	if jsonValueEqual(get(r, "tls", "alpn"), []any{"h3"}) {
		fm := get(r, "transport", "finalMask")
		if in(service, "google-drive", "Google Drive") {
			types, ok := udpTypes(fm)
			if !ok {
				return nativeError("finalmask.udp")
			}
			for _, typ := range types {
				if typ != "noise" {
					return nativeError("finalmask.udp")
				}
			}
		}
		if in(asString(get(fm, "quicParams", "congestion")), "brutal", "force-brutal") {
			return nativeError("finalmask.quicParams.congestion")
		}
	}
	return nil
}

func relativeS3Name(s string) bool {
	if s == "" || strings.Contains(s, "\\") || !isText(s) {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}

func validateXDriveS3(x map[string]any, secrets []any) *ParseError {
	s, e := nativeObject(x["s3"], "xdriveSettings.s3", "endpoint", "region", "bucket", "pathStyle")
	if e != nil {
		return e
	}
	u, err := url.Parse(asString(s["endpoint"]))
	if err != nil || u == nil || !in(u.Scheme, "http", "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nativeError("xdriveSettings.s3.endpoint")
	}
	if has(s, "region") && (!textValue(s["region"]) || strings.ContainsAny(asString(s["region"]), "/, \t")) {
		return nativeError("xdriveSettings.s3.region")
	}
	pathStyle := true
	if has(s, "pathStyle") {
		var ok bool
		pathStyle, ok = s["pathStyle"].(bool)
		if !ok {
			return nativeError("xdriveSettings.s3.pathStyle")
		}
	}
	bucket := asString(s["bucket"])
	if !relativeS3Name(bucket) || strings.Contains(bucket, "/") {
		return nativeError("xdriveSettings.s3.bucket")
	}
	if !pathStyle && (net.ParseIP(u.Hostname()) != nil || strings.Trim(bucket, "abcdefghijklmnopqrstuvwxyz0123456789-.") != "" || strings.Contains(bucket, "..") || strings.HasPrefix(bucket, ".") || strings.HasSuffix(bucket, ".")) {
		return nativeError("xdriveSettings.s3.pathStyle")
	}
	if !relativeS3Name(strings.TrimSuffix(asString(x["remoteFolder"]), "/")) {
		return nativeError("xdriveSettings.remoteFolder")
	}
	if len(secrets) < 2 || len(secrets) > 3 || secrets[0] == "" || secrets[1] == "" || strings.ContainsAny(asString(secrets[0]), "/ ,\t") {
		return nativeError("xdriveSettings.secrets")
	}
	return nil
}

func stringMapValid(v any, headers bool) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) > 64 {
		return false
	}
	seen := map[string]bool{}
	for k, v := range m {
		if !textValue(v) || !textValue(k) {
			return false
		}
		if headers {
			low := strings.ToLower(k)
			if !headerToken(k) || seen[low] {
				return false
			}
			seen[low] = true
		}
	}
	return true
}

var templateVariable = regexp.MustCompile(`\{[^{}]*\}`)

func templateURL(s string) bool {
	// Validate the HTTP URL shape without resolving or exposing credentials.
	// Variable expansion belongs to Xray and remains intact in the record.
	u, err := url.Parse(templateVariable.ReplaceAllString(s, "placeholder"))
	return err == nil && in(u.Scheme, "http", "https") && u.Hostname() != "" && u.Opaque == ""
}

func validateXDriveTemplate(v any) *ParseError {
	m, e := nativeObject(v, "xdriveSettings.template", "flatten", "concurrency", "auth", "put", "get", "delete", "list", "retry")
	if e != nil {
		return e
	}
	if has(m, "flatten") {
		if _, ok := m["flatten"].(bool); !ok {
			return nativeError("template.flatten")
		}
	}
	if has(m, "concurrency") && !nativeInt(m["concurrency"], 2147483647) {
		return nativeError("template.concurrency")
	}
	for _, op := range []string{"put", "get", "delete", "list"} {
		o, e := nativeObject(m[op], "template."+op, "method", "url", "headers", "body", "namesRegex", "hrefPath", "hrefMethod")
		if e != nil {
			return e
		}
		if !textValue(o["url"]) || !templateURL(asString(o["url"])) {
			return nativeError("template." + op + ".url")
		}
		for _, k := range []string{"method", "namesRegex", "hrefPath", "hrefMethod"} {
			if has(o, k) && !textValue(o[k]) {
				return nativeError("template." + op + "." + k)
			}
		}
		for _, k := range []string{"method", "hrefMethod"} {
			if has(o, k) && asString(o[k]) != "" && !headerToken(asString(o[k])) {
				return nativeError("template." + op + "." + k)
			}
		}
		if has(o, "headers") && !stringMapValid(o["headers"], true) {
			return nativeError("template." + op + ".headers")
		}
		if has(o, "body") {
			b, ok := o["body"].(string)
			if !ok || len(b) > 16384 || strings.ContainsRune(b, 0) {
				return nativeError("template." + op + ".body")
			}
		}
		if op != "put" && asString(o["body"]) != "" {
			return nativeError("template." + op + ".body")
		}
		if op != "list" && asString(o["namesRegex"]) != "" {
			return nativeError("template." + op + ".namesRegex")
		}
		if asString(o["hrefMethod"]) != "" && asString(o["hrefPath"]) == "" {
			return nativeError("template." + op + ".hrefMethod")
		}
		if op == "list" {
			re, err := regexp.Compile(asString(o["namesRegex"]))
			if err != nil || re.NumSubexp() < 1 {
				return nativeError("template.list.namesRegex")
			}
		}
	}
	if has(m, "auth") {
		a, e := nativeObject(m["auth"], "template.auth", "type", "header", "username", "password", "tokenUrl", "form", "tokenPath", "expiryPath")
		if e != nil {
			return e
		}
		if has(a, "type") && (!textValue(a["type"]) || !in(asString(a["type"]), "", "none", "static", "basic", "oauth2")) {
			return nativeError("template.auth.type")
		}
		for _, k := range []string{"username", "password", "tokenUrl", "tokenPath", "expiryPath"} {
			if has(a, k) && !textValue(a[k]) {
				return nativeError("template.auth." + k)
			}
		}
		if a["type"] == "oauth2" && !templateURL(asString(a["tokenUrl"])) {
			return nativeError("template.auth.tokenUrl")
		}
		if has(a, "header") && !stringMapValid(a["header"], true) {
			return nativeError("template.auth.header")
		}
		if has(a, "form") && !stringMapValid(a["form"], false) {
			return nativeError("template.auth.form")
		}
		for _, k := range []string{"header", "username", "password", "tokenUrl", "form", "tokenPath", "expiryPath"} {
			applies := false
			switch k {
			case "header":
				applies = in(asString(a["type"]), "static", "oauth2")
			case "username", "password":
				applies = a["type"] == "basic"
			default:
				applies = a["type"] == "oauth2"
			}
			if !applies && lenPositive(a[k]) {
				return nativeError("template.auth." + k)
			}
		}
	}
	if has(m, "retry") {
		r, e := nativeObject(m["retry"], "template.retry", "status", "rateReason")
		if e != nil {
			return e
		}
		if has(r, "rateReason") && !textValue(r["rateReason"]) {
			return nativeError("template.retry.rateReason")
		}
		if has(r, "status") {
			a, ok := r["status"].([]any)
			if !ok || len(a) > 64 {
				return nativeError("template.retry.status")
			}
			for _, v := range a {
				n, ok := isInt(v)
				if !ok || n < 100 || n > 599 {
					return nativeError("template.retry.status")
				}
			}
		}
	}
	return nil
}

func xdriveSettings(r Record) map[string]any { m, _ := r["xdrive"].(map[string]any); return m }
