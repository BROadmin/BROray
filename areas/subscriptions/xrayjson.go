package subscriptions

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
)

var vlessEncryptionValid = servers.VlessEncryptionValid

// Xray JSON profile → BROray links (lib/subscription-xray-json.jq).
// Only endpoint data is extracted; a foreign profile is never executed.
// Every jq `require`/`error` is a panic(jqFail{}) caught per outbound
// (→ the unsupported-node marker) or per document (→ PARSE_ERROR).

const unsupportedMarker = "broray-json-error://unsupported-node"

type jqFail struct{ field string }

func need(ok bool) {
	if !ok {
		panic(jqFail{})
	}
}

func obj(v any) map[string]any {
	m, ok := v.(map[string]any)
	need(ok && m != nil)
	return m
}

func has(m map[string]any, k string) bool { _, ok := m[k]; return ok }

// keysOnly is `type=="object" and ((keys - $keys)|length)==0`.
func keysOnly(v any, keys ...string) bool {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return false
	}
	for k := range m {
		found := false
		for _, a := range keys {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			panic(jqFail{field: safeJSONField(k)})
		}
	}
	return true
}

// textValue is `type=="string" and length<=4096 and all(explode[]; .>=32 and .!=127)`.
func textValue(v any) bool {
	s, ok := v.(string)
	if !ok || runeLen(s) > 4096 {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func unreservedStr(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) == 0 || runeLen(s) > 255 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-' || r == '.' || r == '_' || r == '~') {
			return false
		}
	}
	return true
}

func stringField(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok {
		need(textValue(v))
		return v.(string)
	}
	return def
}

func objectField(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		return obj(v)
	}
	return map[string]any{}
}

// uriEscape is jq @uri (unreserved A-Za-z0-9-_.~ kept, %XX uppercase).
func uriEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)|0x100, 16)[1:]))
		}
	}
	return b.String()
}

func pair(k, v string) string { return k + "=" + uriEscape(v) }

// toJSON is jq tojson (compact; object keys sorted, see report).
func toJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(buf.String(), "\n")
}

func numText(v any) string {
	f, _ := num(v)
	if f == math.Trunc(f) && math.Abs(f) < 1e17 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', 17, 64)
}

func isDecimal(s string) bool { return allDigits(s) }

func ipv4Literal(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if !isDecimal(p) || len(p) > 3 || len(p) > 1 && p[0] == '0' {
			return false
		}
		if n, _ := strconv.Atoi(p); n > 255 {
			return false
		}
	}
	return true
}

func hexGroup(s string) bool {
	if len(s) == 0 || len(s) > 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func ipv6Literal(s string) bool {
	n := runeLen(s)
	if n < 2 || n > 45 || !strings.Contains(s, ":") {
		return false
	}
	if strings.Contains(s, ".") {
		parts := strings.Split(s, ":")
		tail := parts[len(parts)-1]
		if !ipv4Literal(tail) {
			return false
		}
		s = s[:len(s)-len(tail)] + "0:0"
	}
	halves := strings.Split(s, "::")
	groups := func(t string) ([]string, bool) {
		if t == "" {
			return nil, true
		}
		g := strings.Split(t, ":")
		for _, x := range g {
			if !hexGroup(x) {
				return nil, false
			}
		}
		return g, true
	}
	switch len(halves) {
	case 1:
		g := strings.Split(halves[0], ":")
		if len(g) != 8 {
			return false
		}
		for _, x := range g {
			if !hexGroup(x) {
				return false
			}
		}
		return true
	case 2:
		l, ok1 := groups(halves[0])
		r, ok2 := groups(halves[1])
		return ok1 && ok2 && len(l)+len(r) < 8
	}
	return false
}

func addressURI(v any) string {
	s, ok := v.(string)
	need(ok && len(s) > 0 && runeLen(s) <= 255)
	switch {
	case strings.HasPrefix(s, "[") || strings.HasSuffix(s, "]"):
		need(strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") && len(s) >= 2)
		inner := s[1 : len(s)-1]
		need(ipv6Literal(inner))
		return "[" + inner + "]"
	case strings.Contains(s, ":"):
		need(ipv6Literal(s))
		return "[" + s + "]"
	}
	need(unreservedStr(s))
	return s
}

func network(s map[string]any) string {
	switch n := stringField(s, "network", "tcp"); n {
	case "raw", "tcp":
		return "tcp"
	case "websocket", "ws":
		return "ws"
	case "httpUpgrade", "httpupgrade":
		return "httpupgrade"
	case "splithttp", "xhttp":
		return "xhttp"
	case "grpc", "kcp":
		return n
	}
	panic(jqFail{})
}

func onlyTransport(s map[string]any, allowed ...string) {
	need(keysOnly(s, append([]string{"network", "security", "tlsSettings", "realitySettings", "finalmask"}, allowed...)...))
}

func aliasedObject(s map[string]any, primary, legacy string) map[string]any {
	need(!(has(s, primary) && has(s, legacy)))
	if has(s, primary) {
		return objectField(s, primary)
	}
	return objectField(s, legacy)
}

func websocketHost(w map[string]any) string {
	headers := objectField(w, "headers")
	var values []string
	for k, v := range headers {
		need(strings.ToLower(k) == "host")
		need(textValue(v))
		values = append(values, v.(string))
	}
	values = uniqueSorted(values)
	need(len(values) <= 1)
	host := stringField(w, "host", "")
	need(host == "" || len(values) == 0 || host == values[0])
	if host != "" {
		return host
	}
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func inList(v string, list ...string) bool {
	for _, x := range list {
		if v == x {
			return true
		}
	}
	return false
}

func protection(s map[string]any) []string {
	security := stringField(s, "security", "none")
	need(inList(security, "none", "tls", "reality"))
	switch security {
	case "reality":
		need(!has(s, "tlsSettings"))
		need(inList(network(s), "tcp", "grpc", "xhttp"))
		rv := s["realitySettings"]
		need(keysOnly(rv, "serverName", "fingerprint", "publicKey", "password", "shortId", "spiderX", "show", "mldsa65Verify"))
		r := rv.(map[string]any)
		if v, ok := r["show"]; ok {
			_, isBool := v.(bool)
			need(isBool)
		}
		sn, _ := r["serverName"].(string)
		need(textValue(r["serverName"]) && runeLen(sn) > 0)
		if has(r, "publicKey") && has(r, "password") {
			need(jsonEqual(r["publicKey"], r["password"]))
		}
		key := r["publicKey"]
		if has(r, "password") {
			key = r["password"]
		}
		ks, _ := key.(string)
		need(textValue(key) && runeLen(ks) > 0)
		return []string{pair("security", "reality"), pair("sni", sn), pair("pbk", ks),
			pair("fp", stringField(r, "fingerprint", "chrome")), pair("sid", stringField(r, "shortId", "")),
			pair("spx", stringField(r, "spiderX", "")), pair("pqv", stringField(r, "mldsa65Verify", ""))}
	case "tls":
		need(!has(s, "realitySettings"))
		t := objectField(s, "tlsSettings")
		need(keysOnly(t, "serverName", "fingerprint", "alpn", "allowInsecure", "echConfigList", "pinnedPeerCertSha256", "verifyPeerCertByName"))
		if v, ok := t["allowInsecure"]; ok {
			need(v == false)
		}
		var alpn []string
		if v, ok := t["alpn"]; ok {
			arr, isArr := v.([]any)
			need(isArr)
			for _, a := range arr {
				as, _ := a.(string)
				need(textValue(a) && runeLen(as) > 0 && runeLen(as) <= 255 && !strings.Contains(as, ","))
			}
		}
		// ($t.alpn // [] | join(","))
		if arr, ok := t["alpn"].([]any); ok {
			for _, a := range arr {
				alpn = append(alpn, a.(string))
			}
		}
		return []string{pair("security", "tls"), pair("sni", stringField(t, "serverName", "")),
			pair("fp", stringField(t, "fingerprint", "chrome")), pair("alpn", strings.Join(alpn, ",")),
			pair("ech", stringField(t, "echConfigList", "")), pair("pcs", stringField(t, "pinnedPeerCertSha256", "")),
			pair("vcn", stringField(t, "verifyPeerCertByName", ""))}
	}
	need(!has(s, "tlsSettings") && !has(s, "realitySettings"))
	return []string{pair("security", "none")}
}

// jsonEqual compares decoded JSON values (numbers by value).
func jsonEqual(a, b any) bool {
	if fa, ok := num(a); ok {
		fb, ok := num(b)
		return ok && fa == fb
	}
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !jsonEqual(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// rangePair is range_pair.
func rangePair(v any) [2]float64 {
	if _, isNum := num(v); isNum {
		need(isInt(v, 0, 2147483647))
		f, _ := num(v)
		return [2]float64{f, f}
	}
	s, ok := v.(string)
	need(ok)
	var parts []string
	if s != "" {
		parts = strings.Split(s, "-")
	}
	need(len(parts) == 1 || len(parts) == 2)
	var vals []float64
	for _, p := range parts {
		need(isDecimal(p) && len(p) <= 10)
		f, _ := strconv.ParseFloat(p, 64)
		need(f <= 2147483647)
		vals = append(vals, f)
	}
	if len(vals) == 1 {
		return [2]float64{vals[0], vals[0]}
	}
	need(vals[0] <= vals[1])
	return [2]float64{vals[0], vals[1]}
}

var xhttpExtraKeys = []string{"headers", "xPaddingBytes", "noGRPCHeader", "noSSEHeader", "scMaxEachPostBytes",
	"scMinPostsIntervalMs", "scMaxBufferedPosts", "scStreamUpServerSecs", "xmux",
	"xPaddingObfsMode", "xPaddingKey", "xPaddingHeader", "xPaddingPlacement", "xPaddingMethod",
	"uplinkHTTPMethod", "sessionIDPlacement", "sessionIDKey", "sessionIDTable", "sessionIDLength",
	"seqPlacement", "seqKey", "uplinkDataPlacement", "uplinkDataKey", "uplinkChunkSize",
	"serverMaxHeaderBytes", "downloadSettings"}

func withKeys(extra ...string) []string {
	return append(append([]string{}, xhttpExtraKeys...), extra...)
}

func modeOrAuto(m string) string {
	if m == "" {
		return "auto"
	}
	return m
}

// strOr is `($e[$k] // $def)` for already validated text fields.
func strOr(m map[string]any, k, def string) string {
	if s, ok := m[k].(string); ok {
		return s
	}
	return def
}

func xhttpExtra(e map[string]any, mode string, depth int) map[string]any {
	need(keysOnly(e, withKeys("mode")...))
	if v, ok := e["mode"]; ok {
		ms, isStr := v.(string)
		need(isStr && modeOrAuto(ms) == modeOrAuto(mode))
	}
	need(runeLen(toJSON(e)) <= 4096)
	headers := objectField(e, "headers")
	for k, v := range headers {
		need(textValue(k) && len(k) > 0 && strings.ToLower(k) != "host" && textValue(v))
	}
	for _, k := range []string{"noGRPCHeader", "noSSEHeader", "xPaddingObfsMode"} {
		if v, ok := e[k]; ok {
			_, isBool := v.(bool)
			need(isBool)
		}
	}
	for _, k := range []string{"xPaddingBytes", "scMaxEachPostBytes", "scMinPostsIntervalMs", "scStreamUpServerSecs", "sessionIDLength", "uplinkChunkSize"} {
		if v, ok := e[k]; ok {
			rangePair(v)
		}
	}
	if v, ok := e["scMaxBufferedPosts"]; ok {
		need(isInt(v, 0, 2147483647))
	}
	m := objectField(e, "xmux")
	need(keysOnly(m, "maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs", "hKeepAlivePeriod"))
	for _, k := range []string{"maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs"} {
		if v, ok := m[k]; ok {
			rangePair(v)
		}
	}
	if v, ok := m["hKeepAlivePeriod"]; ok {
		need(isInt(v, 0, 2147483647))
	}
	orZero := func(v any) any {
		if !truthy(v) {
			return json.Number("0")
		}
		return v
	}
	need(!(rangePair(orZero(m["maxConcurrency"]))[1] > 0 && rangePair(orZero(m["maxConnections"]))[1] > 0))
	if v, ok := e["xPaddingBytes"]; ok {
		r := rangePair(v)
		need(r[0] > 0 || r == [2]float64{0, 0})
	}
	for _, k := range []string{"xPaddingKey", "xPaddingHeader", "xPaddingPlacement", "xPaddingMethod", "uplinkHTTPMethod",
		"sessionIDPlacement", "sessionIDKey", "sessionIDTable", "seqPlacement", "seqKey", "uplinkDataPlacement", "uplinkDataKey"} {
		if v, ok := e[k]; ok {
			need(textValue(v))
		}
	}
	need(inList(strOr(e, "xPaddingPlacement", ""), "", "cookie", "header", "query", "queryInHeader"))
	need(inList(strOr(e, "xPaddingMethod", ""), "", "repeat-x", "tokenish"))
	for _, k := range []string{"sessionIDPlacement", "seqPlacement"} {
		need(inList(strOr(e, k, ""), "", "path", "cookie", "header", "query"))
	}
	need(inList(strOr(e, "uplinkDataPlacement", ""), "", "auto", "body", "cookie", "header"))
	need(mode == "packet-up" || (!inList(strOr(e, "uplinkDataPlacement", ""), "cookie", "header") &&
		strings.ToUpper(strOr(e, "uplinkHTTPMethod", "POST")) != "GET"))
	if v, ok := e["serverMaxHeaderBytes"]; ok {
		need(isInt(v, 0, 2147483647))
	}
	if table := strOr(e, "sessionIDTable", ""); table != "" {
		sizes := map[string]float64{"ALPHABET": 26, "Alphabet": 52, "BASE36": 36, "Base62": 62, "HEX": 16, "alphabet": 26, "base36": 36, "hex": 16, "number": 10}
		size, known := sizes[table]
		if !known {
			size = float64(runeLen(table))
		}
		var lenv any = json.Number("0")
		if truthy(e["sessionIDLength"]) {
			lenv = e["sessionIDLength"]
		}
		r := rangePair(lenv)
		ascii := true
		for _, c := range table {
			if c >= 128 {
				ascii = false
			}
		}
		enough := r[1] >= 31
		if !enough {
			total := 0.0
			for n := r[0]; n <= r[1]; n++ {
				total += math.Pow(size, n)
			}
			enough = total >= 2147483648
		}
		need(size >= 2 && r[0] > 0 && ascii && enough)
	}
	if has(e, "downloadSettings") {
		need(depth == 0 && mode != "stream-one")
		d := objectField(e, "downloadSettings")
		need(keysOnly(d, "address", "port", "network", "security", "tlsSettings", "realitySettings", "xhttpSettings", "splithttpSettings", "finalmask"))
		need(network(d) == "xhttp")
		if v, ok := d["address"]; ok {
			need(len(addressURI(v)) > 0)
		}
		if v, ok := d["port"]; ok {
			need(isInt(v, 1, 65535))
		}
		protection(d)
		objectField(d, "finalmask")
		x := aliasedObject(d, "xhttpSettings", "splithttpSettings")
		need(keysOnly(x, withKeys("host", "path", "mode", "extra")...))
		stringField(x, "host", "")
		stringField(x, "path", "/")
		m2 := stringField(x, "mode", "auto")
		need(inList(m2, "", "auto", "packet-up", "stream-up", "stream-one"))
		var inner map[string]any
		if has(x, "extra") {
			need(noneOf(x, xhttpExtraKeys))
			inner = objectField(x, "extra")
		} else {
			inner = without(x, "host", "path", "mode")
		}
		xhttpExtra(inner, m2, 1)
	}
	return e
}

// noneOf is `(keys - $list | length) == (keys | length)`.
func noneOf(m map[string]any, list []string) bool {
	for _, k := range list {
		if has(m, k) {
			return false
		}
	}
	return true
}

func without(m map[string]any, keys ...string) map[string]any {
	out := clone(m)
	for _, k := range keys {
		delete(out, k)
	}
	return out
}

func xhttpOptions(x map[string]any) map[string]any {
	mode := stringField(x, "mode", "auto")
	need(keysOnly(x, withKeys("host", "path", "mode", "extra")...))
	if has(x, "extra") {
		need(noneOf(x, xhttpExtraKeys))
		return xhttpExtra(objectField(x, "extra"), mode, 0)
	}
	return xhttpExtra(without(x, "host", "path", "mode"), mode, 0)
}

func stringListOK(v any) bool {
	if _, ok := v.(string); ok {
		return textValue(v)
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range arr {
		if !textValue(e) {
			return false
		}
	}
	return true
}

func rawHeader(h map[string]any) map[string]any {
	need(keysOnly(h, "type", "request", "response"))
	if len(h) == 0 || (len(h) == 1 && h["type"] == "none") {
		return h
	}
	need(h["type"] == "http")
	q := objectField(h, "request")
	r := objectField(h, "response")
	need(keysOnly(q, "version", "method", "path", "headers"))
	need(keysOnly(r, "version", "status", "reason", "headers"))
	for _, o := range []map[string]any{q, r} {
		for k, v := range o {
			if k != "path" && k != "headers" {
				need(textValue(v))
			}
		}
	}
	if v, ok := q["path"]; ok {
		need(stringListOK(v))
	}
	for _, o := range []map[string]any{q, r} {
		for k, v := range objectField(o, "headers") {
			need(textValue(k) && len(k) > 0 && stringListOK(v))
		}
	}
	return h
}

func basicTransport(sv any) []string {
	s := obj(sv)
	switch network(s) {
	case "grpc":
		onlyTransport(s, "grpcSettings")
		g := objectField(s, "grpcSettings")
		need(keysOnly(g, "serviceName", "authority", "multiMode", "mode"))
		if v, ok := g["mode"]; ok {
			need(v == false)
		}
		if v, ok := g["multiMode"]; ok {
			_, isBool := v.(bool)
			need(isBool)
		}
		mode := "gun"
		if g["multiMode"] == true {
			mode = "multi"
		}
		return []string{pair("type", "grpc"), pair("serviceName", stringField(g, "serviceName", "")),
			pair("host", stringField(g, "authority", "")), pair("mode", mode)}
	case "kcp":
		onlyTransport(s, "kcpSettings")
		k := objectField(s, "kcpSettings")
		need(keysOnly(k, "mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow", "header", "seed"))
		out := []string{pair("type", "kcp")}
		for _, name := range []string{"mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow"} {
			if v, ok := k[name]; ok {
				need(isInt(v, 0, 4294967295))
				out = append(out, pair(name, numText(v)))
			}
		}
		if has(k, "seed") {
			out = append(out, pair("seed", stringField(k, "seed", "")))
		}
		if v, ok := k["header"]; ok {
			need(keysOnly(v, "type"))
			out = append(out, pair("headerType", stringField(v.(map[string]any), "type", "none")))
		}
		return out
	case "tcp":
		onlyTransport(s, "tcpSettings", "rawSettings")
		t := aliasedObject(s, "rawSettings", "tcpSettings")
		need(keysOnly(t, "header"))
		h := rawHeader(objectField(t, "header"))
		out := []string{pair("type", "tcp")}
		if len(h) > 0 && !(len(h) == 1 && h["type"] == "none") {
			out = append(out, pair("header", toJSON(h)))
		}
		return out
	case "ws":
		onlyTransport(s, "wsSettings")
		w := objectField(s, "wsSettings")
		need(keysOnly(w, "path", "host", "headers"))
		return []string{pair("type", "ws"), pair("path", stringField(w, "path", "/")), pair("host", websocketHost(w))}
	case "httpupgrade":
		onlyTransport(s, "httpupgradeSettings")
		u := objectField(s, "httpupgradeSettings")
		need(keysOnly(u, "path", "host"))
		return []string{pair("type", "httpupgrade"), pair("path", stringField(u, "path", "/")), pair("host", stringField(u, "host", ""))}
	}
	onlyTransport(s, "xhttpSettings", "splithttpSettings")
	x := aliasedObject(s, "xhttpSettings", "splithttpSettings")
	extra := xhttpOptions(x)
	mode := stringField(x, "mode", "auto")
	need(inList(mode, "", "auto", "packet-up", "stream-up", "stream-one"))
	return []string{pair("type", "xhttp"), pair("path", stringField(x, "path", "/")), pair("host", stringField(x, "host", "")),
		pair("mode", mode), pair("extra", toJSON(extra))}
}

type endpoint struct {
	address any
	port    any
	users   []any
}

func flatEndpoint(m map[string]any) endpoint {
	user := without(m, "address", "port")
	return endpoint{address: m["address"], port: m["port"], users: []any{user}}
}

func listOf(v any) []any {
	arr, ok := v.([]any)
	need(ok && len(arr) > 0)
	return arr
}

func endpointOf(v any) endpoint {
	need(keysOnly(v, "address", "port", "users"))
	m := v.(map[string]any)
	return endpoint{address: m["address"], port: m["port"], users: listOf(m["users"])}
}

func outboundName(profile, out map[string]any, index int, multiple bool) string {
	// string_field($profile;"remarks";string_field($out;"tag";"Сервер")): both evaluated.
	tagLabel := stringField(out, "tag", "Сервер")
	label := stringField(profile, "remarks", tagLabel)
	if multiple {
		label += " / " + stringField(out, "tag", strconv.Itoa(index))
	}
	return label
}

func vlessOutbound(profile, out map[string]any, index int, multiple bool) []string {
	need(keysOnly(out, "protocol", "tag", "settings", "streamSettings", "fragment") && out["protocol"] == "vless")
	settings := obj(out["settings"])
	stream := objectField(out, "streamSettings")
	tr := transport(stream)
	prot := protection(stream)
	name := outboundName(profile, out, index, multiple)
	var eps []endpoint
	if has(settings, "vnext") {
		need(keysOnly(settings, "vnext"))
		for _, e := range listOf(settings["vnext"]) {
			eps = append(eps, endpointOf(e))
		}
	} else {
		need(keysOnly(settings, "address", "port", "id", "encryption", "flow"))
		eps = append(eps, flatEndpoint(settings))
	}
	var lines []string
	for _, ep := range eps {
		address := addressURI(ep.address)
		need(isInt(ep.port, 1, 65535))
		for _, uv := range ep.users {
			need(keysOnly(uv, "id", "encryption", "flow"))
			u := uv.(map[string]any)
			need(unreservedStr(u["id"]))
			enc := stringField(u, "encryption", "none")
			need(vlessEncryptionValid(enc))
			flow := stringField(u, "flow", "")
			secVal := stream["security"]
			need(flow == "" || ((flow == "xtls-rprx-vision" || flow == "xtls-rprx-vision-udp443") &&
				(network(stream) == "tcp" || enc != "none") && (secVal == "reality" || secVal == "tls")))
			params := append(append(append([]string{}, tr...), prot...), pair("encryption", enc), pair("flow", flow),
				pair("fm", toJSON(objectField(stream, "finalmask"))))
			lines = append(lines, "vless://"+u["id"].(string)+"@"+address+":"+numText(ep.port)+"?"+strings.Join(params, "&")+"#"+uriEscape(name))
		}
	}
	return lines
}

func endpointMetadata(u map[string]any) {
	if v, ok := u["level"]; ok {
		f, isNum := num(v)
		need(isNum && f == 0)
	}
	if v, ok := u["email"]; ok {
		need(textValue(v))
	}
}

type credEndpoint struct {
	address, port any
	credential    string
	params        []string
}

func otherEndpoints(protocol string, settings map[string]any) []credEndpoint {
	var out []credEndpoint
	if protocol == "vmess" {
		var eps []endpoint
		if has(settings, "vnext") {
			need(keysOnly(settings, "vnext"))
			for _, e := range listOf(settings["vnext"]) {
				eps = append(eps, endpointOf(e))
			}
		} else {
			need(keysOnly(settings, "address", "port", "id", "security", "alterId", "experiments", "level", "email"))
			eps = append(eps, flatEndpoint(settings))
		}
		for _, ep := range eps {
			need(len(ep.users) > 0)
			for _, uv := range ep.users {
				need(keysOnly(uv, "id", "security", "alterId", "experiments", "level", "email"))
				u := uv.(map[string]any)
				endpointMetadata(u)
				need(unreservedStr(u["id"]))
				if v, ok := u["alterId"]; ok {
					f, isNum := num(v)
					need(isNum && f == 0)
				}
				if v, ok := u["experiments"]; ok {
					need(v == "")
				}
				cipher := stringField(u, "security", "auto")
				need(inList(cipher, "auto", "aes-128-gcm", "chacha20-poly1305", "none"))
				out = append(out, credEndpoint{ep.address, ep.port, u["id"].(string), []string{pair("encryption", cipher)}})
			}
		}
		return out
	}
	var servers []map[string]any
	if has(settings, "servers") {
		need(keysOnly(settings, "servers"))
		for _, sv := range listOf(settings["servers"]) {
			servers = append(servers, obj(sv))
		}
	} else {
		servers = append(servers, settings)
	}
	for _, sv := range servers {
		keys := []string{"address", "port", "password", "level", "email"}
		if protocol == "trojan" {
			keys = append(keys, "flow")
		} else {
			keys = append(keys, "method")
		}
		need(keysOnly(sv, keys...))
		endpointMetadata(sv)
		pw, _ := sv["password"].(string)
		need(textValue(sv["password"]) && runeLen(pw) > 0)
		if protocol == "trojan" {
			if v, ok := sv["flow"]; ok {
				need(v == "")
			}
			out = append(out, credEndpoint{sv["address"], sv["port"], pw, nil})
			continue
		}
		method, _ := sv["method"].(string)
		need(textValue(sv["method"]) && runeLen(method) > 0)
		// 3.x emitted padded standard Base64 (@base64|@uri → "%3D", "%2B"),
		// which its own Shadowsocks parser rejects; URL-safe unpadded Base64
		// is what the SIP002 parser accepts (deliberate fix, see report).
		out = append(out, credEndpoint{sv["address"], sv["port"], base64.RawURLEncoding.EncodeToString([]byte(method + ":" + pw)), nil})
	}
	return out
}

func outboundURIs(profile map[string]any, ov any, index int, multiple bool) []string {
	out := obj(ov)
	if out["protocol"] == "wireguard" {
		return wireGuardOutbound(profile, out, index, multiple)
	}
	if out["protocol"] == "hysteria" {
		return hysteriaOutbound(profile, out, index, multiple)
	}
	if out["protocol"] == "socks" || out["protocol"] == "http" {
		return proxyOutbound(profile, out, index, multiple)
	}
	if out["protocol"] == "vless" {
		return vlessOutbound(profile, out, index, multiple)
	}
	need(keysOnly(out, "protocol", "tag", "settings", "streamSettings"))
	protocol, _ := out["protocol"].(string)
	need(inList(protocol, "vmess", "trojan", "shadowsocks"))
	settings := obj(out["settings"])
	stream := objectField(out, "streamSettings")
	name := outboundName(profile, out, index, multiple)
	var params []string
	if protocol == "shadowsocks" {
		need(network(stream) == "tcp" && truthyOr(stream["security"], "none") == "none")
		t := transport(stream)
		protection(stream)
		base := basicTransport(without(stream, "sockopt"))
		need(len(base) == 1 && base[0] == pair("type", "tcp") && len(objectField(stream, "finalmask")) == 0)
		params = append(params, t[1:]...)
	} else {
		params = append(append(transport(stream), protection(stream)...), pair("fm", toJSON(objectField(stream, "finalmask"))))
	}
	scheme := protocol
	if protocol == "shadowsocks" {
		scheme = "ss"
	}
	var lines []string
	for _, e := range otherEndpoints(protocol, settings) {
		address := addressURI(e.address)
		need(isInt(e.port, 1, 65535))
		all := append(append([]string{}, params...), e.params...)
		line := scheme + "://" + uriEscape(e.credential) + "@" + address + ":" + numText(e.port)
		if len(all) > 0 {
			line += "?" + strings.Join(all, "&")
		}
		lines = append(lines, line+"#"+uriEscape(name))
	}
	return lines
}

// truthyOr is jq `v // def` returning v when truthy.
func truthyOr(v any, def any) any {
	if truthy(v) {
		return v
	}
	return def
}

func tryLines(fn func() []string) (lines []string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			lines, ok = nil, false
			if e, yes := r.(jqFail); yes && e.field != "" {
				lines = []string{unsupportedMarker + "#" + e.field}
			}
		}
	}()
	return fn(), true
}

// xrayJSONToLinks converts an Xray JSON document (object or array of
// profiles) into share links; unsupported outbounds become the marker line.
// ok=false is a document-level failure (PARSE_ERROR).
func xrayJSONToLinks(text []byte, maxNodes int) (out []string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			out, ok = nil, false
		}
	}()
	docs, valid := decodeAll(text)
	if !valid || len(docs) != 1 {
		return nil, false
	}
	var profiles []any
	switch x := docs[0].(type) {
	case map[string]any:
		profiles = []any{x}
	case []any:
		profiles = x
	default:
		return nil, false
	}
	for _, p := range profiles {
		pm, isObj := p.(map[string]any)
		if !isObj || pm == nil {
			return nil, false
		}
		obs, isArr := pm["outbounds"].([]any)
		if !isArr {
			return nil, false
		}
		for _, o := range obs {
			if om, isObj := o.(map[string]any); !isObj || om == nil {
				return nil, false
			}
		}
	}
	limit := maxNodes + 1
	for _, p := range profiles {
		profile := p.(map[string]any)
		var outbounds []any
		for _, o := range profile["outbounds"].([]any) {
			proto := o.(map[string]any)["protocol"]
			if proto != "freedom" && proto != "blackhole" {
				outbounds = append(outbounds, o)
			}
		}
		for i, o := range outbounds {
			lines, good := tryLines(func() []string { return outboundURIs(profile, o, i+1, len(outbounds) > 1) })
			if !good && len(lines) == 0 {
				lines = []string{unsupportedMarker}
			} else if len(lines) > limit {
				lines = lines[:limit]
			}
			for _, l := range lines {
				out = append(out, l)
				if len(out) >= limit {
					return out, true
				}
			}
		}
	}
	return out, true
}
