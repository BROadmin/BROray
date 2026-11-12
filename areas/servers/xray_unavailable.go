package servers

import (
	"github.com/BROadmin/broray/internal/xrayimport"
	"github.com/BROadmin/broray/internal/xrayschema"
)

// classifyUnavailableXray uses the pinned source classifier and then the normal
// endpoint importer. A structural schema or a classification reason alone is
// insufficient: every mapped projection field must pass ordinary validation.
func classifyUnavailableXray(source map[string]any, historical bool) (*unavailableSource, *ParseError) {
	assessment, err := xrayschema.Classify(source)
	if err != nil || assessment == nil || assessment.Source == nil {
		return nil, unavailableProfileError()
	}
	if len(assessment.Reasons) == 0 && (!historical || !historicalXrayLevel(assessment.Source.Fields)) {
		return nil, unavailableProfileError()
	}
	inputs, err := xrayimport.ConvertOutbound(map[string]any{}, assessment.Projection, 1, false, serverImportChecks())
	if err != nil || len(inputs) != 1 || inputs[0].Rejected || inputs[0].UnavailableSource != nil {
		return nil, unavailableProfileError()
	}
	input := inputs[0]
	var projection Record
	var parseErr *ParseError
	if input.NativeOutbound != nil {
		projection, parseErr = parseNativeOutbound(input.NativeOutbound, input.Name, "source-validation", nil)
	} else {
		projection, parseErr = parseLink(input.Link, "source-validation", nil)
	}
	if parseErr != nil {
		return nil, unavailableProfileError()
	}
	if len(input.ClientFields) > 0 {
		projection, err = ApplyClientFields(projection, input.ClientFields)
		if err != nil {
			return nil, unavailableProfileError()
		}
	}
	if Validate(projection, "source-validation") != nil {
		return nil, unavailableProfileError()
	}
	return &unavailableSource{
		Protocol: asString(assessment.Source.Fields["protocol"]),
		Address:  asString(projection["address"]), Port: projection["port"],
		Node: assessment.Source.Fields, Projection: projection,
	}, nil
}

// WrapUnavailableXray accepts an already validated projection together with its
// complete original source. It independently revalidates both the source and
// projection before retaining a private, explicitly non-executable record.
func WrapUnavailableXray(projection Record, source map[string]any) (Record, error) {
	if has(projection, "importProfile") || Validate(projection, "source-validation") != nil {
		return nil, unavailableProfileError()
	}
	classified, err := classifyUnavailableXray(source, false)
	if err != nil {
		return nil, err
	}
	want, wantErr := CanonicalKey(classified.Projection)
	got, gotErr := CanonicalKey(projection)
	if wantErr != nil || gotErr != nil || want != got {
		return nil, unavailableProfileError()
	}
	rec := normalize(Record{
		"schemaVersion": 2, "id": projection["id"], "name": projection["name"], "source": projection["source"],
		"protocol": classified.Protocol, "address": classified.Address, "port": classified.Port,
		"network": "unavailable", "security": "none",
		"importProfile": map[string]any{"format": "xray", "sourceCommit": xrayschema.SourceCommit, "node": classified.Node},
	})
	addNativeImportWarning(rec, importedProfileUnavailable)
	if warnings, ok := projection["importWarnings"].([]any); ok {
		for _, warning := range warnings {
			if text, ok := warning.(string); ok {
				addNativeImportWarning(rec, text)
			}
		}
	}
	if err := validateUnavailableProfile(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// C3 incorrectly classified nonzero account levels as a provider-policy
// dependency. Keep only that old private storage shape valid and disabled;
// the complete projection still passes current endpoint validation above.
// Fresh imports cannot create these historical wrappers.
func historicalXrayLevel(source map[string]any) bool {
	protocol := asString(source["protocol"])
	if protocol == "hysteria" {
		m := get(source, "streamSettings", "hysteriaSettings", "masquerade")
		return m != nil && xrayimport.ValidateHysteriaMasqueradeMetadata(m) == nil
	}
	if !in(protocol, "vless", "vmess", "http", "socks", "trojan", "shadowsocks") {
		return false
	}
	settings, _ := source["settings"].(map[string]any)
	account := settings
	if !has(settings, "address") {
		group := "servers"
		if in(protocol, "vless", "vmess") {
			group = "vnext"
		}
		endpoints, _ := settings[group].([]any)
		if len(endpoints) != 1 {
			return false
		}
		account, _ = endpoints[0].(map[string]any)
		if in(protocol, "vless", "vmess", "http", "socks") {
			users, _ := account["users"].([]any)
			if len(users) != 1 {
				return false
			}
			account, _ = users[0].(map[string]any)
		}
	}
	level, ok := freshUnsigned(account["level"], 32)
	return ok && level != 0
}
