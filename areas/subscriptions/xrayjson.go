package subscriptions

import (
	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/xrayimport"
)

const unsupportedMarker = "broray-json-error://unsupported-node"

func importChecks() xrayimport.Checks {
	return xrayimport.Checks{
		VlessEncryptionValid:  servers.VlessEncryptionValid,
		ValidateStreamOptions: servers.ValidateStreamOptions,
		ValidateClientFields:  servers.ValidateClientFields,
	}
}

func outboundURIs(profile map[string]any, out any, index int, multiple bool) []string {
	lines, err := xrayimport.OutboundURIs(profile, out, index, multiple, importChecks())
	if err != nil {
		field := ""
		if e, ok := err.(*xrayimport.FieldError); ok {
			field = e.Field
		}
		panic(jqFail{field: field})
	}
	return lines
}

func xrayJSONToLinks(text []byte, maxNodes int) ([]string, bool) {
	return xrayJSONToInputs(text, maxNodes, nil, nil)
}

func xrayJSONToInputs(text []byte, maxNodes int, native map[int]nativeJSONInput, nodeWarnings map[int][]string) ([]string, bool) {
	ex, ok := xrayJSONExtract(text, maxNodes)
	if !ok {
		return nil, false
	}
	for index, input := range ex.Native {
		if native == nil {
			ex.Lines[index] = unsupportedMarker
		} else {
			native[index] = input
		}
	}
	for index, warnings := range ex.NodeWarnings {
		if nodeWarnings != nil {
			nodeWarnings[index] = warnings
		}
	}
	// This legacy return shape has no private-source channel. Never expose a
	// runnable validation projection after discarding its unavailable source.
	for index := range ex.UnavailableXray {
		ex.Lines[index] = unsupportedMarker
		delete(native, index)
	}
	return ex.Lines, true
}

func xrayJSONExtract(text []byte, maxNodes int) (*extracted, bool) {
	result, err := xrayimport.DecodeJSON(text, maxNodes, importChecks())
	if err != nil {
		return nil, false
	}
	ex := &extracted{JSONImport: true, Native: map[int]nativeJSONInput{}, NodeWarnings: map[int][]string{}, ClientFields: map[int]map[string]any{}, UnavailableXray: map[int]map[string]any{}}
	for _, input := range result.Inputs {
		index := len(ex.Lines)
		line := input.Link
		if input.Rejected {
			line = unsupportedMarker
			if input.RejectionField != "" {
				line += "#" + safeJSONField(input.RejectionField)
			}
		} else if input.NativeOutbound != nil {
			ex.Native[index] = nativeJSONInput{Outbound: input.NativeOutbound, Name: input.Name}
			line = "JSON"
		}
		ex.Lines = append(ex.Lines, line)
		if len(input.Warnings) > 0 {
			ex.NodeWarnings[index] = input.Warnings
		}
		if len(input.ClientFields) > 0 {
			ex.ClientFields[index] = input.ClientFields
		}
		if input.UnavailableSource != nil {
			ex.UnavailableXray[index] = input.UnavailableSource
		}
	}
	return ex, true
}
