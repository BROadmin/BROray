package xrayimport

import (
	"fmt"
	"net/netip"
	"strings"
)

func wireGuardOutbound(profile, out map[string]any, index int, multiple bool) []string {
	need(keysOnly(out, "protocol", "tag", "settings"))
	w := obj(out["settings"])
	need(keysOnly(w, "secretKey", "address", "peers", "mtu", "reserved", "remoteDNS", "noKernelTun"))
	if v, ok := w["noKernelTun"]; ok {
		need(v == true)
	}
	var b strings.Builder
	line := func(k, v string) { b.WriteString(k + " = " + v + "\n") }
	list := func(v any, ips bool) string {
		arr := listOf(v)
		vals := []string{}
		for _, a := range arr {
			need(textValue(a))
			s := a.(string)
			need(!strings.Contains(s, ","))
			if ips && !strings.Contains(s, "/") {
				ip, e := netip.ParseAddr(s)
				need(e == nil)
				if ip.Is6() {
					s += "/128"
				} else {
					s += "/32"
				}
			}
			vals = append(vals, s)
		}
		return strings.Join(vals, ", ")
	}
	b.WriteString("# BROray name: " + outboundName(profile, out, index, multiple) + "\n[Interface]\n")
	line("PrivateKey", stringField(w, "secretKey", ""))
	line("Address", list(w["address"], true))
	if v, ok := w["mtu"]; ok {
		need(isInt(v, 576, 9000))
		line("MTU", numText(v))
	}
	if v, ok := w["remoteDNS"]; ok {
		arr, valid := v.([]any)
		need(valid)
		if len(arr) > 0 {
			line("DNS", list(v, false))
		}
	}
	if v, ok := w["reserved"]; ok {
		arr, valid := v.([]any)
		need(valid && (len(arr) == 0 || len(arr) == 3))
		var nums []string
		for _, n := range arr {
			need(isInt(n, 0, 255))
			nums = append(nums, numText(n))
		}
		if len(nums) > 0 {
			line("Reserved", strings.Join(nums, ", "))
		}
	}
	for _, v := range listOf(w["peers"]) {
		p := obj(v)
		need(keysOnly(p, "publicKey", "preSharedKey", "endpoint", "allowedIPs", "keepAlive", "level", "email"))
		endpointMetadata(p)
		b.WriteString("[Peer]\n")
		line("PublicKey", stringField(p, "publicKey", ""))
		line("Endpoint", stringField(p, "endpoint", ""))
		if v := stringField(p, "preSharedKey", ""); v != "" {
			line("PresharedKey", v)
		}
		if v, ok := p["allowedIPs"]; ok {
			line("AllowedIPs", list(v, false))
		} else {
			line("AllowedIPs", "0.0.0.0/0, ::/0")
		}
		if v, ok := p["keepAlive"]; ok {
			need(isInt(v, 0, 65535))
			line("PersistentKeepalive", numText(v))
		}
	}
	return []string{b.String()}
}

const hysteriaMasqueradeWarning = "Hysteria2: поле masquerade относится к серверу и не применяется в клиентском подключении."

// ValidateHysteriaMasqueradeMetadata validates inert metadata without opening
// any referenced file or URL. Error text never contains provider values.
func ValidateHysteriaMasqueradeMetadata(v any) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("invalid hysteria masquerade metadata")
		}
	}()
	validateHysteriaMasquerade(v)
	return nil
}

// Xray v26.11.9 consumes masquerade only in the Hysteria listener. Check the
// known JSON shape before omitting it; never open its URL or directory.
func validateHysteriaMasquerade(v any) {
	m := obj(v)
	need(keysOnly(m, "type", "dir", "url", "content", "headers", "insecure", "rewriteHost", "statusCode", "xForwarded"))
	need(inList(stringField(m, "type", ""), "", "file", "proxy", "string"))
	for _, key := range []string{"dir", "url", "content"} {
		if v, exists := m[key]; exists {
			_, ok := v.(string)
			need(ok)
		}
	}
	for _, key := range []string{"insecure", "rewriteHost", "xForwarded"} {
		if v, exists := m[key]; exists {
			_, ok := v.(bool)
			need(ok)
		}
	}
	if v, exists := m["headers"]; exists {
		for _, value := range obj(v) {
			_, ok := value.(string)
			need(ok)
		}
	}
	if v, exists := m["statusCode"]; exists {
		need(isInt(v, -2147483648, 2147483647))
	}
}

func (c *converter) hysteriaOutbound(profile, out map[string]any, index int, multiple bool) []string {
	need(keysOnly(out, "protocol", "tag", "settings", "streamSettings"))
	s := obj(out["settings"])
	need(keysOnly(s, "version", "address", "port") && s["version"] != nil && isInt(s["version"], 2, 2) && isInt(s["port"], 1, 65535))
	stream := objectField(out, "streamSettings")
	need(keysOnly(stream, "network", "security", "tlsSettings", "hysteriaSettings", "finalmask", "sockopt") && stream["network"] == "hysteria" && stream["security"] == "tls")
	h := objectField(stream, "hysteriaSettings")
	need(keysOnly(h, "version", "auth", "udpIdleTimeout", "masquerade") && isInt(h["version"], 2, 2))
	if v, exists := h["masquerade"]; exists {
		validateHysteriaMasquerade(v)
	}
	auth := stringField(h, "auth", "")
	params := protection(stream)
	if has(stream, "sockopt") {
		opts := obj(stream["sockopt"])
		extra := map[string]any{"sockopt": opts}
		need(c.checks.ValidateStreamOptions("hysteria", extra) == nil)
		params = append(params, pair("streamOptions", toJSON(extra)))
	}
	params = append(params, pair("fm", toJSON(objectField(stream, "finalmask"))))
	if v, ok := h["udpIdleTimeout"]; ok {
		need(isInt(v, 0, 2147483647))
		params = append(params, pair("udpIdleTimeout", numText(v)))
	}
	return []string{"hy2://" + uriEscape(auth) + "@" + addressURI(s["address"]) + ":" + numText(s["port"]) + "?" + strings.Join(params, "&") + "#" + uriEscape(outboundName(profile, out, index, multiple))}
}
