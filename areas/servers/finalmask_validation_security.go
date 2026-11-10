package servers

import (
	"encoding/hex"
	"strings"
)

func freshQuic(v any, path string, depth int) *ParseError {
	fields := freshFields("congestion bbrProfile brutalUp brutalDown", freshString)
	freshAdd(fields, "debug brutalDisableLossCompensation disablePathMTUDiscovery disableChromeParrot disableGSO disableStatelessReset", freshBool)
	freshAdd(fields, "initStreamReceiveWindow maxStreamReceiveWindow initConnectionReceiveWindow maxConnectionReceiveWindow", freshInt(64, false))
	freshAdd(fields, "maxIdleTimeout keepAlivePeriod maxIncomingStreams", freshInt(64, true))
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	if !in(strings.ToLower(asString(m["bbrProfile"])), "", "conservative", "standard", "aggressive") {
		return nativeError(path + ".bbrProfile")
	}
	var up uint64
	for _, key := range []string{"brutalUp", "brutalDown"} {
		if !has(m, key) {
			continue
		}
		n, ok := freshBandwidth(m[key])
		if !ok || (n > 0 && n < 65536) {
			return nativeError(path + "." + key)
		}
		if key == "brutalUp" {
			up = n
		}
	}
	congestion := strings.ToLower(asString(m["congestion"]))
	if !in(congestion, "", "brutal", "reno", "bbr", "force-brutal") || (congestion == "force-brutal" && up == 0) {
		return nativeError(path + ".congestion")
	}
	for _, key := range []string{"initStreamReceiveWindow", "maxStreamReceiveWindow", "initConnectionReceiveWindow", "maxConnectionReceiveWindow"} {
		n, _ := freshUnsigned(m[key], 64)
		if n > 0 && n < 16384 {
			return nativeError(path + "." + key)
		}
	}
	for _, item := range []struct {
		key      string
		min, max int64
	}{{"maxIdleTimeout", 4, 120}, {"keepAlivePeriod", 2, 60}, {"maxIncomingStreams", 8, 0}} {
		n, _ := freshSigned(m[item.key], 64)
		if n != 0 && (n < item.min || (item.max > 0 && n > item.max)) {
			return nativeError(path + "." + item.key)
		}
	}
	return nil
}

// Nonempty filesystem/script paths and caller-owned routing controls are outside
// the fresh-import contract. Neutral values remain valid and are not rewritten.
func freshEmptyString(v any, path string, _ int) *ParseError {
	s, ok := v.(string)
	if !ok || s != "" {
		return nativeError(path)
	}
	return nil
}

func freshFalse(v any, path string, _ int) *ParseError {
	b, ok := v.(bool)
	if !ok || b {
		return nativeError(path)
	}
	return nil
}

func freshEmptyArray(v any, path string, _ int) *ParseError {
	a, ok := v.([]any)
	if !ok || a == nil || len(a) != 0 {
		return nativeError(path)
	}
	return nil
}

func freshStringList(v any, path string, depth int) *ParseError {
	if _, ok := v.(string); ok {
		return nil
	}
	return freshArray(freshString)(v, path, depth)
}

func freshTLS(v any, path string, depth int) *ParseError {
	fields := freshFields("serverName minVersion maxVersion cipherSuites fingerprint pinnedPeerCertSha256 verifyPeerCertByName echConfigList", freshString)
	freshAdd(fields, "enableSessionResumption disableSystemRoot useSystemCA", freshBool)
	freshAdd(fields, "allowInsecure rejectUnknownSni", freshFalse)
	freshAdd(fields, "masterKeyLog echServerKeys", freshEmptyString)
	freshAdd(fields, "alpn curvePreferences", freshStringList)
	fields["certificates"] = freshArray(freshTLSCert)
	fields["echSockopt"] = freshSocket
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	for _, pin := range strings.Split(asString(m["pinnedPeerCertSha256"]), ",") {
		pin = strings.TrimSpace(pin)
		if pin == "" {
			continue
		}
		b, err := hex.DecodeString(strings.ReplaceAll(pin, ":", ""))
		if err != nil || len(b) != 32 {
			return nativeError(path + ".pinnedPeerCertSha256")
		}
	}
	return nil
}

func freshTLSCert(v any, path string, depth int) *ParseError {
	fields := freshFields("certificateFile keyFile", freshEmptyString)
	freshAdd(fields, "certificate key", freshArray(freshString))
	freshAdd(fields, "oneTimeLoading buildChain", freshBool)
	fields["usage"] = freshString
	fields["ocspStapling"] = freshInt(64, false)
	_, e := freshObject(v, path, depth, fields)
	return e
}

func freshSocket(v any, path string, depth int) *ParseError {
	fields := freshFields("tcpKeepAliveInterval tcpKeepAliveIdle tcpWindowClamp tcpMaxSeg tcpUserTimeout", freshInt(32, true))
	freshAdd(fields, "domainStrategy tcpCongestion addressPortStrategy", freshString)
	freshAdd(fields, "tcpMptcp", freshBool)
	freshAdd(fields, "acceptProxyProtocol penetrate v6only", freshFalse)
	freshAdd(fields, "dialerProxy interface", freshEmptyString)
	freshAdd(fields, "customSockopt trustedXForwardedFor", freshEmptyArray)
	fields["mark"] = func(v any, p string, _ int) *ParseError {
		n, ok := freshSigned(v, 32)
		if !ok || n != 0 {
			return nativeError(p)
		}
		return nil
	}
	fields["tproxy"] = func(v any, p string, _ int) *ParseError {
		s, ok := v.(string)
		if !ok || !in(strings.ToLower(s), "", "off") {
			return nativeError(p)
		}
		return nil
	}
	fields["tcpFastOpen"] = func(v any, p string, d int) *ParseError {
		if _, ok := v.(bool); ok {
			return nil
		}
		return freshInt(32, true)(v, p, d)
	}
	fields["happyEyeballs"] = func(v any, p string, d int) *ParseError {
		f := freshFields("interleave maxConcurrentTry", freshInt(32, false))
		f["tryDelayMs"] = freshInt(64, false)
		f["prioritizeIPv6"] = freshBool
		_, e := freshObject(v, p, d, f)
		return e
	}
	m, e := freshObject(v, path, depth, fields)
	if e != nil {
		return e
	}
	if !in(strings.ToLower(asString(m["domainStrategy"])), "", "asis", "useip", "useipv4", "useipv6", "useipv4v6", "useipv6v4", "forceip", "forceipv4", "forceipv6", "forceipv4v6", "forceipv6v4") {
		return nativeError(path + ".domainStrategy")
	}
	if !in(strings.ToLower(asString(m["addressPortStrategy"])), "", "none", "srvportonly", "srvaddressonly", "srvportandaddress", "txtportonly", "txtaddressonly", "txtportandaddress") {
		return nativeError(path + ".addressPortStrategy")
	}
	return nil
}
