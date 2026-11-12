package subscriptions

import (
	"github.com/BROadmin/broray/internal/clashimport"
)

const clashWarning = clashimport.DocumentWarning

// Kept for package callers; the bounded grammar is shared with manual import.
func clashDocument(body []byte) (map[string]any, bool, error) {
	nodes, recognized, err := clashimport.DecodeDocument(body, maxNodes)
	if !recognized || err != nil {
		return nil, recognized, err
	}
	values := make([]any, len(nodes))
	for i, node := range nodes {
		values[i] = node
	}
	return map[string]any{"proxies": values}, true, nil
}

func clashExtract(body []byte) (*extracted, bool, *subErr) {
	nodes, recognized, err := clashimport.DecodeDocument(body, maxNodes)
	if !recognized {
		return nil, false, nil
	}
	if err == clashimport.ErrTooManyNodes {
		return nil, true, errTooLarge
	}
	if err != nil {
		return nil, true, fail("PARSE_ERROR", "Некорректная или слишком сложная структура Clash YAML/JSON.")
	}
	if len(nodes) == 0 {
		return nil, true, fail("NO_VALID_NODES", "Подписка не содержит узлов.")
	}
	ex := &extracted{Clash: true, NodeWarnings: map[int][]string{}, ClientFields: map[int]map[string]any{}, UnavailableClash: map[int]map[string]any{}, Native: map[int]nativeJSONInput{}}
	for i, node := range nodes {
		if node == nil {
			ex.Lines = append(ex.Lines, unsupportedMarker+"#Clash.parameters")
			continue
		}
		input, err := clashimport.ConvertNode(node, importChecks())
		line := input.Link
		if err != nil {
			field := "parameters"
			if e, ok := err.(*clashimport.FieldError); ok {
				field = e.Field
			}
			line = unsupportedMarker + "#Clash." + safeJSONField(field)
		} else if input.Unavailable != nil {
			// Empty placeholders retain source indices without embedding private JSON
			// in a URI or any diagnostic. The staging adapter revalidates this source.
			ex.UnavailableClash[i] = input.Unavailable.Fields
			line = ""
		} else if input.NativeOutbound != nil {
			ex.Native[i] = nativeJSONInput{Outbound: input.NativeOutbound, Name: input.Name}
			line = ""
		}
		ex.Lines = append(ex.Lines, line)
		ex.NodeWarnings[i] = input.Warnings
		if input.Unavailable == nil {
			ex.ClientFields[i] = input.ClientFields
		}
	}
	return ex, true, nil
}

// clashNode is the compatibility bridge for callers which only consume a URI.
// Rich imports use ConvertNode directly and apply its typed ClientFields.
func clashNode(v any) (line string, warnings []string, field string) {
	source, ok := v.(map[string]any)
	if !ok {
		return "", nil, "parameters"
	}
	input, err := clashimport.ConvertNode(source, importChecks())
	if err != nil {
		if e, ok := err.(*clashimport.FieldError); ok {
			return "", nil, e.Field
		}
		return "", nil, "parameters"
	}
	if input.Unavailable != nil {
		return "", input.Warnings, "capability"
	}
	if input.NativeOutbound != nil {
		return "", input.Warnings, "nativeOutbound"
	}
	// Returning a bare link would erase validated certificate/header sidecars.
	if len(input.ClientFields) > 0 {
		return "", input.Warnings, "clientFields"
	}
	return input.Link, input.Warnings, ""
}
