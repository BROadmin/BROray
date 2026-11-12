// Package xrayimport extracts client endpoints without applying profile DNS,
// routing, listeners or process configuration. It performs no I/O.
package xrayimport

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/internal/xrayschema"
)

// Checks are supplied per conversion by the record owner. No package-global
// callbacks can change another request's validation policy.
type Checks struct {
	VlessEncryptionValid  func(string) bool
	ValidateStreamOptions func(string, map[string]any) error
	ValidateClientFields  func(string, string, string, map[string]any) error
}

type converter struct {
	checks Checks
	limit  int
}

// Input is private parse input, not a public preview or a stored record.
// Native transports retain their structured credentials instead of a fake URI.
type Input struct {
	Link           string
	NativeOutbound map[string]any
	Name           string
	Warnings       []string
	ClientFields   map[string]any
	// UnavailableSource is private intent. Link/NativeOutbound are then only
	// validation projections and MUST NOT be connected or stored as runnable.
	UnavailableSource map[string]any
	FromProfile       bool
	Rejected          bool
	RejectionField    string
}

type Result struct{ Inputs []Input }

var ErrDocument = errors.New("invalid or unsupported import document")

type FieldError struct{ Field string }

func (e *FieldError) Error() string { return "unsupported client endpoint field" }

const ProfileWarning = "Из полного профиля импортирован только сервер. DNS, маршрутизация, inbound и API не применяются."
const JSONWarning = "Из JSON импортируются отдельные серверы. DNS, маршрутизация, балансировка и фрагментация профиля не применяются. Политикой управляет BROray. Параметры, требующие недоступных возможностей или ресурсов профиля, сохраняются с запретом подключения. Служебное поле gRPC mode=false не переносится."

// StripBOM removes framing only at byte zero; embedded BOMs are data.
func StripBOM(text []byte) []byte { return bytes.TrimPrefix(text, []byte{0xef, 0xbb, 0xbf}) }

func LooksJSON(text []byte) bool {
	b := bytes.TrimSpace(StripBOM(text))
	return len(b) > 0 && (b[0] == '{' || b[0] == '[')
}

func IsShareLink(text string) bool {
	if strings.ContainsAny(text, "\r\n") {
		return false
	}
	text = strings.TrimSpace(text)
	i := strings.Index(text, "://")
	return i > 0 && inList(text[:i], "vless", "vmess", "trojan", "ss", "hysteria2", "hy2", "tuic", "socks", "socks5", "http", "https", "vpn")
}

// DecodeBase64 unwraps one subscription/manual envelope. Protocol-specific
// VMess and Shadowsocks encoding is handled later by their URI parsers.
func DecodeBase64(text []byte) ([]byte, bool) {
	if len(text) == 0 || len(text) > 3*1024*1024 {
		return nil, false
	}
	compact := strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t', ' ':
			return -1
		case '_':
			return '/'
		case '-':
			return '+'
		}
		return r
	}, string(StripBOM(text)))
	switch len(compact) % 4 {
	case 2:
		compact += "=="
	case 3:
		compact += "="
	}
	b, err := base64.StdEncoding.DecodeString(compact)
	return b, err == nil && len(b) > 0 && len(b) <= 2*1024*1024
}

// DecodeJSON accepts a bare outbound, a full profile, or a flat array of
// share-link strings, bare outbounds and full profiles. Shape validation of
// the entire document precedes any endpoint conversion.
func DecodeJSON(text []byte, maxNodes int, checks Checks) (result Result, err error) {
	defer func() {
		if recover() != nil {
			result, err = Result{}, ErrDocument
		}
	}()
	if maxNodes < 1 || maxNodes > 1000000 || checks.VlessEncryptionValid == nil || checks.ValidateStreamOptions == nil {
		return Result{}, ErrDocument
	}
	v, e := strictjson.Decode(StripBOM(text))
	if e != nil {
		return Result{}, ErrDocument
	}
	items := []any{v}
	if list, ok := v.([]any); ok {
		items = list
	} else if _, ok := v.(map[string]any); !ok {
		return Result{}, ErrDocument
	}
	for _, item := range items {
		switch value := item.(type) {
		case string:
			if !IsShareLink(value) {
				return Result{}, ErrDocument
			}
		case map[string]any:
			if has(value, "outbounds") {
				if has(value, "protocol") {
					return Result{}, ErrDocument
				}
				list, ok := value["outbounds"].([]any)
				if !ok {
					return Result{}, ErrDocument
				}
				for _, candidate := range list {
					if out, ok := candidate.(map[string]any); !ok || out == nil {
						return Result{}, ErrDocument
					}
				}
			} else if _, ok := value["protocol"].(string); !ok {
				return Result{}, ErrDocument
			}
		default:
			return Result{}, ErrDocument
		}
	}
	c := converter{checks: checks}
	limit := maxNodes + 1
	for _, item := range items {
		if link, ok := item.(string); ok {
			result.Inputs = append(result.Inputs, Input{Link: strings.TrimSpace(link)})
		} else {
			m := item.(map[string]any)
			profile := map[string]any{}
			outbounds := []any{m}
			fromProfile := has(m, "outbounds")
			if fromProfile {
				profile = m
				outbounds = m["outbounds"].([]any)
			}
			var clients []map[string]any
			for _, value := range outbounds {
				out := value.(map[string]any)
				if out["protocol"] != "freedom" && out["protocol"] != "blackhole" {
					clients = append(clients, out)
				}
			}
			for index, out := range clients {
				c.limit = limit - len(result.Inputs)
				nodes := c.inputs(profile, out, index+1, len(clients) > 1, fromProfile)
				result.Inputs = append(result.Inputs, nodes...)
				if len(result.Inputs) >= limit {
					return result, nil
				}
			}
		}
		if len(result.Inputs) >= limit {
			return result, nil
		}
	}
	return result, nil
}

func (c *converter) inputs(profile, out map[string]any, index int, multiple, fromProfile bool) (nodes []Input) {
	defer func() {
		if failure := recover(); failure != nil {
			field := ""
			if f, ok := failure.(jqFail); ok {
				field = f.field
			}
			nodes = []Input{{Rejected: true, RejectionField: field, FromProfile: fromProfile}}
		}
	}()
	assessment, err := xrayschema.Classify(out)
	need(err == nil)
	var unavailable map[string]any
	if len(assessment.Reasons) > 0 {
		// A private source represents exactly one server. Expanding the same
		// complete intent into several records would make selection ambiguous.
		need(singleSourceEndpoint(assessment.Source.Fields))
		unavailable = assessment.Source.Fields
	}
	out = assessment.Projection
	out, clientFields, variants := c.extractClientFields(out)
	stream, _ := out["streamSettings"].(map[string]any)
	native := out["protocol"] == "wireguard" || out["protocol"] == "masque" || stream["network"] == "xdrive" || stream["network"] == "masque"
	if native {
		if len(variants) == 1 {
			for key, value := range variants[0] {
				clientFields[key] = value
			}
		}
		name := outboundName(profile, out, index, multiple)
		if !fromProfile {
			name = stringField(out, "tag", "")
		}
		return []Input{{NativeOutbound: out, Name: name, ClientFields: clientFields, UnavailableSource: unavailable, FromProfile: fromProfile}}
	}
	lines := c.outboundURIs(profile, out, index, multiple)
	need(unavailable == nil || len(lines) == 1)
	if len(lines) > c.limit {
		lines = lines[:c.limit]
	}
	for i, link := range lines {
		fields := without(clientFields)
		if i < len(variants) {
			for key, value := range variants[i] {
				fields[key] = value
			}
		}
		node := Input{Link: link, ClientFields: fields, UnavailableSource: unavailable, FromProfile: fromProfile}
		if out["protocol"] == "hysteria" {
			hysteria, _ := stream["hysteriaSettings"].(map[string]any)
			if has(hysteria, "masquerade") {
				node.Warnings = append(node.Warnings, hysteriaMasqueradeWarning)
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func singleSourceEndpoint(out map[string]any) bool {
	settings, _ := out["settings"].(map[string]any)
	for _, group := range []string{"vnext", "servers"} {
		if values, ok := settings[group].([]any); ok {
			if len(values) != 1 {
				return false
			}
			endpoint, _ := values[0].(map[string]any)
			if users, ok := endpoint["users"].([]any); ok && len(users) > 1 {
				return false
			}
		}
	}
	return true
}

// ConvertOutbound retains typed client fields that cannot fit in a share link.
// A caller must apply ClientFields before using the normalized record.
func ConvertOutbound(profile map[string]any, out any, index int, multiple bool, checks Checks) (nodes []Input, err error) {
	if checks.VlessEncryptionValid == nil || checks.ValidateStreamOptions == nil || checks.ValidateClientFields == nil {
		return nil, ErrDocument
	}
	m, ok := out.(map[string]any)
	if !ok || m == nil {
		return nil, ErrDocument
	}
	c := converter{checks: checks, limit: 501}
	nodes = c.inputs(profile, m, index, multiple, false)
	for _, node := range nodes {
		if node.Rejected {
			return nil, &FieldError{Field: node.RejectionField}
		}
	}
	return nodes, nil
}

// OutboundURIs is a deliberately narrow compatibility bridge. A string-only
// caller cannot retain typed sidecars or private unavailable intent.
func OutboundURIs(profile map[string]any, out any, index int, multiple bool, checks Checks) (lines []string, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			field := ""
			if f, ok := failure.(jqFail); ok {
				field = f.field
			}
			lines, err = nil, &FieldError{Field: field}
		}
	}()
	nodes, err := ConvertOutbound(profile, out, index, multiple, checks)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if node.UnavailableSource != nil || node.NativeOutbound != nil || node.Link == "" {
			return nil, &FieldError{}
		}
		for field := range node.ClientFields {
			if field != "tls" {
				return nil, &FieldError{Field: field}
			}
		}
	}
	// Basic TLS fields are already represented by the original narrow URI
	// converter; it rejects advanced fields rather than dropping the sidecar.
	c := converter{checks: checks, limit: 501}
	return c.outboundURIs(profile, out, index, multiple), nil
}
