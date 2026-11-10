package servers

import (
	"encoding/json"
	"strings"

	"github.com/BROadmin/broray/internal/strictjson"
)

func nativeError(field string) *ParseError {
	return die("неподдерживаемое или неправильное поле JSON: " + safeField(field))
}

func addNativeImportWarning(r Record, warning string) {
	warnings, _ := r["importWarnings"].([]any)
	for _, value := range warnings {
		if existing, ok := value.(string); ok && existing == warning {
			return
		}
	}
	r["importWarnings"] = append(warnings, warning)
}

func nativeKeys(m map[string]any, prefix string, allowed ...string) *ParseError {
	for _, k := range sortedKeys(m) {
		if !in(k, allowed...) {
			return nativeError(prefix + "." + k)
		}
	}
	return nil
}

func nativeObject(v any, prefix string, allowed ...string) (map[string]any, *ParseError) {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil, nativeError(prefix)
	}
	return m, nativeKeys(m, prefix, allowed...)
}

// ParseNativeOutbound imports only a client endpoint, never a foreign process,
// routing, listener or DNS configuration. Name is chosen by the caller.
func ParseNativeOutbound(input map[string]any, name, id string, source map[string]any) (Record, error) {
	r, err := parseNativeOutbound(input, name, id, source)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func parseNativeOutbound(input map[string]any, name, id string, source map[string]any) (Record, *ParseError) {
	// Detach caller-owned maps before storing credentials or normalizing fields.
	out := normalize(input)
	if err := nativeKeys(out, "outbound", "protocol", "tag", "settings", "streamSettings", "mux"); err != nil {
		return nil, err
	}
	if has(out, "tag") && !textValue(out["tag"]) {
		return nil, nativeError("outbound.tag")
	}
	if err := validateID(id); err != nil {
		return nil, err
	}
	if !textValue(name) {
		return nil, nativeError("name")
	}
	proto := asString(out["protocol"])
	st, err := nativeObject(out["streamSettings"], "streamSettings", "network", "security", "tlsSettings", "xdriveSettings", "masqueSettings", "sockopt", "finalmask")
	if err != nil {
		return nil, err
	}
	network := asString(st["network"])
	if !(proto == "vless" && network == "xdrive" || proto == "masque" && network == "masque") {
		return nil, nativeError("protocol.network")
	}
	if source == nil {
		source = map[string]any{"type": "manual"}
	}
	security := "none"
	if has(st, "security") {
		security = asString(st["security"])
	}
	r := Record{"schemaVersion": 2, "id": id, "name": name, "source": cloneSource(source), "protocol": proto, "network": network, "security": security}
	if has(st, "tlsSettings") {
		r["tls"] = st["tlsSettings"]
	}
	if has(st, "sockopt") {
		r["streamOptions"] = map[string]any{"sockopt": st["sockopt"]}
	}
	if has(st, "finalmask") {
		r["transport"] = map[string]any{"finalMask": st["finalmask"]}
	}
	if has(out, "mux") {
		r["mux"] = out["mux"]
	}
	if proto == "masque" {
		settings, e := nativeObject(out["settings"], "settings", "address", "port", "remoteDNS")
		if e != nil {
			return nil, e
		}
		r["address"], r["port"] = settings["address"], settings["port"]
		r["masque"] = st["masqueSettings"]
		if has(st, "xdriveSettings") {
			return nil, nativeError("streamSettings.xdriveSettings")
		}
		if has(settings, "remoteDNS") {
			r["remoteDNS"] = settings["remoteDNS"]
		}
	} else {
		settings, e := nativeVLESSSettings(out["settings"])
		if e != nil {
			return nil, e
		}
		endpoints, ok := settings["vnext"].([]any)
		if !ok || len(endpoints) != 1 {
			return nil, nativeError("settings.vnext")
		}
		endpoint, e := nativeObject(endpoints[0], "settings.vnext", "address", "port", "users")
		if e != nil {
			return nil, e
		}
		users, ok := endpoint["users"].([]any)
		if !ok || len(users) != 1 {
			return nil, nativeError("settings.vnext.users")
		}
		user, e := nativeObject(users[0], "settings.vnext.users", "id", "encryption", "flow", "level", "email")
		if e != nil {
			return nil, e
		}
		if has(user, "level") && !jsonEq(user["level"], 0) {
			return nil, nativeError("settings.vnext.users.level")
		}
		if has(user, "email") && !textValue(user["email"]) {
			return nil, nativeError("settings.vnext.users.email")
		}
		if has(user, "email") || has(user, "level") {
			addNativeImportWarning(r, "Служебные поля email и level=0 не применяются.")
		}
		r["address"], r["port"] = endpoint["address"], endpoint["port"]
		r["uuid"], r["encryption"] = user["id"], user["encryption"]
		if has(user, "flow") {
			r["flow"] = user["flow"]
		}
		r["xdrive"] = st["xdriveSettings"]
		if has(st, "masqueSettings") {
			return nil, nativeError("streamSettings.masqueSettings")
		}
	}
	if name == "" {
		r["name"] = r["address"]
	}
	if err := validate(r, "native JSON"); err != nil {
		return nil, err
	}
	if err := validateFreshFinalMasks(r); err != nil {
		return nil, err
	}
	return r, nil
}

// Xray accepts both the modern flat endpoint and the legacy vnext form.
// A document mixing them is ambiguous even though upstream prefers address.
func nativeVLESSSettings(input any) (map[string]any, *ParseError) {
	m, ok := input.(map[string]any)
	if !ok {
		return nil, nativeError("settings")
	}
	if !has(m, "address") {
		return nativeObject(m, "settings", "vnext")
	}
	if err := nativeKeys(m, "settings", "address", "port", "id", "encryption", "flow", "level", "email"); err != nil {
		return nil, err
	}
	user := map[string]any{}
	for _, k := range []string{"id", "encryption", "flow", "level", "email"} {
		if has(m, k) {
			user[k] = m[k]
		}
	}
	return map[string]any{"vnext": []any{map[string]any{"address": m["address"], "port": m["port"], "users": []any{user}}}}, nil
}

func parseNativeDocument(text, id string, source map[string]any) (Record, *ParseError) {
	v, err := strictjson.Decode([]byte(text))
	if err != nil {
		return nil, nativeError("document")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nativeError("document")
	}
	for _, field := range []string{"tag", "remarks"} {
		if has(m, field) && !textValue(m[field]) {
			return nil, nativeError(field)
		}
	}
	out := m
	name := asString(m["tag"])
	if has(m, "outbounds") {
		list, ok := m["outbounds"].([]any)
		if !ok {
			return nil, nativeError("outbounds")
		}
		var nodes []map[string]any
		for _, v := range list {
			o, ok := v.(map[string]any)
			if !ok {
				return nil, nativeError("outbounds")
			}
			if !in(asString(o["protocol"]), "freedom", "blackhole") {
				nodes = append(nodes, o)
			}
		}
		if len(nodes) != 1 {
			return nil, die("для ручного JSON-импорта нужен один сервер; список добавьте как подписку")
		}
		out = nodes[0]
		name = asString(m["remarks"])
		if name == "" {
			name = asString(out["tag"])
		}
	}
	r, e := parseNativeOutbound(out, name, id, source)
	if e != nil {
		return nil, e
	}
	if has(m, "outbounds") {
		addNativeImportWarning(r, "Из полного профиля импортирован только сервер. DNS, маршрутизация, inbound и API не применяются.")
	}
	return r, nil
}

func nativeStreamValid(r Record) *ParseError {
	if !basicRecordValid(r) {
		return nativeError("record")
	}
	if !in(asString(r["security"]), "none", "tls") {
		return nativeError("streamSettings.security")
	}
	st := map[string]any{"security": r["security"]}
	if has(r, "tls") {
		st["tlsSettings"] = r["tls"]
	}
	if !protectionValid(st) {
		return nativeError("streamSettings.tlsSettings")
	}
	if e := streamOptionsError(r); e != nil {
		return e
	}
	if has(r, "transport") {
		m, e := nativeObject(r["transport"], "transport", "finalMask")
		if e != nil {
			return e
		}
		if has(m, "finalMask") && !maskValid(m["finalMask"]) {
			return nativeError("streamSettings.finalmask")
		}
	}
	return nil
}

func nativeStream(r Record, block string) map[string]any {
	st := map[string]any{"network": r["network"], "security": r["security"], block + "Settings": r[block]}
	if has(r, "tls") {
		st["tlsSettings"] = r["tls"]
	}
	if has(r["transport"], "finalMask") {
		st["finalmask"] = get(r, "transport", "finalMask")
	}
	mergeStreamOptions(st, r)
	return st
}

func nativeInt(v any, max float64) bool {
	if j, ok := v.(json.Number); ok {
		if _, e := j.Int64(); e != nil {
			return false
		}
	}
	n, ok := isInt(v)
	return ok && n >= 0 && n <= max
}

func nativeXMux(v any, prefix string) *ParseError {
	m, e := nativeObject(v, prefix, "maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs", "hKeepAlivePeriod")
	if e != nil {
		return e
	}
	ends := map[string]int64{}
	for k, v := range m {
		if k == "hKeepAlivePeriod" {
			if !strictjson.SignedInt64(v) {
				return nativeError(prefix + "." + k)
			}
			continue
		}
		a, b, ok := nativeRange(v)
		if !ok || a < 0 || b > 2147483647 {
			return nativeError(prefix + "." + k)
		}
		ends[k] = b
	}
	if ends["maxConnections"] > 0 && ends["maxConcurrency"] > 0 {
		return nativeError(prefix + ".maxConnections.maxConcurrency")
	}
	return nil
}

func nativeRange(v any) (int64, int64, bool) {
	if n, ok := isInt(v); ok && n >= 0 && n <= 2147483647 {
		return int64(n), int64(n), true
	}
	s, ok := v.(string)
	if !ok {
		return 0, 0, false
	}
	p := strings.Split(s, "-")
	if len(p) == 1 {
		p = append(p, p[0])
	}
	if len(p) != 2 {
		return 0, 0, false
	}
	var ends [2]int64
	for i, x := range p {
		if !allDigits(x) {
			return 0, 0, false
		}
		n, e := json.Number(x).Int64()
		if e != nil || n > 2147483647 {
			return 0, 0, false
		}
		ends[i] = n
	}
	return ends[0], ends[1], ends[0] <= ends[1]
}
