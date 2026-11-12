package servers

import (
	"encoding/base64"
	"net"
	"net/netip"
	"strconv"
)

// Xray JSON has different defaults from wg-quick and Clash. Keep it structured
// so an explicit empty peer route list and the kernel-TUN preference survive.
func parseNativeWireGuard(out map[string]any, name, id string, source map[string]any) (Record, *ParseError) {
	if source == nil {
		source = map[string]any{"type": "manual"}
	}
	if has(out, "streamSettings") || has(out, "mux") {
		return nil, nativeError("wireguard.streamSettings.mux")
	}
	wg, e := nativeObject(out["settings"], "settings", "secretKey", "address", "peers", "mtu", "reserved", "remoteDNS", "noKernelTun")
	if e != nil {
		return nil, e
	}
	key, ok := wgKey(asString(wg["secretKey"]))
	if !ok {
		return nil, nativeError("settings.secretKey")
	}
	wg["secretKey"] = key
	// encoding/json accepts either a byte array or standard Base64 for []byte.
	// Store one representation so equivalent native exports have one identity.
	if encoded, ok := wg["reserved"].(string); ok {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, nativeError("settings.reserved")
		}
		values := make([]any, len(decoded))
		for i, value := range decoded {
			values[i] = int(value)
		}
		wg["reserved"] = values
	}
	if !has(wg, "noKernelTun") {
		wg["noKernelTun"] = false
	}
	if _, ok := wg["noKernelTun"].(bool); !ok {
		return nil, nativeError("settings.noKernelTun")
	}
	if !has(wg, "mtu") {
		wg["mtu"] = 1420
	} else {
		mtu, valid := freshSigned(wg["mtu"], 32)
		if !valid {
			return nil, nativeError("settings.mtu")
		}
		if mtu == 0 {
			wg["mtu"] = 1420
		}
	}
	if !has(wg, "address") {
		wg["address"] = []any{"10.0.0.1/32", "fd59:7153:2388:b5fd::1/128"}
	} else {
		addresses, valid := wg["address"].([]any)
		if !valid || addresses == nil {
			return nil, nativeError("settings.address")
		}
		for i, value := range addresses {
			text, valid := value.(string)
			if !valid {
				return nil, nativeError("settings.address")
			}
			if address, err := netip.ParseAddr(text); err == nil && address.Zone() == "" {
				addresses[i] = netip.PrefixFrom(address, address.BitLen()).String()
			}
		}
	}
	peers, ok := wg["peers"].([]any)
	if !ok || len(peers) < 1 || len(peers) > 32 {
		return nil, nativeError("settings.peers")
	}
	metadata := false
	for _, value := range peers {
		peer, e := nativeObject(value, "settings.peers", "publicKey", "preSharedKey", "endpoint", "allowedIPs", "keepAlive", "level", "email")
		if e != nil {
			return nil, e
		}
		for _, field := range []string{"publicKey", "preSharedKey"} {
			if field == "preSharedKey" && !has(peer, field) {
				continue
			}
			if field == "preSharedKey" && peer[field] == "" {
				delete(peer, field)
				continue
			}
			key, valid := wgKey(asString(peer[field]))
			if !valid {
				return nil, nativeError("settings.peers." + field)
			}
			peer[field] = key
		}
		if !has(peer, "allowedIPs") {
			peer["allowedIPs"] = []any{"0.0.0.0/0", "::/0"}
		}
		if has(peer, "level") {
			if _, valid := freshUnsigned(peer["level"], 32); !valid {
				return nil, nativeError("settings.peers.level")
			}
			metadata = true
		}
		if has(peer, "email") {
			if !textValue(peer["email"]) {
				return nil, nativeError("settings.peers.email")
			}
			metadata = true
		}
	}
	host, port, err := net.SplitHostPort(asString(peers[0].(map[string]any)["endpoint"]))
	if err != nil {
		return nil, nativeError("settings.peers.endpoint")
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return nil, nativeError("settings.peers.endpoint")
	}
	if name == "" {
		name = "WireGuard @ " + host
	}
	r := Record{"schemaVersion": 2, "id": id, "name": name, "source": cloneSource(source),
		"protocol": "wireguard", "network": "wireguard", "security": "wireguard", "address": host, "port": number, "wireguard": wg}
	options := map[string]any{}
	for _, field := range []string{"targetStrategy", "sendThrough"} {
		if has(out, field) {
			options[field] = out[field]
		}
	}
	if len(options) > 0 {
		r["outboundOptions"] = options
	}
	if metadata {
		addNativeImportWarning(r, "Поля WireGuard peer email и level сохранены. Xray использует их только в серверной роли; на клиентское соединение они не влияют. Политикой управляет BROray.")
	}
	if e := validate(r, "WireGuard JSON"); e != nil {
		return nil, e
	}
	return r, nil
}
