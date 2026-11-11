package servers

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/strictjson"
)

// ---------- jq-like accessors over decoded JSON ----------

// get walks object keys; a missing key or a null/non-object step yields nil.
func get(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// has reports whether an object has a key (null → false, like jq 1.7).
func has(v any, k string) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m[k]
	return ok
}

// objOK: a value that jq can index with .key (object or null).
func objOK(v any) bool { return v == nil || isObject(v) }

func str(v any) (string, bool) { s, ok := v.(string); return s, ok }

// strOr is `(v // def)` for a value expected to be a string ("" stays "").
func strOr(v any, def string) any {
	if v == nil || v == false {
		return def
	}
	return v
}

// jqLength is jq `length` (error → ok=false).
func jqLength(v any) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, true
	case string:
		return float64(runeLen(x)), true
	case []any:
		return float64(len(x)), true
	case map[string]any:
		return float64(len(x)), true
	}
	if f, ok := num(v); ok {
		return math.Abs(f), true
	}
	return 0, false
}

func isInt(v any) (float64, bool) {
	f, ok := num(v)
	return f, ok && f == math.Floor(f)
}

func isText(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func hexPin(v any) bool {
	s, ok := v.(string)
	if !ok || runeLen(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func urlKey(v any) bool {
	s, ok := v.(string)
	if !ok || runeLen(s) != 2603 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func keysSubset(m map[string]any, allowed ...string) bool {
	for k := range m {
		if !in(k, allowed...) {
			return false
		}
	}
	return true
}

// ---------- broray_server_stream_fields_valid (util.sh:191-266) ----------

func kcpOptionsValid(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || !keysSubset(m, "mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow") {
		return false
	}
	for _, x := range m {
		f, ok := isInt(x)
		if !ok || f < 0 || f > 4294967295 {
			return false
		}
	}
	d := func(k string, def float64) float64 {
		if x := alt(m[k]); x != nil {
			f, _ := num(x)
			return f
		}
		return def
	}
	mtu := d("mtu", 1350)
	tti := d("tti", 20)
	return mtu >= 21 && mtu <= 65507 && tti >= 10 && tti <= 1000 && d("cwndMultiplier", 1) >= 1 && d("maxSendingWindow", 1048576) >= mtu
}

func maskValid(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || !keysSubset(m, "tcp", "udp", "quicParams") {
		return false
	}
	items := func(v any, types ...string) bool {
		v = alt(v, []any{})
		arr, ok := v.([]any)
		if !ok {
			return false
		}
		for _, it := range arr {
			o, ok := it.(map[string]any)
			if !ok || !keysSubset(o, "type", "settings") {
				return false
			}
			t, ok := o["type"].(string)
			if !ok || !in(t, types...) {
				return false
			}
			if !isObject(alt(o["settings"], map[string]any{})) {
				return false
			}
		}
		return true
	}
	return items(m["tcp"], "header-custom", "fragment", "sudoku", "xmc") &&
		items(m["udp"], "header-custom", "mkcp-legacy", "noise", "salamander", "sudoku", "xdns", "xicmp", "realm", "udphop")
}

func udpTypes(fm any) ([]string, bool) {
	arr, ok := alt(get(fm, "udp"), []any{}).([]any)
	if !ok {
		return nil, false
	}
	var out []string
	for _, it := range arr {
		t, _ := get(it, "type").(string)
		out = append(out, t)
	}
	return out, true
}

func streamFieldsValid(s map[string]any) bool {
	for _, k := range []string{"tls", "reality", "transport", "xhttp", "hysteria", "grpc", "ws", "httpupgrade"} {
		if !objOK(s[k]) {
			return false
		}
	}
	protocol, _ := s["protocol"].(string)
	network := s["network"]
	security := s["security"]
	if protocol == "hysteria2" {
		hy := s["hysteria"]
		if has(hy, "udpIdleTimeout") && !nonNegInt32(get(hy, "udpIdleTimeout")) {
			return false
		}
		if !isText(s["auth"]) {
			return false
		}
		obfs := strOr(get(hy, "obfs"), "")
		obfsS, ok := obfs.(string)
		if !ok || !in(obfsS, "", "salamander", "gecko") {
			return false
		}
		pw := strOr(get(hy, "obfsPassword"), "")
		if !isText(pw) {
			return false
		}
		if (obfsS == "") != (pw == "") {
			return false
		}
		fm := alt(get(hy, "finalMask"), map[string]any{})
		if !maskValid(fm) {
			return false
		}
		types, ok := udpTypes(get(hy, "finalMask"))
		if !ok {
			return false
		}
		if obfsS != "" && in("salamander", types...) {
			return false
		}
		ports := strOr(get(hy, "ports"), "")
		if ports != "" {
			if in("udphop", types...) {
				return false
			}
			ps, ok := ports.(string)
			if !ok {
				return false
			}
			items := strings.Split(ps, ",")
			if len(items) == 0 || len(items) > 128 {
				return false
			}
			for _, it := range items {
				parts := strings.Split(it, "-")
				if len(parts) != 1 && len(parts) != 2 {
					return false
				}
				var nums []int
				for _, part := range parts {
					if len(part) == 0 || len(part) > 5 || !allDigits(part) {
						return false
					}
					n, _ := strconv.Atoi(part)
					if n < 1 || n > 65535 {
						return false
					}
					nums = append(nums, n)
				}
				if len(nums) == 2 && nums[0] > nums[1] {
					return false
				}
			}
			first, _, _ := splitFirst(ps, ',')
			first, _, _ = splitFirst(first, '-')
			n, _ := strconv.Atoi(first)
			if !jsonEq(s["port"], n) {
				return false
			}
		}
	}
	if get(s, "tls", "allowInsecure") == true {
		return false
	}
	tr := s["transport"]
	if in(protocol, "vless", "vmess", "trojan") {
		switch network {
		case "xhttp":
			m, ok := strOr(get(tr, "mode"), "auto").(string)
			if !ok || !in(m, "auto", "packet-up", "stream-up", "stream-one") {
				return false
			}
		case "grpc":
			m, ok := strOr(get(tr, "mode"), "gun").(string)
			if !ok || !in(m, "gun", "multi", "auto") {
				return false
			}
		}
		if network != "xhttp" {
			l, ok := jqLength(alt(get(tr, "extra"), map[string]any{}))
			if !ok || l != 0 {
				return false
			}
		}
		ht := get(tr, "headerType")
		switch network {
		case "raw":
			if !(ht == nil || ht == "none" || ht == "http") {
				return false
			}
		case "kcp":
		default:
			if !(ht == nil || ht == "none") {
				return false
			}
		}
	}
	if network == "kcp" {
		if !(security == "none" || security == "tls") {
			return false
		}
		if !kcpOptionsValid(alt(get(tr, "kcp"), map[string]any{})) {
			return false
		}
		if !isText(strOr(get(tr, "kcpSeed"), "")) {
			return false
		}
		legacy := alt(get(tr, "kcpLegacy"), false)
		if _, ok := legacy.(bool); !ok {
			return false
		}
		h, ok := strOr(get(tr, "headerType"), "none").(string)
		if !ok || !in(h, "none", "srtp", "utp", "wechat-video", "wechat", "dtls", "wireguard", "dns") {
			return false
		}
		if strOr(get(tr, "host"), "") != "" && get(tr, "headerType") != "dns" {
			return false
		}
		if legacy == true {
			l, ok := jqLength(alt(get(tr, "finalMask"), map[string]any{}))
			if !ok || l != 0 {
				return false
			}
		}
	}
	tls := s["tls"]
	for _, k := range []string{"echConfigList", "verifyPeerCertByName", "pinnedPeerCertSha256"} {
		if has(tls, k) && !(security == "tls" && isText(get(tls, k))) {
			return false
		}
	}
	if pin := strOr(get(tls, "pinnedPeerCertSha256"), ""); pin != "" {
		ps, ok := pin.(string)
		if !ok {
			return false
		}
		for _, p := range strings.Split(ps, ",") {
			if !hexPin(p) {
				return false
			}
		}
	}
	if pqv := strOr(get(s, "reality", "mldsa65Verify"), ""); pqv != "" {
		if !(security == "reality" && urlKey(pqv)) {
			return false
		}
	}
	if has(tr, "finalMask") && !maskValid(get(tr, "finalMask")) {
		return false
	}
	return validateModel(s)
}

// ---------- subscription-xray-json.jq validate_model (:309-320) ----------

func validateModel(s map[string]any) bool {
	if s["network"] == "xhttp" {
		x := alt(s["xhttp"], s["transport"], map[string]any{})
		if !isObject(x) {
			return false
		}
		mode, ok := strOr(get(x, "mode"), "auto").(string)
		if !ok || !in(mode, "", "auto", "packet-up", "stream-up", "stream-one") {
			return false
		}
		extra := map[string]any{}
		if has(x, "extra") {
			e, ok := get(x, "extra").(map[string]any)
			if !ok {
				return false
			}
			extra = e
		}
		if !xhttpExtraValid(extra, mode, 0) {
			return false
		}
	}
	tr := alt(s["transport"], map[string]any{})
	if !isObject(tr) {
		return false
	}
	if has(tr, "header") {
		path, _ := strOr(get(tr, "path"), "/").(string)
		if !(s["network"] == "raw" && strOr(get(tr, "headerType"), "none") == "none" && strOr(get(tr, "host"), "") == "" && in(path, "", "/")) {
			return false
		}
		if !rawHeaderValid(get(tr, "header")) {
			return false
		}
	}
	return true
}

func textValue(v any) bool {
	s, ok := v.(string)
	return ok && runeLen(s) <= 4096 && isText(s)
}

func decimal(v any) bool { s, ok := v.(string); return ok && allDigits(s) }

// rangePair is range_pair → [lo, hi] or ok=false.
func rangePair(v any) ([2]float64, bool) {
	if f, ok := num(v); ok {
		if f != math.Floor(f) || f < 0 || f > 2147483647 {
			return [2]float64{}, false
		}
		return [2]float64{f, f}, true
	}
	s, ok := v.(string)
	if !ok {
		return [2]float64{}, false
	}
	if s == "" {
		return [2]float64{}, true
	}
	parts := strings.Split(s, "-")
	if len(parts) != 1 && len(parts) != 2 {
		return [2]float64{}, false
	}
	var ns []float64
	for _, p := range parts {
		if !allDigits(p) || len(p) > 10 {
			return [2]float64{}, false
		}
		f, _ := strconv.ParseFloat(p, 64)
		if f > 2147483647 {
			return [2]float64{}, false
		}
		ns = append(ns, f)
	}
	if len(ns) == 1 {
		return [2]float64{ns[0], ns[0]}, true
	}
	if ns[0] > ns[1] {
		return [2]float64{}, false
	}
	return [2]float64{ns[0], ns[1]}, true
}

var xhttpExtraKeys = []string{"headers", "xPaddingBytes", "noGRPCHeader", "noSSEHeader", "scMaxEachPostBytes",
	"scMinPostsIntervalMs", "scMaxBufferedPosts", "scStreamUpServerSecs", "xmux",
	"xPaddingObfsMode", "xPaddingKey", "xPaddingHeader", "xPaddingPlacement", "xPaddingMethod",
	"uplinkHTTPMethod", "sessionIDPlacement", "sessionIDKey", "sessionIDTable", "sessionIDLength",
	"seqPlacement", "seqKey", "uplinkDataPlacement", "uplinkDataKey", "uplinkChunkSize",
	"serverMaxHeaderBytes", "downloadSettings"}

func nonNegInt32(v any) bool {
	f, ok := isInt(v)
	return ok && f >= 0 && f <= 2147483647
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func asciiLower(s string) string { return asciiOnlyLower(s) }

// xhttpExtraValid is xhttp_extra($mode;$depth).
func xhttpExtraValid(e map[string]any, mode string, depth int) bool {
	if !keysSubset(e, append(append([]string(nil), xhttpExtraKeys...), "mode")...) {
		return false
	}
	if has(e, "mode") {
		m, ok := e["mode"].(string)
		if !ok || orStr(m, "auto") != orStr(mode, "auto") {
			return false
		}
	}
	if b, err := json.Marshal(e); err != nil || runeLen(string(b)) > 4096 {
		return false
	}
	headers := map[string]any{}
	if has(e, "headers") {
		h, ok := e["headers"].(map[string]any)
		if !ok {
			return false
		}
		headers = h
	}
	for k, v := range headers {
		if !(textValue(k) && runeLen(k) > 0 && asciiLower(k) != "host" && textValue(v)) {
			return false
		}
	}
	for _, k := range []string{"noGRPCHeader", "noSSEHeader", "xPaddingObfsMode"} {
		if has(e, k) {
			if _, ok := e[k].(bool); !ok {
				return false
			}
		}
	}
	for _, k := range []string{"xPaddingBytes", "scMaxEachPostBytes", "scMinPostsIntervalMs", "scStreamUpServerSecs", "sessionIDLength", "uplinkChunkSize"} {
		if has(e, k) {
			if _, ok := rangePair(e[k]); !ok {
				return false
			}
		}
	}
	if has(e, "scMaxBufferedPosts") && !nonNegInt32(e["scMaxBufferedPosts"]) {
		return false
	}
	xm := map[string]any{}
	if has(e, "xmux") {
		m, ok := e["xmux"].(map[string]any)
		if !ok {
			return false
		}
		xm = m
	}
	if !keysSubset(xm, "maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs", "hKeepAlivePeriod") {
		return false
	}
	for _, k := range []string{"maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs"} {
		if has(xm, k) {
			if _, ok := rangePair(xm[k]); !ok {
				return false
			}
		}
	}
	if has(xm, "hKeepAlivePeriod") && !strictjson.SignedInt64(xm["hKeepAlivePeriod"]) {
		return false
	}
	mc, ok1 := rangePair(alt(xm["maxConcurrency"], json.Number("0")))
	mx, ok2 := rangePair(alt(xm["maxConnections"], json.Number("0")))
	if !ok1 || !ok2 || (mc[1] > 0 && mx[1] > 0) {
		return false
	}
	if has(e, "xPaddingBytes") {
		r, _ := rangePair(e["xPaddingBytes"])
		if !(r[0] > 0 || (r[0] == 0 && r[1] == 0)) {
			return false
		}
	}
	for _, k := range []string{"xPaddingKey", "xPaddingHeader", "xPaddingPlacement", "xPaddingMethod", "uplinkHTTPMethod",
		"sessionIDPlacement", "sessionIDKey", "sessionIDTable", "seqPlacement", "seqKey", "uplinkDataPlacement", "uplinkDataKey"} {
		if has(e, k) && !textValue(e[k]) {
			return false
		}
	}
	sOr := func(k string) (string, bool) {
		v := alt(e[k], "")
		s, ok := v.(string)
		return s, ok
	}
	if v, ok := sOr("xPaddingPlacement"); !ok || !in(v, "", "cookie", "header", "query", "queryInHeader") {
		return false
	}
	if v, ok := sOr("xPaddingMethod"); !ok || !in(v, "", "repeat-x", "tokenish") {
		return false
	}
	for _, k := range []string{"sessionIDPlacement", "seqPlacement"} {
		if v, ok := sOr(k); !ok || !in(v, "", "path", "cookie", "header", "query") {
			return false
		}
	}
	udp, ok := sOr("uplinkDataPlacement")
	if !ok || !in(udp, "", "auto", "body", "cookie", "header") {
		return false
	}
	method, ok := alt(e["uplinkHTTPMethod"], "POST").(string)
	if !ok {
		return false
	}
	if mode != "packet-up" && (in(udp, "cookie", "header") || asciiUpper(method) == "GET") {
		return false
	}
	if has(e, "serverMaxHeaderBytes") && !nonNegInt32(e["serverMaxHeaderBytes"]) {
		return false
	}
	if table, _ := alt(e["sessionIDTable"], "").(string); table != "" {
		sizes := map[string]float64{"ALPHABET": 26, "Alphabet": 52, "BASE36": 36, "Base62": 62, "HEX": 16, "alphabet": 26, "base36": 36, "hex": 16, "number": 10}
		size, known := sizes[table]
		if !known {
			size = float64(runeLen(table))
		}
		r, ok := rangePair(alt(e["sessionIDLength"], json.Number("0")))
		if !ok {
			return false
		}
		for _, c := range table {
			if c >= 128 {
				return false
			}
		}
		if !(size >= 2 && r[0] > 0) {
			return false
		}
		if r[1] < 31 {
			total := 0.0
			for n := r[0]; n <= r[1]; n++ {
				total += math.Pow(size, n)
			}
			if total < 2147483648 {
				return false
			}
		}
	}
	if has(e, "downloadSettings") {
		if depth != 0 || mode == "stream-one" {
			return false
		}
		d, ok := e["downloadSettings"].(map[string]any)
		if !ok {
			return false
		}
		if !downloadSettingsValid(d) {
			return false
		}
	}
	return true
}

func jqNetwork(s map[string]any) (string, bool) {
	v := "tcp"
	if has(s, "network") {
		if !textValue(s["network"]) {
			return "", false
		}
		v = s["network"].(string)
	}
	switch v {
	case "raw", "tcp":
		return "tcp", true
	case "websocket", "ws":
		return "ws", true
	case "httpUpgrade", "httpupgrade":
		return "httpupgrade", true
	case "splithttp", "xhttp":
		return "xhttp", true
	case "grpc", "kcp":
		return v, true
	}
	return "", false
}

func stringField(o map[string]any, k, def string) (string, bool) {
	if !has(o, k) {
		return def, true
	}
	if !textValue(o[k]) {
		return "", false
	}
	return o[k].(string), true
}

func downloadSettingsValid(d map[string]any) bool {
	if !keysSubset(d, "address", "port", "network", "security", "tlsSettings", "realitySettings", "xhttpSettings", "splithttpSettings", "finalmask") {
		return false
	}
	if n, ok := jqNetwork(d); !ok || n != "xhttp" {
		return false
	}
	if has(d, "address") && !addressURIValid(d["address"]) {
		return false
	}
	if has(d, "port") {
		f, ok := isInt(d["port"])
		if !ok || f < 1 || f > 65535 {
			return false
		}
	}
	if !protectionValid(d) {
		return false
	}
	if has(d, "finalmask") && !isObject(d["finalmask"]) {
		return false
	}
	if has(d, "xhttpSettings") && has(d, "splithttpSettings") {
		return false
	}
	var x map[string]any = map[string]any{}
	for _, k := range []string{"xhttpSettings", "splithttpSettings"} {
		if has(d, k) {
			m, ok := d[k].(map[string]any)
			if !ok {
				return false
			}
			x = m
		}
	}
	if !keysSubset(x, append([]string{"host", "path", "mode", "extra"}, xhttpExtraKeys...)...) {
		return false
	}
	if _, ok := stringField(x, "host", ""); !ok {
		return false
	}
	if _, ok := stringField(x, "path", "/"); !ok {
		return false
	}
	m, ok := stringField(x, "mode", "auto")
	if !ok || !in(m, "", "auto", "packet-up", "stream-up", "stream-one") {
		return false
	}
	var extra map[string]any
	if has(x, "extra") {
		for k := range x {
			if in(k, xhttpExtraKeys...) {
				return false
			}
		}
		e, ok := x["extra"].(map[string]any)
		if !ok {
			return false
		}
		extra = e
	} else {
		extra = map[string]any{}
		for k, v := range x {
			if !in(k, "host", "path", "mode") {
				extra[k] = v
			}
		}
	}
	return xhttpExtraValid(extra, m, 1)
}

func unreserved(v any) bool {
	s, ok := v.(string)
	if !ok || runeLen(s) == 0 || runeLen(s) > 255 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-' || r == '.' || r == '_' || r == '~') {
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
		if !isHex(s[i]) {
			return false
		}
	}
	return true
}

func ipv4Literal(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if !allDigits(p) || len(p) > 3 || (len(p) > 1 && p[0] == '0') {
			return false
		}
		n, _ := strconv.Atoi(p)
		if n > 255 {
			return false
		}
	}
	return true
}

func ipv6Literal(s string) bool {
	if len(s) < 2 || len(s) > 45 || !strings.Contains(s, ":") {
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
	groups := func(h string) ([]string, bool) {
		if h == "" {
			return nil, true
		}
		g := strings.Split(h, ":")
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

func addressURIValid(v any) bool {
	s, ok := v.(string)
	if !ok || runeLen(s) == 0 || runeLen(s) > 255 {
		return false
	}
	if strings.HasPrefix(s, "[") || strings.HasSuffix(s, "]") {
		if !(strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")) || len(s) < 2 {
			return false
		}
		return ipv6Literal(s[1 : len(s)-1])
	}
	if strings.Contains(s, ":") {
		return ipv6Literal(s)
	}
	return unreserved(s)
}

// protectionValid is `protection` (subscription-xray-json.jq:63-90), validity only.
func protectionValid(s map[string]any) bool {
	security, ok := stringField(s, "security", "none")
	if !ok || !in(security, "none", "tls", "reality") {
		return false
	}
	switch security {
	case "reality":
		if has(s, "tlsSettings") {
			return false
		}
		if n, ok := jqNetwork(s); !ok || !in(n, "tcp", "grpc", "xhttp") {
			return false
		}
		r, ok := s["realitySettings"].(map[string]any)
		if !ok || !keysSubset(r, "serverName", "fingerprint", "publicKey", "password", "shortId", "spiderX", "show", "mldsa65Verify") {
			return false
		}
		if has(r, "show") {
			if _, ok := r["show"].(bool); !ok {
				return false
			}
		}
		if !textValue(r["serverName"]) || r["serverName"] == "" {
			return false
		}
		if has(r, "publicKey") && has(r, "password") && !jsonValueEqual(r["publicKey"], r["password"]) {
			return false
		}
		key := r["publicKey"]
		if has(r, "password") {
			key = r["password"]
		}
		if !textValue(key) || key == "" {
			return false
		}
		for _, k := range []string{"fingerprint", "shortId", "spiderX", "mldsa65Verify"} {
			if _, ok := stringField(r, k, ""); !ok {
				return false
			}
		}
	case "tls":
		if has(s, "realitySettings") {
			return false
		}
		t := map[string]any{}
		if has(s, "tlsSettings") {
			m, ok := s["tlsSettings"].(map[string]any)
			if !ok {
				return false
			}
			t = m
		}
		if !keysSubset(t, "serverName", "fingerprint", "alpn", "allowInsecure", "echConfigList", "pinnedPeerCertSha256", "verifyPeerCertByName") {
			return false
		}
		if has(t, "allowInsecure") && t["allowInsecure"] != false {
			return false
		}
		if has(t, "alpn") {
			arr, ok := t["alpn"].([]any)
			if !ok {
				return false
			}
			for _, a := range arr {
				s, ok := a.(string)
				if !textValue(a) || !ok || s == "" || runeLen(s) > 255 || strings.Contains(s, ",") {
					return false
				}
			}
		}
		for _, k := range []string{"serverName", "fingerprint", "echConfigList", "pinnedPeerCertSha256", "verifyPeerCertByName"} {
			if _, ok := stringField(t, k, ""); !ok {
				return false
			}
		}
	default:
		if has(s, "tlsSettings") || has(s, "realitySettings") {
			return false
		}
	}
	return true
}

func jsonValueEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func stringList(v any) bool {
	if _, ok := v.(string); ok {
		return textValue(v)
	}
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range arr {
		if !textValue(x) {
			return false
		}
	}
	return true
}

// rawHeaderValid is raw_header (subscription-xray-json.jq:169-180).
func rawHeaderValid(v any) bool {
	h, ok := v.(map[string]any)
	if !ok || !keysSubset(h, "type", "request", "response") {
		return false
	}
	if len(h) == 0 || (len(h) == 1 && h["type"] == "none") {
		return true
	}
	if h["type"] != "http" {
		return false
	}
	obj := func(k string) (map[string]any, bool) {
		if !has(h, k) {
			return map[string]any{}, true
		}
		m, ok := h[k].(map[string]any)
		return m, ok
	}
	q, ok1 := obj("request")
	r, ok2 := obj("response")
	if !ok1 || !ok2 || !keysSubset(q, "version", "method", "path", "headers") || !keysSubset(r, "version", "status", "reason", "headers") {
		return false
	}
	for _, o := range []map[string]any{q, r} {
		for k, val := range o {
			if k == "path" || k == "headers" {
				continue
			}
			if !textValue(val) {
				return false
			}
		}
	}
	if has(q, "path") && !stringList(q["path"]) {
		return false
	}
	for _, o := range []map[string]any{q, r} {
		hd := map[string]any{}
		if has(o, "headers") {
			m, ok := o["headers"].(map[string]any)
			if !ok {
				return false
			}
			hd = m
		}
		for k, val := range hd {
			if !(textValue(k) && k != "" && stringList(val)) {
				return false
			}
		}
	}
	return true
}

// ---------- broray_server_validate (server.sh:230-309) ----------

func nonEmptyString(v any) bool { s, ok := v.(string); return ok && s != "" }

func structureValid(s map[string]any) bool {
	if !jsonEq(s["schemaVersion"], 2) || !nonEmptyString(s["id"]) {
		return false
	}
	if _, ok := s["name"].(string); !ok {
		return false
	}
	if !nonEmptyString(s["protocol"]) || !nonEmptyString(s["address"]) {
		return false
	}
	port, ok := num(s["port"])
	if !ok || port < 1 || port > 65535 {
		return false
	}
	switch s["protocol"] {
	case "trojan":
		if !nonEmptyString(s["password"]) {
			return false
		}
	case "hysteria2":
		if _, ok := s["auth"].(string); !ok {
			return false
		}
		if s["network"] != "hysteria" || s["security"] != "tls" || !isObject(s["hysteria"]) || !jsonEq(get(s, "hysteria", "version"), 2) {
			return false
		}
		if get(s, "tls", "allowInsecure") == true {
			return false
		}
		if has(s["tls"], "pinnedPeerCertSha256") && !hexPin(get(s, "tls", "pinnedPeerCertSha256")) {
			return false
		}
		if !isObject(s["tls"]) || !nonEmptyString(get(s, "tls", "serverName")) {
			return false
		}
	case "shadowsocks":
		if !nonEmptyString(s["method"]) || !nonEmptyString(s["password"]) || s["network"] != "raw" || s["security"] != "none" {
			return false
		}
	case "vless":
		enc, ok := strOr(s["encryption"], "none").(string)
		if !ok || !VlessEncryptionValid(enc) {
			return false
		}
		if s["security"] == "reality" && !in(asString(s["network"]), "raw", "grpc", "xhttp") {
			return false
		}
		if !nonEmptyString(s["uuid"]) {
			return false
		}
		if s["flow"] != nil {
			if !(in(asString(s["flow"]), "xtls-rprx-vision", "xtls-rprx-vision-udp443") &&
				(s["network"] == "raw" || enc != "none") && (s["security"] == "reality" || s["security"] == "tls")) {
				return false
			}
		}
	default:
		if !nonEmptyString(s["uuid"]) {
			return false
		}
	}
	return nonEmptyString(s["network"]) && nonEmptyString(s["security"]) && isObject(s["source"]) && nonEmptyString(get(s, "source", "type"))
}

func asString(v any) string { s, _ := v.(string); return s }

// Validate is broray_server_validate of a record (label = the file path shown
// in the 3.x message "сервер имеет неправильную структуру: <file>").
func Validate(rec Record, label string) error {
	if err := validate(rec, label); err != nil {
		return err
	}
	return nil
}

func validate(rec Record, label string) *ParseError {
	if e := streamOptionsError(rec); e != nil {
		return e
	}
	if rec["protocol"] == "wireguard" {
		return validateWireGuard(rec)
	}
	if in(asString(rec["protocol"]), "http", "socks") {
		return validateProxy(rec)
	}
	if rec["protocol"] == "amneziawg" {
		return validateAmnezia(rec)
	}
	if rec["network"] == "xdrive" {
		return validateXDrive(rec)
	}
	if rec["protocol"] == "masque" {
		return validateMasque(rec)
	}
	if !streamFieldsValid(rec) {
		return die("сервер содержит неподдерживаемые параметры TLS/REALITY/FinalMask")
	}
	if !structureValid(rec) {
		return die("сервер имеет неправильную структуру: " + label)
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
