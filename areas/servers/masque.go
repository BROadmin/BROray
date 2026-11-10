package servers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/netip"
	"net/url"
	"strings"
)

func validateMasque(r Record) *ParseError {
	if e := nativeStreamValid(r); e != nil {
		return e
	}
	if r["protocol"] != "masque" || r["network"] != "masque" || r["security"] != "tls" {
		return nativeError("protocol.network.security")
	}
	if has(r, "mux") {
		return nativeError("mux")
	}
	if has(r, "remoteDNS") {
		a, ok := r["remoteDNS"].([]any)
		if !ok || len(a) > 64 {
			return nativeError("settings.remoteDNS")
		}
		for _, v := range a {
			ip, e := netip.ParseAddr(asString(v))
			if e != nil || ip.Zone() != "" {
				return nativeError("settings.remoteDNS")
			}
		}
	}
	m, e := nativeObject(r["masque"], "masqueSettings", "host", "path", "user", "pass", "headers", "warp", "xmux")
	if e != nil {
		return e
	}
	for _, k := range []string{"host", "path", "user", "pass"} {
		if has(m, k) && !textValue(m[k]) {
			return nativeError("masqueSettings." + k)
		}
	}
	if host := asString(m["host"]); host != "" {
		u, e := url.Parse("https://" + host)
		if e != nil || u.Host != host || u.User != nil {
			return nativeError("masqueSettings.host")
		}
	}
	if p := asString(m["path"]); p != "" {
		p = strings.NewReplacer("{target}", "*", "{ipproto}", "*", "{?target,ipproto}", "?target=*&ipproto=*", "{?ipproto,target}", "?ipproto=*&target=*", "{&target,ipproto}", "&target=*&ipproto=*", "{&ipproto,target}", "&ipproto=*&target=*").Replace(p)
		if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "{}") {
			return nativeError("masqueSettings.path")
		}
	}
	if strings.Contains(asString(m["user"]), ":") {
		return nativeError("masqueSettings.user")
	}
	if has(m, "headers") {
		if !stringMapValid(m["headers"], true) {
			return nativeError("masqueSettings.headers")
		}
		for k := range m["headers"].(map[string]any) {
			switch strings.ToLower(k) {
			case "host", "capsule-protocol":
				return nativeError("masqueSettings.headers." + k)
			case "authorization":
				if asString(m["user"]) != "" || asString(m["pass"]) != "" {
					return nativeError("masqueSettings.headers.Authorization")
				}
			}
		}
	}
	if has(m, "warp") {
		if asString(m["user"]) != "" || asString(m["pass"]) != "" {
			return nativeError("masqueSettings.warp.user.pass")
		}
		if e := validateMasqueWarp(m["warp"]); e != nil {
			return e
		}
	}
	if has(m, "xmux") {
		if e := nativeXMux(m["xmux"], "masqueSettings.xmux"); e != nil {
			return e
		}
	}
	return nil
}

func warpKey(v any) []byte {
	s, ok := v.(string)
	if !ok || len(s) > 16384 {
		return nil
	}
	s = strings.TrimSpace(s)
	if p, _ := pem.Decode([]byte(s)); p != nil {
		return p.Bytes
	}
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil {
		return nil
	}
	return b
}

func validateMasqueWarp(v any) *ParseError {
	w, e := nativeObject(v, "masqueSettings.warp", "privateKey", "publicKey", "address")
	if e != nil {
		return e
	}
	der := warpKey(w["privateKey"])
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		key, err = x509.ParseECPrivateKey(der)
	}
	ec, ok := key.(*ecdsa.PrivateKey)
	if err != nil || !ok || ec.Curve != elliptic.P256() {
		return nativeError("masqueSettings.warp.privateKey")
	}
	if _, e := x509.ParsePKIXPublicKey(warpKey(w["publicKey"])); e != nil {
		return nativeError("masqueSettings.warp.publicKey")
	}
	addresses, ok := w["address"].([]any)
	if !ok || len(addresses) < 1 || len(addresses) > 2 {
		return nativeError("masqueSettings.warp.address")
	}
	seen := map[bool]bool{}
	for _, v := range addresses {
		a, err := netip.ParseAddr(asString(v))
		if err != nil {
			p, e := netip.ParsePrefix(asString(v))
			if e != nil {
				return nativeError("masqueSettings.warp.address")
			}
			a = p.Addr()
		}
		if a.Zone() != "" || seen[a.Is4()] {
			return nativeError("masqueSettings.warp.address")
		}
		seen[a.Is4()] = true
	}
	return nil
}

func masqueSettings(r Record) map[string]any { m, _ := r["masque"].(map[string]any); return m }
