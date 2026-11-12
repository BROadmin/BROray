package xrayimport

import "strings"

func (c *converter) proxyOutbound(profile, out map[string]any, index int, multiple bool) []string {
	need(keysOnly(out, "protocol", "tag", "settings", "streamSettings"))
	settings := obj(out["settings"])
	s := settings
	if has(settings, "servers") {
		need(keysOnly(settings, "servers"))
		arr := listOf(settings["servers"])
		need(len(arr) == 1)
		s = obj(arr[0])
		need(keysOnly(s, "address", "port", "users"))
	} else {
		need(keysOnly(s, "address", "port", "user", "pass", "level", "email"))
		endpointMetadata(s)
	}
	user, password := "", ""
	if has(s, "users") {
		arr, ok := s["users"].([]any)
		need(ok && len(arr) <= 1)
		if len(arr) == 1 {
			u := obj(arr[0])
			need(keysOnly(u, "user", "pass", "level", "email"))
			endpointMetadata(u)
			user = stringField(u, "user", "")
			password = stringField(u, "pass", "")
		}
	} else {
		user = stringField(s, "user", "")
		password = stringField(s, "pass", "")
	}
	need(user != "" || password == "")
	stream := objectField(out, "streamSettings")
	params := append(append(c.transport(stream), protection(stream)...), pair("fm", toJSON(objectField(stream, "finalmask"))))
	name := outboundName(profile, out, index, multiple)
	need(isInt(s["port"], 1, 65535))
	scheme := out["protocol"].(string)
	credential := ""
	if user != "" {
		credential = uriEscape(user) + ":" + uriEscape(password) + "@"
	}
	return []string{scheme + "://" + credential + addressURI(s["address"]) + ":" + numText(s["port"]) + "?" + strings.Join(params, "&") + "#" + uriEscape(name)}
}
