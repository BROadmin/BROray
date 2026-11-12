package servers

import (
	"encoding/json"

	"github.com/BROadmin/broray/internal/clashimport"
	"github.com/BROadmin/broray/internal/clashschema"
	"github.com/BROadmin/broray/internal/xrayimport"
	"github.com/BROadmin/broray/internal/xrayschema"
)

const importedProfileUnavailable = "Конфигурация сервера сохранена со всеми параметрами. Подключение с этими параметрами пока недоступно в BROray."

func serverImportChecks() xrayimport.Checks {
	return xrayimport.Checks{
		VlessEncryptionValid:  VlessEncryptionValid,
		ValidateStreamOptions: ValidateStreamOptions,
		ValidateClientFields:  ValidateClientFields,
	}
}

// ParseUnavailableClash accepts only an explicit, semantically validated
// capability result. No validation failure is converted into an unavailable
// profile, and no stripped projection can become executable.
func ParseUnavailableClash(input map[string]any, id string, source map[string]any) (Record, error) {
	converted, err := clashimport.ConvertNode(input, serverImportChecks())
	if err != nil || converted.Unavailable == nil || converted.Capability == nil || converted.Link != "" {
		return nil, unavailableProfileError()
	}
	if source == nil {
		source = map[string]any{"type": "manual"}
	}
	node := normalize(converted.Unavailable.Fields)
	address, port := unavailableEndpoint(node)
	name := asString(node["name"])
	if name == "" {
		name = converted.Unavailable.Type
	}
	rec := normalize(Record{
		"schemaVersion": 2, "id": id, "name": name, "source": cloneSource(source),
		"protocol": converted.Unavailable.Type, "address": address, "port": port,
		"network": "unavailable", "security": "none",
		"importProfile": map[string]any{"format": "clash", "sourceCommit": clashschema.SourceCommit, "node": node},
	})
	addNativeImportWarning(rec, importedProfileUnavailable)
	for _, warning := range converted.Warnings {
		addNativeImportWarning(rec, warning)
	}
	if err := validateUnavailableProfile(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func unavailableProfileError() *ParseError {
	return die("неправильная сохранённая конфигурация импортированного сервера")
}

// Source formats have independent pinned schemas and capability classifiers.
// Reasons and supported flags are never persisted as authority.
type unavailableSource struct {
	Protocol   string
	Address    string
	Port       any
	Node       map[string]any
	Projection Record
}

func classifyUnavailableProfile(profile map[string]any) (*unavailableSource, *ParseError) {
	if !keysSubset(profile, "format", "sourceCommit", "node") {
		return nil, unavailableProfileError()
	}
	node, ok := profile["node"].(map[string]any)
	if !ok {
		return nil, unavailableProfileError()
	}
	b, err := json.Marshal(node)
	if err != nil || len(b) > 256*1024 {
		return nil, unavailableProfileError()
	}
	switch profile["format"] {
	case "clash":
		if profile["sourceCommit"] != clashschema.SourceCommit {
			return nil, unavailableProfileError()
		}
		converted, err := clashimport.ConvertNode(node, serverImportChecks())
		if err != nil || converted.Unavailable == nil || converted.Capability == nil || converted.Link != "" {
			return nil, unavailableProfileError()
		}
		address, port := unavailableEndpoint(converted.Unavailable.Fields)
		return &unavailableSource{Protocol: converted.Unavailable.Type, Address: address, Port: port, Node: converted.Unavailable.Fields}, nil
	case "xray":
		if profile["sourceCommit"] != xrayschema.SourceCommit {
			return nil, unavailableProfileError()
		}
		return classifyUnavailableXray(node, true)
	}
	return nil, unavailableProfileError()
}

// Mesh protocols may have no host/port pair; their classifier validates their
// own endpoint shape. Presentation must still match the normalized source.
func unavailableEndpoint(node map[string]any) (string, any) {
	port := node["port"]
	if port == nil {
		port = json.Number("0")
	}
	return asString(node["server"]), port
}

func validateUnavailableProfile(rec Record) *ParseError {
	if !jsonEq(rec["schemaVersion"], 2) || !ValidID(asString(rec["id"])) || !isText(rec["name"]) || !isObject(rec["source"]) || !nonEmptyString(get(rec, "source", "type")) || rec["network"] != "unavailable" || rec["security"] != "none" {
		return unavailableProfileError()
	}
	profile, ok := rec["importProfile"].(map[string]any)
	if !ok {
		return unavailableProfileError()
	}
	node, err := classifyUnavailableProfile(profile)
	if err != nil {
		return err
	}
	if rec["protocol"] != node.Protocol || rec["address"] != node.Address || !jsonEq(rec["port"], node.Port) {
		return unavailableProfileError()
	}
	return nil
}

func unavailableProfileIdentity(rec Record) map[string]any {
	profile, _ := rec["importProfile"].(map[string]any)
	identity := normalize(profile)
	// Schema provenance must validate, but does not change connection intent.
	delete(identity, "sourceCommit")
	if node, ok := identity["node"].(map[string]any); ok {
		switch identity["format"] {
		case "clash":
			delete(node, "name")
		case "xray":
			delete(node, "tag")
		}
	}
	return map[string]any{"importProfile": identity}
}

func parseClashDocument(text, id string, source map[string]any) (Record, bool, *ParseError) {
	nodes, recognized, err := clashimport.DecodeDocument([]byte(text), 1)
	if !recognized {
		return nil, false, nil
	}
	if err == clashimport.ErrTooManyNodes || err == nil && len(nodes) != 1 {
		return nil, true, die("для ручного импорта Clash нужен один сервер; список добавьте как подписку")
	}
	if err != nil || nodes[0] == nil {
		return nil, true, die("неправильная структура документа Clash")
	}
	input, err := clashimport.ConvertNode(nodes[0], serverImportChecks())
	if err != nil {
		return nil, true, die("неправильные параметры сервера Clash")
	}
	var rec Record
	if input.Unavailable != nil {
		rec, err = ParseUnavailableClash(input.Unavailable.Fields, id, source)
	} else {
		if input.NativeOutbound != nil {
			rec, err = ParseNativeOutbound(input.NativeOutbound, input.Name, id, source)
		} else {
			rec, err = ParseLink(input.Link, id, source)
		}
		if err == nil && len(input.ClientFields) > 0 {
			rec, err = ApplyClientFields(rec, input.ClientFields)
		}
	}
	if err != nil {
		return nil, true, die("неправильные параметры сервера Clash")
	}
	addNativeImportWarning(rec, clashimport.DocumentWarning)
	for _, warning := range input.Warnings {
		addNativeImportWarning(rec, warning)
	}
	return rec, true, nil
}
