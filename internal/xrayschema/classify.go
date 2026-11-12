package xrayschema

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

type Reason struct{ Kind, Path string }
type Assessment struct {
	Source     *Node
	Projection map[string]any
	Reasons    []Reason
}

// Classify supplies a non-executable validation projection. The caller MUST
// validate it through the normal endpoint converter; it must never connect
// using that projection when Reasons is nonempty.
func Classify(input map[string]any) (*Assessment, error) {
	n, err := Parse(input)
	if err != nil {
		return nil, err
	}
	a := &Assessment{Source: n, Projection: n.KnownFields}
	for _, p := range n.UnknownPaths {
		a.Reasons = append(a.Reasons, Reason{"unknown-extension", p})
	}
	if err = a.outbound(a.Projection, "Xray"); err != nil {
		return nil, err
	}
	sort.Slice(a.Reasons, func(i, j int) bool {
		if a.Reasons[i].Path == a.Reasons[j].Path {
			return a.Reasons[i].Kind < a.Reasons[j].Kind
		}
		return a.Reasons[i].Path < a.Reasons[j].Path
	})
	return a, nil
}

func objectValue(v any) map[string]any { m, _ := v.(map[string]any); return m }
func textValue(v any) string           { s, _ := v.(string); return s }
func active(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case json.Number:
		return x != "0"
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
func (a *Assessment) detach(m map[string]any, k, path, kind string) {
	if active(m[k]) {
		a.Reasons = append(a.Reasons, Reason{kind, path + "." + k})
		delete(m, k)
	}
}
func (a *Assessment) outbound(m map[string]any, path string) error {
	if _, ok := m["proxySettings"]; ok {
		a.Reasons = append(a.Reasons, Reason{"core-removed", path + ".proxySettings"})
		delete(m, "proxySettings")
	}
	if s := textValue(m["sendThrough"]); s == "origin" || s == "srcip" {
		a.detach(m, "sendThrough", path, "local-control")
	}
	s := objectValue(m["settings"])
	if s != nil {
		if m["protocol"] == "vless" {
			for _, field := range []string{"seed", "testseed"} {
				a.detachPresent(s, field, path+".settings", "core-ineffective")
			}
		}
		if reverse := objectValue(s["reverse"]); reverse != nil {
			if strings.TrimSpace(textValue(reverse["tag"])) == "" {
				return invalid(path + ".settings.reverse.tag")
			}
			a.detach(s, "reverse", path+".settings", "profile-dependency")
		}
		if strings.HasPrefix(textValue(s["address"]), "env:") {
			a.Reasons = append(a.Reasons, Reason{"local-resource", path + ".settings.address"})
			s["address"] = "unresolved-import.invalid"
		}
		// Account levels remain valid with BROray's local DefaultManager;
		// they do not require importing a provider's root policy.
		for _, group := range []string{"servers", "vnext"} {
			items, _ := s[group].([]any)
			for _, item := range items {
				endpoint := objectValue(item)
				ep := path + ".settings." + group + "[]"
				users, _ := endpoint["users"].([]any)
				for _, u := range users {
					user := objectValue(u)
					if err := validateRawUserMetadata(user, ep+".users[]"); err != nil {
						return err
					}
					if m["protocol"] == "vless" {
						a.detachPresent(user, "testseed", ep+".users[]", "core-ineffective")
					}
					if reverse := objectValue(user["reverse"]); reverse != nil {
						if strings.TrimSpace(textValue(reverse["tag"])) == "" {
							return invalid(ep + ".users[].reverse.tag")
						}
						a.detach(user, "reverse", ep+".users[]", "profile-dependency")
					}
				}
			}
		}
	}
	return a.stream(objectValue(m["streamSettings"]), path+".streamSettings")
}

// HTTP/SOCKS users are upstream json.RawMessage values, so their account
// metadata needs explicit validation in addition to the enclosing schema.
func validateRawUserMetadata(user map[string]any, path string) error {
	if user == nil {
		return invalid(path)
	}
	if value, exists := user["level"]; exists {
		text, ok := number(value)
		if !ok {
			return invalid(path + ".level")
		}
		if _, err := strconv.ParseUint(text, 10, 32); err != nil {
			return invalid(path + ".level")
		}
	}
	if value, exists := user["email"]; exists {
		if _, ok := value.(string); !ok {
			return invalid(path + ".email")
		}
	}
	return nil
}
func (a *Assessment) socket(m map[string]any, path string) {
	for _, k := range []string{"mark", "interface", "penetrate", "customSockopt"} {
		a.detach(m, k, path, "local-control")
	}
	a.detach(m, "dialerProxy", path, "profile-dependency")
	for _, k := range []string{"acceptProxyProtocol", "v6only", "trustedXForwardedFor"} {
		a.detach(m, k, path, "listener-only")
	}
	if s := strings.ToLower(textValue(m["tproxy"])); s != "" && s != "off" {
		a.detach(m, "tproxy", path, "listener-only")
	}
}
func (a *Assessment) tls(m map[string]any, path string) error {
	if m == nil {
		return nil
	}
	a.detach(m, "masterKeyLog", path, "local-resource")
	for _, k := range []string{"rejectUnknownSni", "echServerKeys"} {
		a.detach(m, k, path, "listener-only")
	}
	a.detach(m, "allowInsecure", path, "core-removed")
	a.socket(objectValue(m["echSockopt"]), path+".echSockopt")
	if certs, ok := m["certificates"].([]any); ok {
		kept := make([]any, 0, len(certs))
		for _, c := range certs {
			cert := objectValue(c)
			// Detached local resources must not hide malformed inline material.
			if err := validateInlineCertificate(cert, path+".certificates[]"); err != nil {
				return err
			}
			file := active(cert["certificateFile"]) || active(cert["keyFile"])
			if file {
				if !active(cert["certificateFile"]) && !active(cert["certificate"]) {
					return invalid(path + ".certificates[].certificate")
				}
				usage := strings.ToLower(textValue(cert["usage"]))
				if usage != "" && usage != "encipherment" && usage != "verify" && usage != "issue" {
					return invalid(path + ".certificates[].usage")
				}
				a.Reasons = append(a.Reasons, Reason{"local-resource", path + ".certificates[]"})
				continue
			}
			if strings.EqualFold(textValue(cert["usage"]), "issue") {
				a.Reasons = append(a.Reasons, Reason{"listener-only", path + ".certificates[].usage"})
				continue
			}
			kept = append(kept, c)
		}
		m["certificates"] = kept
	}
	return nil
}
func (a *Assessment) stream(m map[string]any, path string) error {
	if m == nil {
		return nil
	}
	a.socket(objectValue(m["sockopt"]), path+".sockopt")
	if err := a.tls(objectValue(m["tlsSettings"]), path+".tlsSettings"); err != nil {
		return err
	}
	if r := objectValue(m["realitySettings"]); r != nil {
		a.detach(r, "masterKeyLog", path+".realitySettings", "local-resource")
		for _, k := range []string{"target", "dest", "type", "xver", "serverNames", "privateKey", "minClientVer", "maxClientVer", "maxTimeDiff", "shortIds", "mldsa65Seed", "limitFallbackUpload", "limitFallbackDownload"} {
			a.detach(r, k, path+".realitySettings", "listener-only")
		}
	}
	for _, k := range []string{"xhttpSettings", "splithttpSettings"} {
		x := objectValue(m[k])
		if x == nil {
			continue
		}
		a.detach(x, "h2Params", path+"."+k, "listener-only")
		if err := a.stream(objectValue(x["downloadSettings"]), path+"."+k+".downloadSettings"); err != nil {
			return err
		}
		extra := objectValue(x["extra"])
		a.detach(extra, "h2Params", path+"."+k+".extra", "listener-only")
		if err := a.stream(objectValue(extra["downloadSettings"]), path+"."+k+".extra.downloadSettings"); err != nil {
			return err
		}
	}
	// Hysteria's known listener metadata is inert in the client. Keep it for
	// typed private preservation; unknown nested fields still add Reasons.
	hy := objectValue(m["hysteriaSettings"])
	if masq := objectValue(hy["masquerade"]); masq != nil {
		if kind, ok := masq["type"].(string); ok && kind != "" && kind != "file" && kind != "proxy" && kind != "string" {
			a.detach(hy, "masquerade", path+".hysteriaSettings", "unknown-extension")
		}
	}
	for _, k := range []string{"rawSettings", "tcpSettings", "wsSettings", "httpupgradeSettings"} {
		a.detach(objectValue(m[k]), "acceptProxyProtocol", path+"."+k, "listener-only")
	}
	mask := objectValue(m["finalmask"])
	for _, lane := range []string{"tcp", "udp"} {
		items, _ := mask[lane].([]any)
		kept := make([]any, 0, len(items))
		for _, item := range items {
			v := objectValue(item)
			s := objectValue(v["settings"])
			p := path + ".finalmask." + lane + "[].settings"
			if v["type"] == "filter" {
				resource, err := validateFilterRules(s, p)
				if err != nil {
					return err
				}
				a.Reasons = append(a.Reasons, Reason{"listener-only", p})
				if resource {
					a.Reasons = append(a.Reasons, Reason{"local-resource", p})
				}
				continue
			}
			kept = append(kept, item)
			if v["type"] == "noise" {
				a.detach(s, "script", p, "local-execution")
			}
			if v["type"] == "realm" {
				if err := a.tls(objectValue(s["tlsConfig"]), p+".tlsConfig"); err != nil {
					return err
				}
			}
			if v["type"] == "xdns" {
				rs, _ := s["resolvers"].([]any)
				for _, r := range rs {
					if err := a.tls(objectValue(objectValue(r)["tlsConfig"]), p+".resolvers[].tlsConfig"); err != nil {
						return err
					}
				}
			}
		}
		if items != nil {
			mask[lane] = kept
		}
	}
	return a.streamContext(m, path)
}
