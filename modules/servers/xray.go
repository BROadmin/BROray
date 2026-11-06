package servers

import (
	"encoding/json"
	"strings"
)

// Outbound renders the Xray outbound object for a server, byte-for-byte in the
// shape BROray 3.x's server-config-generator.sh produced (tag "proxy").
func Outbound(s Server) map[string]any {
	stream := streamSettings(s)
	switch s.Protocol {
	case "vless":
		user := map[string]any{"id": s.UUID, "encryption": orDefault(s.Encryption, "none")}
		if s.Flow != "" {
			user["flow"] = s.Flow
		}
		return map[string]any{"tag": "proxy", "protocol": "vless",
			"settings":       map[string]any{"vnext": []any{map[string]any{"address": s.Address, "port": s.Port, "users": []any{user}}}},
			"streamSettings": stream}
	case "vmess":
		user := map[string]any{"id": s.UUID, "alterId": s.AlterID, "security": orDefault(s.Encryption, "auto")}
		return map[string]any{"tag": "proxy", "protocol": "vmess",
			"settings":       map[string]any{"vnext": []any{map[string]any{"address": s.Address, "port": s.Port, "users": []any{user}}}},
			"streamSettings": stream}
	case "trojan":
		return map[string]any{"tag": "proxy", "protocol": "trojan",
			"settings":       map[string]any{"servers": []any{map[string]any{"address": s.Address, "port": s.Port, "password": s.Password}}},
			"streamSettings": stream}
	case "shadowsocks":
		return map[string]any{"tag": "proxy", "protocol": "shadowsocks",
			"settings": map[string]any{"servers": []any{map[string]any{"address": s.Address, "port": s.Port, "method": s.Method, "password": s.Password}}}}
	case "hysteria2":
		settings := map[string]any{"version": 2, "address": s.Address, "port": s.Port}
		if s.Hysteria.ObfsPassword != "" {
			settings["obfsPassword"] = s.Hysteria.ObfsPassword
		}
		return map[string]any{"tag": "proxy", "protocol": "hysteria", "settings": settings, "streamSettings": stream}
	}
	return nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func compact(m map[string]any) map[string]any {
	for k, v := range m {
		switch x := v.(type) {
		case string:
			if x == "" {
				delete(m, k)
			}
		case []string:
			if len(x) == 0 {
				delete(m, k)
			}
		case nil:
			delete(m, k)
		}
	}
	return m
}

func streamSettings(s Server) map[string]any {
	st := map[string]any{"network": s.Network, "security": s.Security}
	t := s.Transport
	var extra map[string]any
	if len(t.Extra) > 0 {
		_ = json.Unmarshal(t.Extra, &extra)
	}
	switch s.Network {
	case "xhttp":
		x := map[string]any{"path": orDefault(t.Path, "/"), "mode": orDefault(t.Mode, "auto")}
		if t.Host != "" {
			x["host"] = t.Host
		}
		if len(extra) > 0 {
			x["extra"] = extra
		}
		st["xhttpSettings"] = x
	case "raw":
		header := map[string]any{"type": orDefault(t.HeaderType, "none")}
		if t.HeaderType == "http" {
			req := map[string]any{"path": []string{orDefault(t.Path, "/")}}
			if t.Host != "" {
				req["headers"] = map[string]any{"Host": strings.Split(t.Host, ",")}
			}
			header["request"] = req
		}
		st["rawSettings"] = map[string]any{"header": header}
	case "ws":
		w := map[string]any{"path": orDefault(t.Path, "/")}
		if t.Host != "" {
			w["host"] = t.Host
		}
		st["wsSettings"] = w
	case "grpc":
		st["grpcSettings"] = compact(map[string]any{"serviceName": t.ServiceName, "authority": t.Host, "multiMode": t.Mode == "multi"})
	case "httpupgrade":
		st["httpupgradeSettings"] = compact(map[string]any{"path": orDefault(t.Path, "/"), "host": t.Host})
	case "kcp":
		st["kcpSettings"] = map[string]any{}
	case "hysteria":
		st["hysteriaSettings"] = map[string]any{"version": 2, "auth": s.Auth}
	}
	switch s.Security {
	case "tls":
		alpn := s.TLS.ALPN
		tls := map[string]any{"serverName": s.TLS.ServerName, "fingerprint": orDefault(s.TLS.Fingerprint, "chrome")}
		if len(alpn) > 0 {
			tls["alpn"] = alpn
		}
		st["tlsSettings"] = compact(tls)
	case "reality":
		st["realitySettings"] = compact(map[string]any{"serverName": s.Reality.ServerName, "fingerprint": orDefault(s.Reality.Fingerprint, "chrome"), "publicKey": s.Reality.PublicKey, "shortId": s.Reality.ShortID, "spiderX": s.Reality.SpiderX})
	}
	if s.Network == "hysteria" {
		var udp []any
		if s.Hysteria.Obfs != "" {
			os := map[string]any{"password": s.Hysteria.ObfsPassword}
			if s.Hysteria.Obfs == "gecko" {
				os["packetSize"] = "512-1200"
			}
			udp = append(udp, map[string]any{"type": "salamander", "settings": os})
		}
		if s.Hysteria.Ports != "" {
			udp = append(udp, map[string]any{"type": "udphop", "settings": map[string]any{"mode": "intervalremote", "interval": 30, "remotePorts": s.Hysteria.Ports}})
		}
		if len(udp) > 0 {
			st["finalmask"] = map[string]any{"udp": udp}
		}
	}
	return st
}

// XrayConfig renders the full Xray configuration: one SOCKS inbound on
// listen:port (noauth, udp) and the server's outbound, like 3.x.
func XrayConfig(s Server, listen string, port int, logLevel, accessLog, errorLog string) map[string]any {
	inbound := map[string]any{"tag": "socks", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}}
	if listen != "" {
		inbound["listen"] = listen
	}
	return map[string]any{
		"log":       map[string]any{"access": accessLog, "error": errorLog, "loglevel": orDefault(logLevel, "warning")},
		"inbounds":  []any{inbound},
		"outbounds": []any{Outbound(s)},
	}
}
