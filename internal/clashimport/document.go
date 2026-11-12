package clashimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/internal/xrayimport"
	"go.yaml.in/yaml/v3"
)

const DocumentWarning = "Из Clash/Mihomo импортируются отдельные серверы. DNS, rules, proxy-groups, providers и локальная политика UDP не применяются; удалённые providers не загружаются."

var documentCandidate = regexp.MustCompile(`(?m)(^|[,{])[ \t]*(proxies|type|'proxies'|'type'|"proxies"|"type")[ \t]*:`)
var ErrTooManyNodes = errors.New("too many Clash server nodes")
var errDocument = errors.New("invalid or overly complex Clash document")

// DecodeDocument extracts only server definitions. It never resolves providers,
// applies root policy, or echoes document values in errors. A nil element marks
// a malformed node, so subscriptions can report its original position.
func DecodeDocument(body []byte, maxNodes int) ([]map[string]any, bool, error) {
	body = xrayimport.StripBOM(body)
	candidate := documentCandidate.Match(body)
	if len(body) > 2*1024*1024 {
		return nil, candidate, errDocument
	}
	var value any
	if xrayimport.LooksJSON(body) {
		var err error
		value, err = strictjson.Decode(body)
		if err != nil {
			return nil, candidate, errDocument
		}
	} else {
		if !candidate {
			return nil, false, nil
		}
		// Bound allocation before building an untrusted YAML syntax tree.
		structural := 0
		for _, c := range body {
			if bytes.IndexByte([]byte(":,[]{}\n&*"), c) >= 0 {
				structural++
				if structural > 20000 {
					return nil, candidate, errDocument
				}
			}
		}
		d := yaml.NewDecoder(bytes.NewReader(body))
		var root, extra yaml.Node
		if d.Decode(&root) != nil {
			return nil, candidate, errDocument
		}
		if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
			return nil, false, nil
		}
		recognized := false
		for i := 0; i < len(root.Content[0].Content); i += 2 {
			key := root.Content[0].Content[i]
			if key.Kind == yaml.ScalarNode && (key.Value == "proxies" || key.Value == "type") {
				recognized = true
			}
		}
		if !recognized {
			return nil, false, nil
		}
		if d.Decode(&extra) != io.EOF {
			return nil, true, errDocument
		}
		budget := 20000
		var err error
		value, err = decodeYAMLNode(&root, 0, &budget, map[*yaml.Node]bool{})
		if err != nil {
			return nil, true, errDocument
		}
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	_, profile := root["proxies"]
	_, bare := root["type"]
	if !profile && !bare {
		return nil, false, nil
	}
	// Ambiguous mixed profile dialects must not select a subset silently.
	if _, exists := root["protocol"]; exists {
		return nil, true, errDocument
	}
	if _, exists := root["outbounds"]; exists {
		return nil, true, errDocument
	}
	if profile {
		if bare {
			return nil, true, errDocument
		}
		items, ok := root["proxies"].([]any)
		if !ok {
			return nil, true, errDocument
		}
		if maxNodes < 1 || len(items) > maxNodes {
			return nil, true, ErrTooManyNodes
		}
		out := make([]map[string]any, len(items))
		for i, item := range items {
			out[i], _ = item.(map[string]any)
		}
		return out, true, nil
	}
	return []map[string]any{root}, true, nil
}

func decodeYAMLNode(node *yaml.Node, depth int, budget *int, active map[*yaml.Node]bool) (any, error) {
	*budget--
	if node == nil || *budget < 0 || depth > 32 || active[node] {
		return nil, errDocument
	}
	active[node] = true
	defer delete(active, node)
	if node.Kind == yaml.AliasNode {
		return decodeYAMLNode(node.Alias, depth+1, budget, active)
	}
	switch node.Tag {
	case "!!map", "!!seq", "!!str", "!!int", "!!float", "!!bool", "!!null", "":
	default:
		return nil, errDocument
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return nil, errDocument
		}
		return decodeYAMLNode(node.Content[0], depth+1, budget, active)
	case yaml.MappingNode:
		out := map[string]any{}
		if len(node.Content)%2 != 0 {
			return nil, errDocument
		}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" {
				return nil, errDocument
			}
			if _, duplicate := out[key.Value]; duplicate {
				return nil, errDocument
			}
			value, err := decodeYAMLNode(node.Content[i+1], depth+1, budget, active)
			if err != nil {
				return nil, err
			}
			out[key.Value] = value
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := decodeYAMLNode(child, depth+1, budget, active)
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	case yaml.ScalarNode:
		var value any
		if node.Decode(&value) != nil {
			return nil, errDocument
		}
		b, err := json.Marshal(value)
		if err != nil {
			return nil, errDocument
		}
		return strictjson.Decode(b)
	}
	return nil, errDocument
}
