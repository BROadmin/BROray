// Package clashschema validates the pinned Mihomo server field vocabulary.
// Validation alone does not establish an Xray runtime mapping.
package clashschema

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
	Type         string
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
	typ, ok := input["type"].(string)
	schema, known := protocolSchemas[typ]
	if !ok || !known {
		return nil, invalid("type")
	}
	n := &Node{Type: typ}
	b := budget{remaining: 20000, node: n, dropUnknown: dropUnknown}
	v, err := b.object(input, schema, "Clash", 0)
	if err != nil {
		return nil, err
	}
	n.Fields = v
	if typ == "ss" {
		plugin, _ := v["plugin"].(string)
		plugins := map[string]string{"obfs": "simpleObfsOption", "v2ray-plugin": "v2rayObfsOption", "gost-plugin": "gostObfsOption", "shadow-tls": "shadowTLSOption", "restls": "restlsOption", "jls": "jlsOption", "kcptun": "kcpTunOption"}
		if opts, present := v["plugin-opts"]; present {
			if s, known := plugins[plugin]; known {
				parsed, e := b.value(opts, s, "Clash.plugin-opts", 1)
				if e != nil {
					return nil, e
				}
				n.Fields["plugin-opts"] = parsed
			} else if plugin != "" {
				n.UnknownPaths = append(n.UnknownPaths, "Clash.plugin-opts.<extension>")
			}
		}
	}
	sort.Strings(n.UnknownPaths)
	return n, nil
}

type budget struct {
	dropUnknown bool
	remaining   int
	bytes       int
	node        *Node
}

func invalid(path string) error { return fmt.Errorf("неправильное поле %s", path) }

func (b *budget) count(depth int, size int) error {
	b.remaining--
	b.bytes += size
	if depth > 32 || b.remaining < 0 || b.bytes > 256*1024 {
		return invalid("Clash.bounds")
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
	out := map[string]any{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		canonical := strings.ToLower(strings.ReplaceAll(k, "_", "-"))
		// One upstream field deliberately uses mixed case (alterId). Keys
		// compare case-insensitively but persist the declared spelling.
		if canonical == "alterid" {
			canonical = "alterId"
		}
		fieldType, known := fieldSchemas[schema][canonical]
		if !known {
			if b.dropUnknown {
				continue
			}
			// Unknown names and values remain private. They cannot certify runtime
			// support, and diagnostic paths must not echo attacker-controlled keys.
			v, e := b.copy(m[k], depth+1)
			if e != nil {
				return nil, e
			}
			if e = b.count(depth, len(k)); e != nil {
				return nil, e
			}
			out[k] = v
			b.node.UnknownPaths = append(b.node.UnknownPaths, path+".<extension>")
			continue
		}
		v, e := b.value(m[k], fieldType, path+"."+canonical, depth+1)
		if e != nil {
			return nil, e
		}
		if old, exists := out[canonical]; exists && !reflect.DeepEqual(old, v) {
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
		return nil, invalid(path)
	}
	if strings.HasPrefix(typ, "[]") {
		if typ == "[]uint8" {
			if s, ok := v.(string); ok {
				if len(s) > 256*1024 {
					return nil, invalid(path)
				}
				raw, e := base64.StdEncoding.DecodeString(s)
				if e != nil {
					return nil, invalid(path)
				}
				a := make([]any, len(raw))
				for i, n := range raw {
					a[i] = json.Number(strconv.Itoa(int(n)))
				}
				v = a
			}
		}
		a, ok := v.([]any)
		if !ok {
			return nil, invalid(path)
		}
		if len(a) > 20000 {
			return nil, invalid("Clash.bounds")
		}
		out := make([]any, len(a))
		for i, x := range a {
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
		if !ok || m == nil {
			return nil, invalid(path)
		}
		out := map[string]any{}
		for k, x := range m {
			if e := b.count(depth, len(k)); e != nil {
				return nil, e
			}
			vv, e := b.value(x, typ[len("map[string]"):], path+".*", depth+1)
			if e != nil {
				return nil, e
			}
			out[k] = vv
		}
		return out, nil
	}
	if _, ok := fieldSchemas[typ]; ok {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, invalid(path)
		}
		return b.object(m, typ, path, depth+1)
	}
	switch typ {
	case "any":
		return b.copy(v, depth+1)
	case "string", "C.DNSPrefer":
		s, ok := v.(string)
		if !ok {
			var yes bool
			s, yes = numberText(v)
			if !yes {
				return nil, invalid(path)
			}
		}
		if e := b.count(depth, len(s)); e != nil {
			return nil, e
		}
		return s, nil
	case "bool":
		if v, ok := v.(bool); ok {
			return v, nil
		}
		if n, ok := numberText(v); ok {
			i, e := strconv.ParseInt(n, 10, 64)
			if e == nil {
				return i != 0, nil
			}
		}
		return nil, invalid(path)
	case "int", "int32", "int64", "uint8", "uint16", "uint32", "uint64":
		s, ok := numberText(v)
		base := 10
		if !ok {
			s, ok = v.(string)
			base = 0
		}
		if !ok {
			return nil, invalid(path)
		}
		bits := 64
		if typ != "int" && typ != "int64" && typ != "uint64" {
			bits, _ = strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(typ, "u"), "int"))
		}
		if strings.HasPrefix(typ, "uint") {
			n, e := strconv.ParseUint(s, base, bits)
			if e != nil {
				return nil, invalid(path)
			}
			return json.Number(strconv.FormatUint(n, 10)), nil
		}
		n, e := strconv.ParseInt(s, base, bits)
		if e != nil {
			return nil, invalid(path)
		}
		return json.Number(strconv.FormatInt(n, 10)), nil
	}
	return nil, invalid(path)
}

func numberText(v any) (string, bool) {
	switch n := v.(type) {
	case json.Number:
		return n.String(), true
	case int:
		return strconv.FormatInt(int64(n), 10), true
	case int64:
		return strconv.FormatInt(n, 10), true
	case uint64:
		return strconv.FormatUint(n, 10), true
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), true
	}
	return "", false
}

func (b *budget) copy(v any, depth int) (any, error) {
	if e := b.count(depth, 0); e != nil {
		return nil, e
	}
	switch v := v.(type) {
	case nil, bool:
		return v, nil
	case string:
		if e := b.count(depth, len(v)); e != nil {
			return nil, e
		}
		return v, nil
	case json.Number:
		if _, e := json.Marshal(v); e != nil {
			return nil, invalid("Clash.extension")
		}
		return v, nil
	case int, int64, uint64, float64:
		data, e := json.Marshal(v)
		if e != nil {
			return nil, invalid("Clash.extension")
		}
		return json.Number(data), nil
	case []any:
		if len(v) > 20000 {
			return nil, invalid("Clash.bounds")
		}
		out := make([]any, len(v))
		for i, x := range v {
			c, e := b.copy(x, depth+1)
			if e != nil {
				return nil, e
			}
			out[i] = c
		}
		return out, nil
	case map[string]any:
		out := map[string]any{}
		for k, x := range v {
			if e := b.count(depth, len(k)); e != nil {
				return nil, e
			}
			c, e := b.copy(x, depth+1)
			if e != nil {
				return nil, e
			}
			out[k] = c
		}
		return out, nil
	}
	return nil, invalid("Clash.extension")
}
