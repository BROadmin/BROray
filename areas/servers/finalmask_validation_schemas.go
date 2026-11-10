package servers

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Schemas and Build constraints follow infra/conf/transport_finalmask.go at
// Xray v26.11.9. This check does not execute masks, resolve names, or open files.
func freshMask(v any, path string, depth int) *ParseError {
	_, e := freshObject(v, path, depth, map[string]freshField{
		"tcp":        freshArray(freshMaskEntry("tcp")),
		"udp":        freshArray(freshMaskEntry("udp")),
		"quicParams": freshQuic,
	})
	return e
}

func freshMaskEntry(lane string) freshField {
	return func(v any, path string, depth int) *ParseError {
		m, e := freshObject(v, path, depth, map[string]freshField{"type": freshString, "settings": freshMapShape})
		if e != nil {
			return e
		}
		kind := asString(m["type"])
		var check freshField
		switch kind {
		case "header-custom":
			check = freshHeader(lane)
		case "sudoku":
			check = freshSudoku
		case "fragment":
			if lane == "tcp" {
				check = freshFragment
			}
		case "xmc":
			if lane == "tcp" {
				check = freshXMC
			}
		case "mkcp-legacy":
			if lane == "udp" {
				check = freshMkcp
			}
		case "noise":
			if lane == "udp" {
				check = freshNoise
			}
		case "salamander":
			if lane == "udp" {
				check = freshSalamander
			}
		case "xdns":
			if lane == "udp" {
				check = freshXDNS
			}
		case "xicmp":
			if lane == "udp" {
				check = freshXicmp
			}
		case "realm":
			if lane == "udp" {
				check = freshRealm
			}
		case "udphop":
			if lane == "udp" {
				check = freshHop
			}
		}
		// filter remains the existing explicit BROray exclusion.
		if check == nil {
			return nativeError(path + ".type")
		}
		settings := any(map[string]any{})
		if has(m, "settings") {
			settings = m["settings"]
		}
		return check(settings, path+".settings", depth+1)
	}
}

func freshMapShape(v any, path string, depth int) *ParseError {
	m, ok := v.(map[string]any)
	if !ok || m == nil || depth > 64 {
		return nativeError(path)
	}
	return nil
}

// Raw byte fields are checked together with their encoding discriminator.
func freshRaw(v any, path string, _ int) *ParseError {
	if v == nil {
		return nativeError(path)
	}
	return nil
}

func freshBytes(m map[string]any, key, path string) *ParseError {
	kind := strings.ToLower(asString(m["type"]))
	value, exists := m[key]
	if !exists && in(kind, "", "array") {
		return nil
	}
	b, err := json.Marshal(value)
	if !exists || err != nil {
		return nativeError(path + "." + key)
	}
	switch kind {
	case "", "array":
		if _, ok := value.([]any); ok {
			if e := freshArray(freshInt(8, false))(value, path+"."+key, 0); e != nil {
				return e
			}
		}
		var data []byte
		err = json.Unmarshal(b, &data)
	case "str", "hex", "base64":
		var s string
		err = json.Unmarshal(b, &s)
		if err == nil && kind == "hex" {
			_, err = hex.DecodeString(s)
		}
		if err == nil && kind == "base64" {
			_, err = base64.StdEncoding.DecodeString(s)
		}
	default:
		return nativeError(path + ".type")
	}
	if err != nil {
		return nativeError(path + "." + key)
	}
	return nil
}

var freshVariable = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func freshCustomItem(tcp bool) freshField {
	return func(v any, path string, depth int) *ParseError {
		fields := freshFields("capture type reuse", freshString)
		freshAdd(fields, "rand", freshInt(32, true))
		freshAdd(fields, "randRange", freshRangeField)
		fields["packet"] = freshRaw
		fields["transform"] = freshTransform
		if tcp {
			fields["delay"] = freshRangeField
		}
		m, e := freshObject(v, path, depth, fields)
		if e != nil {
			return e
		}
		for _, key := range []string{"capture", "reuse"} {
			if s := asString(m[key]); s != "" && !freshVariable.MatchString(s) {
				return nativeError(path + "." + key)
			}
		}
		n := 0
		if has(m, "packet") {
			n++
		}
		if r, _ := freshSigned(m["rand"], 32); r > 0 {
			n++
		}
		if asString(m["reuse"]) != "" {
			n++
		}
		if has(m, "transform") {
			n++
		}
		if n > 1 || (n == 0 && asString(m["capture"]) != "") {
			return nativeError(path)
		}
		if has(m, "randRange") {
			a, b := freshDefaultRange(m, "randRange")
			if a < 0 || b > 255 {
				return nativeError(path + ".randRange")
			}
		}
		return freshBytes(m, "packet", path)
	}
}

func freshTransform(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, map[string]freshField{"op": freshString, "args": freshArray(freshTransformArg)})
	if e != nil {
		return e
	}
	if asString(m["op"]) == "" {
		return nativeError(path + ".op")
	}
	if a, _ := m["args"].([]any); len(a) == 0 {
		return nativeError(path + ".args")
	}
	return nil
}

func freshTransformArg(v any, path string, depth int) *ParseError {
	fields := freshFields("type reuse metadata", freshString)
	fields["bytes"] = freshRaw
	fields["u64"] = freshInt(64, false)
	fields["transform"] = freshTransform
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	n := 0
	for _, key := range []string{"bytes", "u64", "transform"} {
		if has(m, key) {
			n++
		}
	}
	for _, key := range []string{"reuse", "metadata"} {
		if asString(m[key]) != "" {
			n++
		}
	}
	if n != 1 {
		return nativeError(path)
	}
	if s := asString(m["reuse"]); s != "" && !freshVariable.MatchString(s) {
		return nativeError(path + ".reuse")
	}
	if has(m, "bytes") {
		return freshBytes(m, "bytes", path)
	}
	return nil
}

func freshHeader(lane string) freshField {
	return func(v any, path string, depth int) *ParseError {
		if lane == "tcp" {
			_, e := freshObject(v, path, depth, freshFields("clients servers errors", freshArray(freshArray(freshCustomItem(true)))))
			return e
		}
		fields := freshFields("client server", freshArray(freshCustomItem(false)))
		fields["mode"] = freshString
		m, e := freshObject(v, path, depth, fields)
		if e != nil {
			return e
		}
		if !in(asString(m["mode"]), "", "prefix", "standalone") {
			return nativeError(path + ".mode")
		}
		return nil
	}
}

func freshFragment(v any, path string, depth int) *ParseError {
	fields := freshFields("length delay maxSplit", freshRangeField)
	freshAdd(fields, "lengths delays", freshArray(freshRangeField))
	fields["packets"] = freshString
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	packets := strings.ToLower(asString(m["packets"]))
	if !in(packets, "", "tlshello") {
		a, _, ok := freshRangePair(packets, strconv.IntSize)
		if !ok || a == 0 {
			return nativeError(path + ".packets")
		}
	}
	a, _ := freshDefaultRange(m, "length")
	key := "length"
	if lengths, _ := m["lengths"].([]any); len(lengths) > 0 {
		a, _, _ = freshRange(lengths[len(lengths)-1])
		key = "lengths"
	}
	if a == 0 {
		return nativeError(path + "." + key)
	}
	return nil
}

func freshNoise(v any, path string, depth int) *ParseError {
	_, e := freshObject(v, path, depth, map[string]freshField{"reset": freshRangeField, "noise": freshArray(freshNoiseItem), "script": freshEmptyString})
	return e
}

func freshNoiseItem(v any, path string, depth int) *ParseError {
	fields := freshFields("rand randRange delay", freshRangeField)
	fields["type"] = freshString
	fields["packet"] = freshRaw
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	_, b := freshDefaultRange(m, "rand")
	if has(m, "packet") && b > 0 {
		return nativeError(path + ".packet")
	}
	if strings.ToLower(asString(m["type"])) == "exp" {
		return freshNoiseExpression(m["packet"], path+".packet")
	}
	if has(m, "randRange") {
		a, b := freshDefaultRange(m, "randRange")
		if a < 0 || b > 255 {
			return nativeError(path + ".randRange")
		}
	}
	return freshBytes(m, "packet", path)
}

var freshNoiseToken = regexp.MustCompile(`<\s*([a-z]+)(?:\s+([^>]*?))?\s*>`)

func freshNoiseExpression(v any, path string) *ParseError {
	s, ok := v.(string)
	if !ok {
		return nativeError(path)
	}
	matches := freshNoiseToken.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return nativeError(path)
	}
	end := 0
	for _, idx := range matches {
		if strings.TrimSpace(s[end:idx[0]]) != "" {
			return nativeError(path)
		}
		end = idx[1]
		kind := s[idx[2]:idx[3]]
		arg := ""
		if idx[4] >= 0 {
			arg = strings.TrimSpace(s[idx[4]:idx[5]])
		}
		switch kind {
		case "b":
			arg = strings.Join(strings.Fields(arg), "")
			arg = strings.TrimPrefix(strings.TrimPrefix(arg, "0x"), "0X")
			if b, e := hex.DecodeString(arg); e != nil || len(b) == 0 {
				return nativeError(path)
			}
		case "r", "rc", "rd":
			a, b, valid := freshRangePair(arg, strconv.IntSize)
			if arg == "" || !valid || a < 0 || b < a || b > 65535 {
				return nativeError(path)
			}
		case "t", "c", "n":
			if arg != "" {
				return nativeError(path)
			}
		default:
			return nativeError(path)
		}
	}
	if strings.TrimSpace(s[end:]) != "" {
		return nativeError(path)
	}
	return nil
}

func freshMkcp(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, freshFields("header value", freshString))
	if e != nil {
		return e
	}
	if !in(strings.ToLower(asString(m["header"])), "", "dns", "dtls", "srtp", "utp", "wechat", "wireguard") {
		return nativeError(path + ".header")
	}
	return nil
}

func freshSalamander(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, map[string]freshField{"password": freshString, "packetSize": freshRangeField})
	if e != nil {
		return e
	}
	a, b := freshDefaultRange(m, "packetSize")
	if b > 0 && (a <= 0 || b > 2048) {
		return nativeError(path + ".packetSize")
	}
	return nil
}

func freshSudoku(v any, path string, depth int) *ParseError {
	fields := freshFields("password ascii customTable custom_table", freshString)
	freshAdd(fields, "customTables custom_tables", freshArray(freshString))
	freshAdd(fields, "paddingMin padding_min paddingMax padding_max", freshInt(32, false))
	_, e := freshObject(v, path, depth, fields)
	return e
}

func freshXDNS(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, map[string]freshField{"domains": freshArray(freshXDNSDomain), "resolvers": freshArray(freshXDNSResolver), "extraPoll": freshInt(32, true)})
	if e != nil {
		return e
	}
	n, _ := freshSigned(m["extraPoll"], 32)
	if n < 0 || n > 3 {
		return nativeError(path + ".extraPoll")
	}
	return nil
}

func freshXDNSDomain(v any, path string, depth int) *ParseError {
	fields := freshFields("lenLimit labelLimit edns0", freshInt(32, true))
	fields["names"] = freshArray(freshString)
	fields["types"] = freshArray(freshInt(32, true))
	_, e := freshObject(v, path, depth, fields)
	return e
}

func freshXDNSResolver(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, map[string]freshField{"addrs": freshArray(freshString), "worker": freshInt(32, true), "tlsConfig": freshTLS, "quicConfig": freshQuic})
	if e != nil {
		return e
	}
	addrs, _ := m["addrs"].([]any)
	for i, v := range addrs {
		s := v.(string)
		if !strings.Contains(s, "://") {
			s = "udp://" + s
		}
		u, err := url.Parse(s)
		if err != nil || !in(u.Scheme, "tcp", "udp", "https") {
			return nativeError(path + ".addrs." + strconv.Itoa(i))
		}
	}
	return nil
}

var freshUsername = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

func freshXMC(v any, path string, depth int) *ParseError {
	fields := freshFields("hostname password", freshString)
	fields["profiles"] = freshArray(freshXMCProfile)
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	if asString(m["password"]) == "" {
		return nativeError(path + ".password")
	}
	if a, _ := m["profiles"].([]any); len(a) == 0 {
		return nativeError(path + ".profiles")
	}
	return nil
}

func freshXMCProfile(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, freshFields("username uuid texturesValue texturesSignature", freshString))
	if e != nil {
		return e
	}
	if !freshUsername.MatchString(asString(m["username"])) {
		return nativeError(path + ".username")
	}
	s := asString(m["uuid"])
	if len(s) == 45 && strings.EqualFold(s[:9], "urn:uuid:") {
		s = s[9:]
	}
	if len(s) == 38 && s[0] == '{' && s[37] == '}' {
		s = s[1:37]
	}
	if len(s) == 36 {
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return nativeError(path + ".uuid")
		}
		s = strings.ReplaceAll(s, "-", "")
	}
	if b, err := hex.DecodeString(s); err != nil || len(b) != 16 {
		return nativeError(path + ".uuid")
	}
	for _, key := range []string{"texturesValue", "texturesSignature"} {
		if asString(m[key]) == "" {
			return nativeError(path + "." + key)
		}
	}
	return nil
}

func freshXicmp(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, map[string]freshField{"dgram": freshBool, "ips": freshArray(freshString)})
	if e != nil {
		return e
	}
	a, _ := m["ips"].([]any)
	for i, v := range a {
		if _, err := netip.ParseAddr(v.(string)); err != nil {
			return nativeError(path + ".ips." + strconv.Itoa(i))
		}
	}
	return nil
}

func freshRealm(v any, path string, depth int) *ParseError {
	fields := freshFields("url ipMode", freshString)
	fields["stunServers"] = freshArray(freshString)
	fields["tlsConfig"] = freshTLS
	fields["portMapping"] = func(v any, p string, d int) *ParseError {
		f := freshFields("timeout lifetime", freshInt(64, true))
		f["enabled"] = freshBool
		_, e := freshObject(v, p, d, f)
		return e
	}
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	u, err := url.Parse(asString(m["url"]))
	if err != nil || !in(u.Scheme, "realm", "realm+http") || u.Hostname() == "" || u.User == nil || u.User.String() == "" || strings.TrimPrefix(u.Path, "/") == "" {
		return nativeError(path + ".url")
	}
	a, _ := m["stunServers"].([]any)
	if len(a) == 0 {
		return nativeError(path + ".stunServers")
	}
	for i, v := range a {
		if _, _, err := net.SplitHostPort(v.(string)); err != nil {
			return nativeError(path + ".stunServers." + strconv.Itoa(i))
		}
	}
	return nil
}

func freshHop(v any, path string, depth int) *ParseError {
	m, e := freshObject(v, path, depth, map[string]freshField{"mode": freshString, "interval": freshRangeField, "remoteIPs": freshArray(freshString), "remotePorts": freshPorts})
	if e != nil {
		return e
	}
	for _, mode := range strings.Split(strings.ToLower(asString(m["mode"])), ",") {
		if !in(mode, "intervallocal", "intervalremote", "perconnremote") {
			return nativeError(path + ".mode")
		}
	}
	a, b := freshDefaultRange(m, "interval")
	if (a != 0 || b != 0) && a < 5 {
		return nativeError(path + ".interval")
	}
	ips, _ := m["remoteIPs"].([]any)
	for i, v := range ips {
		s := v.(string)
		_, ea := netip.ParseAddr(s)
		_, ep := netip.ParsePrefix(s)
		if ea != nil && ep != nil {
			return nativeError(path + ".remoteIPs." + strconv.Itoa(i))
		}
	}
	return nil
}

func freshPorts(v any, path string, _ int) *ParseError {
	if s, ok := v.(string); ok {
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			ends := strings.Split(part, "-")
			if len(ends) > 2 {
				return nativeError(path)
			}
			for _, end := range ends {
				if _, e := strconv.ParseUint(end, 10, 16); e != nil {
					return nativeError(path)
				}
			}
		}
		return nil
	}
	return freshInt(32, false)(v, path, 0)
}
