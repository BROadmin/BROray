package xrayschema

import (
	"reflect"
	"strings"
)

func (a *Assessment) detachPresent(m map[string]any, k, path, kind string) {
	if _, exists := m[k]; exists {
		a.Reasons = append(a.Reasons, Reason{kind, path + "." + k})
		delete(m, k)
	}
}

// Only the validation projection is normalized. Inactive/shadowed intent stays
// in Source and disables execution, including after a private backup restore.
// Structural checks cover all blocks before this step, not just the selected one.
func (a *Assessment) streamContext(m map[string]any, path string) error {
	if m == nil {
		return nil
	}
	// Preserve the import contract: an explicitly supplied method must name
	// a transport, rather than disappearing during alias normalization.
	if method, exists := m["method"]; exists && method == nil {
		return invalid(path + ".method")
	}
	network := "raw"
	if m["network"] != nil {
		var valid bool
		network, valid = canonicalTransport(textValue(m["network"]))
		if !valid {
			return invalid(path + ".network")
		}
	}
	if m["method"] != nil {
		method, valid := canonicalTransport(textValue(m["method"]))
		if !valid {
			return invalid(path + ".method")
		}
		if m["network"] != nil && network != method {
			a.Reasons = append(a.Reasons, Reason{"inactive-context", path + ".network"})
		}
		network = method
	}
	if m["network"] != nil || m["method"] != nil {
		m["network"] = network
		delete(m, "method")
	}
	for _, pair := range [][2]string{{"rawSettings", "tcpSettings"}, {"xhttpSettings", "splithttpSettings"}} {
		preferred, legacy := pair[0], pair[1]
		if m[preferred] != nil && m[legacy] != nil {
			if !reflect.DeepEqual(m[preferred], m[legacy]) {
				a.Reasons = append(a.Reasons, Reason{"inactive-context", path + "." + legacy})
			}
			delete(m, legacy)
		}
	}
	blocks := map[string]string{
		"rawSettings": "raw", "tcpSettings": "raw", "xhttpSettings": "xhttp", "splithttpSettings": "xhttp",
		"wsSettings": "ws", "grpcSettings": "grpc", "httpupgradeSettings": "httpupgrade", "kcpSettings": "kcp",
		"hysteriaSettings": "hysteria", "masqueSettings": "masque", "xdriveSettings": "xdrive",
	}
	for key, applies := range blocks {
		if applies != network {
			if active(m[key]) {
				a.Reasons = append(a.Reasons, Reason{"inactive-context", path + "." + key})
			}
			delete(m, key)
		}
	}
	security := strings.ToLower(textValue(m["security"]))
	for key, applies := range map[string]string{"tlsSettings": "tls", "realitySettings": "reality"} {
		if security != applies {
			if active(m[key]) {
				a.Reasons = append(a.Reasons, Reason{"inactive-context", path + "." + key})
			}
			delete(m, key)
		}
	}
	return nil
}

func canonicalTransport(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "raw", "tcp":
		return "raw", true
	case "xhttp", "splithttp":
		return "xhttp", true
	case "ws", "websocket":
		return "ws", true
	case "kcp", "mkcp":
		return "kcp", true
	case "grpc", "httpupgrade", "hysteria", "masque", "xdrive":
		return strings.ToLower(s), true
	}
	return "", false
}
