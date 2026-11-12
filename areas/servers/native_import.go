package servers

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/internal/xrayimport"
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
	if err := nativeKeys(out, "outbound", "protocol", "tag", "settings", "streamSettings", "mux", "targetStrategy", "sendThrough"); err != nil {
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
	if proto == "wireguard" {
		return parseNativeWireGuard(out, name, id, source)
	}
	st, err := nativeObject(out["streamSettings"], "streamSettings", "network", "method", "address", "port", "security", "tlsSettings", "xdriveSettings", "masqueSettings", "sockopt", "finalmask")
	if err != nil {
		return nil, err
	}
	if has(st, "method") {
		if !textValue(st["method"]) || has(st, "network") && !textValue(st["network"]) {
			return nil, nativeError("streamSettings.method")
		}
		st["network"] = st["method"]
	}
	network := strings.ToLower(asString(st["network"]))
	if !(proto == "vless" && network == "xdrive" || proto == "masque" && network == "masque") {
		return nil, nativeError("protocol.network")
	}
	if source == nil {
		source = map[string]any{"type": "manual"}
	}
	security := "none"
	if has(st, "security") {
		if !textValue(st["security"]) {
			return nil, nativeError("streamSettings.security")
		}
		security = strings.ToLower(asString(st["security"]))
		if security == "" {
			security = "none"
		}
	}
	r := Record{"schemaVersion": 2, "id": id, "name": name, "source": cloneSource(source), "protocol": proto, "network": network, "security": security}
	endpoint := map[string]any{}
	for _, key := range []string{"address", "port"} {
		if has(st, key) {
			endpoint[key] = st[key]
		}
	}
	if len(endpoint) > 0 {
		r["streamEndpoint"] = endpoint
	}
	options := map[string]any{}
	for _, key := range []string{"targetStrategy", "sendThrough"} {
		if has(out, key) {
			options[key] = out[key]
		}
	}
	if len(options) > 0 {
		r["outboundOptions"] = options
	}
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
	} else if network == "xdrive" {
		// Some client exports omit the MUX required by Xray XDRIVE. This is
		// an explicit import compatibility policy, not relaxed validation.
		r["mux"] = map[string]any{"enabled": true}
		addNativeImportWarning(r, "В экспорте XDRIVE отсутствует mux. Для обязательного клиентского MUX BROray добавляет mux.enabled=true; применяется локальная политика мультиплексирования.")
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
		if has(user, "level") {
			level, ok := isInt(user["level"])
			if !ok || level < 0 || level > 4294967295 {
				return nil, nativeError("settings.vnext.users.level")
			}
		}
		if has(user, "email") && !textValue(user["email"]) {
			return nil, nativeError("settings.vnext.users.email")
		}
		if has(user, "email") {
			r["accountEmail"] = user["email"]
		}
		if has(user, "level") {
			r["accountLevel"] = user["level"]
			addNativeImportWarning(r, clientAccountLevelWarning)
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
	inputs, err := xrayimport.DecodeJSON([]byte(text), 1, xrayimport.Checks{
		VlessEncryptionValid:  VlessEncryptionValid,
		ValidateStreamOptions: ValidateStreamOptions,
		ValidateClientFields:  ValidateClientFields,
	})
	if err != nil {
		return nil, nativeError("document")
	}
	if len(inputs.Inputs) != 1 {
		return nil, die("для ручного JSON-импорта нужен один сервер; список добавьте как подписку")
	}
	input := inputs.Inputs[0]
	if input.Rejected {
		return nil, nativeError(orStr(input.RejectionField, "outbound"))
	}
	var rec Record
	var parseErr *ParseError
	if input.NativeOutbound != nil {
		rec, parseErr = parseNativeOutbound(input.NativeOutbound, input.Name, id, source)
	} else {
		rec, parseErr = parseLink(input.Link, id, source)
	}
	if parseErr != nil {
		return nil, parseErr
	}
	if len(input.ClientFields) > 0 {
		var applyErr error
		rec, applyErr = ApplyClientFields(rec, input.ClientFields)
		if applyErr != nil {
			if e, ok := applyErr.(*ParseError); ok {
				return nil, e
			}
			return nil, nativeError("outbound")
		}
	}
	if input.UnavailableSource != nil {
		wrapped, wrapErr := WrapUnavailableXray(rec, input.UnavailableSource)
		if wrapErr != nil {
			return nil, unavailableProfileError()
		}
		rec = wrapped
	}
	if input.NativeOutbound == nil {
		addNativeImportWarning(rec, xrayimport.JSONWarning)
	}
	if input.FromProfile {
		addNativeImportWarning(rec, xrayimport.ProfileWarning)
	}
	for _, warning := range input.Warnings {
		addNativeImportWarning(rec, warning)
	}
	return rec, nil
}

// parseInputEnvelope only unwraps recognized document forms and one Base64
// layer. URI and INI parsing retain their existing final validation path.
func parseInputEnvelope(text, id string, source map[string]any) (Record, bool, *ParseError) {
	if isWireGuardInput(text) {
		if amnezia.HasNativeINIParameters(text) {
			if source == nil {
				source = map[string]any{"type": "manual"}
			}
			// Reuse the established private native-profile importer without
			// changing BOM, comments, line endings or the original INI values.
			rec, err := parseAmnezia("vpn://"+base64.RawURLEncoding.EncodeToString([]byte(text)), id, source)
			return rec, true, err
		}
		return nil, false, nil
	}
	if rec, recognized, err := parseAmneziaDocument(text, id, source); recognized {
		return rec, true, err
	}
	if rec, recognized, err := parseClashDocument(text, id, source); recognized {
		return rec, true, err
	}
	if xrayimport.LooksJSON([]byte(text)) {
		rec, err := parseNativeDocument(text, id, source)
		return rec, true, err
	}
	if strings.Contains(text, "://") {
		// A URI is one line. Otherwise a second link can be swallowed by
		// the first link's fragment instead of being counted as another node.
		if strings.ContainsAny(text, "\r\n") {
			return nil, true, die("для ручного импорта нужен один сервер; несколько ссылок передайте отдельно")
		}
		return nil, false, nil
	}
	decoded, ok := xrayimport.DecodeBase64([]byte(text))
	if !ok {
		return nil, false, nil
	}
	if isWireGuardInput(string(decoded)) {
		rec, err := parseLink(string(decoded), id, source)
		return rec, true, err
	}
	if rec, recognized, err := parseAmneziaDocument(string(decoded), id, source); recognized {
		return rec, true, err
	}
	if rec, recognized, err := parseClashDocument(string(decoded), id, source); recognized {
		return rec, true, err
	}
	if xrayimport.LooksJSON(decoded) {
		rec, err := parseNativeDocument(string(decoded), id, source)
		return rec, true, err
	}
	plain := strings.TrimSpace(string(xrayimport.StripBOM(decoded)))
	if xrayimport.IsShareLink(plain) {
		rec, err := parseLink(plain, id, source)
		return rec, true, err
	}
	return nil, true, nativeError("document")
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
