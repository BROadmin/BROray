// Package xrayschema accounts for pinned Xray client fields without executing
// provider profiles or claiming that structural validation implies support.
package xrayschema

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type Node struct {
	Fields       map[string]any
	KnownFields  map[string]any
	UnknownPaths []string
}

func Parse(input map[string]any) (*Node, error) {
	n, err := parse(input, false)
	if err != nil {
		return nil, err
	}
	known, err := parse(input, true)
	if err != nil {
		return nil, err
	}
	n.KnownFields = known.Fields
	return n, nil
}

func parse(input map[string]any, dropUnknown bool) (*Node, error) {
	n := &Node{}
	b := budget{remaining: 20000, node: n, dropUnknown: dropUnknown}
	m, err := b.object(input, "OutboundDetourConfig", "Xray", 0)
	if err != nil {
		return nil, err
	}
	protocol, _ := m["protocol"].(string)
	if _, ok := protocolSchemas[protocol]; !ok {
		return nil, invalid("Xray.protocol")
	}
	n.Fields = m
	sort.Strings(n.UnknownPaths)
	return n, nil
}

type budget struct {
	dropUnknown      bool
	remaining, bytes int
	node             *Node
}

func invalid(path string) error { return fmt.Errorf("неправильное поле %s", path) }
func (b *budget) count(depth, size int) error {
	b.remaining--
	b.bytes += size
	if depth > 32 || b.remaining < 0 || b.bytes > 256*1024 {
		return invalid("Xray.bounds")
	}
	return nil
}
func folded(m map[string]any, key string) any {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return nil
}

func (b *budget) object(m map[string]any, schema, path string, depth int) (map[string]any, error) {
	if m == nil {
		return nil, invalid(path)
	}
	if e := b.count(depth, len(m)); e != nil {
		return nil, e
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]any{}
	for _, k := range keys {
		canonical, typ := "", ""
		for name, t := range fieldSchemas[schema] {
			if strings.EqualFold(k, name) {
				canonical, typ = name, t
				break
			}
		}
		// Compatibility for old AEAD VMess exports, not a current core field.
		// Only explicit numeric zero is neutral; never revive legacy alter IDs.
		if (schema == "VMessOutboundConfig" || schema == "VMessUser") && strings.EqualFold(k, "alterId") {
			if value, ok := number(m[k]); !ok || value != "0" {
				return nil, invalid(path + ".alterId")
			}
			canonical, typ = "alterId", "uint32"
		}
		if e := b.count(depth, len(k)); e != nil {
			return nil, e
		}
		if typ == "" {
			if b.dropUnknown {
				continue
			}
			v, e := b.copy(m[k], depth+1)
			if e != nil {
				return nil, e
			}
			out[k] = v
			b.node.UnknownPaths = append(b.node.UnknownPaths, path+".<extension>")
			continue
		}
		// These upstream raw messages are typed by surrounding protocol/mask.
		if schema == "OutboundDetourConfig" && canonical == "settings" {
			p, _ := folded(m, "protocol").(string)
			s, ok := protocolSchemas[p]
			if !ok {
				return nil, invalid("Xray.protocol")
			}
			typ = "*" + s
		}
		if schema == "SplitHTTPConfig" && canonical == "extra" {
			typ = "*SplitHTTPConfig"
		}
		if schema == "TCPConfig" && canonical == "header" {
			h, ok := m[k].(map[string]any)
			if !ok {
				return nil, invalid(path + ".header")
			}
			kind, _ := folded(h, "type").(string)
			if kind == "http" {
				typ = "Authenticator"
			} else if kind == "none" {
				typ = "NoHeader"
			} else {
				b.node.UnknownPaths = append(b.node.UnknownPaths, path+".header.<extension>")
			}
		}
		if schema == "Mask" && canonical == "settings" {
			kind, _ := folded(m, "type").(string)
			maskSchemas := map[string]string{"filter": "Filter", "fragment": "FragmentMask", "noise": "NoiseMask", "mkcp-legacy": "MkcpLegacy", "salamander": "Salamander", "sudoku": "Sudoku", "xdns": "XDNS", "xmc": "XMC", "xicmp": "Xicmp", "realm": "Realm", "udphop": "UDPHop"}
			if kind == "header-custom" {
				if strings.Contains(path, ".tcp[]") {
					typ = "HeaderCustomTCP"
				} else {
					typ = "HeaderCustomUDP"
				}
			} else if s, ok := maskSchemas[kind]; ok {
				typ = s
			} else {
				b.node.UnknownPaths = append(b.node.UnknownPaths, path+".settings.<extension>")
			}
		}
		v, e := b.value(m[k], typ, path+"."+canonical, depth+1)
		if e != nil {
			return nil, e
		}
		if old, ok := out[canonical]; ok && !reflect.DeepEqual(old, v) {
			return nil, invalid(path + "." + canonical)
		}
		out[canonical] = v
	}
	return out, nil
}

func (b *budget) value(v any, typ, path string, depth int) (any, error) {
	if e := b.count(depth, 0); e != nil {
		return nil, e
	}
	if strings.HasPrefix(typ, "*") {
		if v == nil {
			return nil, nil
		}
		return b.value(v, typ[1:], path, depth+1)
	}
	if v == nil {
		if strings.HasPrefix(typ, "[]") || strings.HasPrefix(typ, "map[") || typ == "json.RawMessage" || typ == "interface{}" {
			return nil, nil
		}
		return nil, invalid(path)
	}
	if strings.HasPrefix(typ, "[]") {
		if typ == "[]byte" {
			if s, ok := v.(string); ok {
				raw, e := base64.StdEncoding.DecodeString(s)
				if e != nil {
					return nil, invalid(path)
				}
				if e = b.count(depth, len(raw)); e != nil {
					return nil, e
				}
				return s, nil
			}
		}
		a, ok := v.([]any)
		if !ok || len(a) > 20000 {
			return nil, invalid(path)
		}
		out := make([]any, len(a))
		for i, x := range a {
			if x == nil {
				return nil, invalid(path + "[]")
			}
			vv, e := b.value(x, typ[2:], path+"[]", depth+1)
			if e != nil {
				return nil, e
			}
			out[i] = vv
		}
		return out, nil
	}
	if strings.HasPrefix(typ, "map[string]") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, invalid(path)
		}
		out := map[string]any{}
		for k, x := range m {
			if e := b.count(depth, len(k)); e != nil {
				return nil, e
			}
			vv, e := b.value(x, typ[11:], path+".*", depth+1)
			if e != nil {
				return nil, e
			}
			out[k] = vv
		}
		return out, nil
	}
	if typ == "NoHeader" {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, invalid(path)
		}
		for k := range m {
			if k != "type" {
				b.node.UnknownPaths = append(b.node.UnknownPaths, path+".<extension>")
			}
		}
		if b.dropUnknown {
			return map[string]any{"type": m["type"]}, nil
		}
		return b.copy(v, depth+1)
	}
	if _, ok := fieldSchemas[typ]; ok {
		m, yes := v.(map[string]any)
		if !yes {
			return nil, invalid(path)
		}
		return b.object(m, typ, path, depth+1)
	}
	switch typ {
	case "Address", "string", "Bandwidth", "TransportProtocol":
		s, ok := v.(string)
		if !ok {
			return nil, invalid(path)
		}
		if e := b.count(depth, len(s)); e != nil {
			return nil, e
		}
		return s, nil
	case "StringList":
		if s, ok := v.(string); ok {
			if e := b.count(depth, len(s)); e != nil {
				return nil, e
			}
			return s, nil
		}
		return b.value(v, "[]string", path, depth+1)
	case "bool":
		if x, ok := v.(bool); ok {
			return x, nil
		}
	case "byte", "uint16", "uint32", "uint64", "int16", "int32", "int64":
		s, ok := number(v)
		if !ok {
			return nil, invalid(path)
		}
		bits := 8
		if typ != "byte" {
			bits, _ = strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(typ, "u"), "int"))
		}
		if strings.HasPrefix(typ, "u") || typ == "byte" {
			if _, e := strconv.ParseUint(s, 10, bits); e != nil {
				return nil, invalid(path)
			}
		} else if _, e := strconv.ParseInt(s, 10, bits); e != nil {
			return nil, invalid(path)
		}
		return json.Number(s), nil
	case "Int32Range":
		if s, ok := v.(string); ok {
			if !validRange(s, 32) {
				return nil, invalid(path)
			}
			return s, nil
		}
		return b.value(v, "int32", path, depth+1)
	case "PortList":
		if s, ok := v.(string); ok {
			for _, p := range strings.Split(s, ",") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "env:") {
					continue
				}
				if !validRange(p, 16) {
					return nil, invalid(path)
				}
			}
			if e := b.count(depth, len(s)); e != nil {
				return nil, e
			}
			return s, nil
		}
		return b.value(v, "uint16", path, depth+1)
	case "realm.PortMapping":
		m, ok := v.(map[string]any)
		if !ok {
			return nil, invalid(path)
		}
		out := map[string]any{}
		for k, x := range m {
			t := "int64"
			if k == "enabled" {
				t = "bool"
			} else if k != "timeout" && k != "lifetime" {
				if b.dropUnknown {
					continue
				}
				b.node.UnknownPaths = append(b.node.UnknownPaths, path+".<extension>")
				vv, e := b.copy(x, depth+1)
				if e != nil {
					return nil, e
				}
				out[k] = vv
				continue
			}
			vv, e := b.value(x, t, path+"."+k, depth+1)
			if e != nil {
				return nil, e
			}
			out[k] = vv
		}
		return out, nil
	case "json.RawMessage", "interface{}":
		// Raw byte payloads, XDrive template, and TFO have builders which impose
		// further semantics. This parser retains shape; it never runs them.
		return b.copy(v, depth+1)
	}
	return nil, invalid(path)
}

func number(v any) (string, bool) {
	switch n := v.(type) {
	case json.Number:
		return string(n), true
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), true
	case int:
		return strconv.Itoa(n), true
	case int32:
		return strconv.FormatInt(int64(n), 10), true
	case int64:
		return strconv.FormatInt(n, 10), true
	case uint32:
		return strconv.FormatUint(uint64(n), 10), true
	case uint64:
		return strconv.FormatUint(n, 10), true
	}
	return "", false
}
func validRange(s string, bits int) bool {
	if s == "" {
		return true
	}
	parts := []string{s}
	start := 0
	if strings.HasPrefix(s, "-") {
		start = 1
	}
	if pos := strings.Index(s[start:], "-"); pos >= 0 {
		pos += start
		parts = []string{s[:pos], s[pos+1:]}
	}
	for _, p := range parts {
		if bits == 16 {
			if _, e := strconv.ParseUint(p, 10, 16); e != nil {
				return false
			}
		} else if _, e := strconv.ParseInt(p, 10, bits); e != nil {
			return false
		}
	}
	return true
}
func (b *budget) copy(v any, depth int) (any, error) {
	if e := b.count(depth, 0); e != nil {
		return nil, e
	}
	switch x := v.(type) {
	case nil, bool:
		return x, nil
	case string:
		if e := b.count(depth, len(x)); e != nil {
			return nil, e
		}
		return x, nil
	case map[string]any:
		out := map[string]any{}
		for k, vv := range x {
			if e := b.count(depth, len(k)); e != nil {
				return nil, e
			}
			cv, e := b.copy(vv, depth+1)
			if e != nil {
				return nil, e
			}
			out[k] = cv
		}
		return out, nil
	case []any:
		if len(x) > 20000 {
			return nil, invalid("Xray.bounds")
		}
		out := make([]any, len(x))
		for i, vv := range x {
			cv, e := b.copy(vv, depth+1)
			if e != nil {
				return nil, e
			}
			out[i] = cv
		}
		return out, nil
	default:
		if s, ok := number(v); ok && json.Valid([]byte(s)) {
			return json.Number(s), nil
		}
	}
	return nil, invalid("Xray.value")
}
