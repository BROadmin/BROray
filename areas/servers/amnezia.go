package servers

import (
	"encoding/json"

	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/cgi"
)

const amneziaUnavailable = "Конфигурация AmneziaWG сохранена. Подключение пока не поддерживается BROray."

func parseAmnezia(link, id string, source map[string]any) (Record, *ParseError) {
	parsed, err := amnezia.ParseLink(link)
	if err != nil {
		return nil, die(err.Error())
	}
	return buildAmneziaRecord(parsed, link, id, source)
}

// ParseAmneziaProfile consumes a detached, typed native container. The stored
// model is revalidated before it can be staged or written.
func ParseAmneziaProfile(parsed amnezia.Imported, id string, source map[string]any) (Record, error) {
	rec, err := buildAmneziaRecord(parsed, "", id, source)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func buildAmneziaRecord(parsed amnezia.Imported, link, id string, source map[string]any) (Record, *ParseError) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	if source == nil {
		source = map[string]any{"type": "manual"}
	}
	// The stored form preserves both the original and parsed fields, so its
	// independent size bound must be checked before preview reports success.
	model, err := json.Marshal(parsed.Profile)
	if err != nil || len(model) > 2*amnezia.MaxProfileBytes {
		return nil, die("AmneziaWG: превышен размер сохраняемого профиля")
	}
	rec := normalize(Record{"schemaVersion": 2, "id": id, "name": parsed.Name, "source": cloneSource(source), "uri": link,
		"protocol": "amneziawg", "address": parsed.Address, "port": parsed.Port, "network": "udp", "security": "amneziawg", "amneziawg": parsed.Profile})
	if link == "" {
		delete(rec, "uri")
	}
	if err := validateAmnezia(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func parseAmneziaDocument(text, id string, source map[string]any) (Record, bool, *ParseError) {
	profiles, recognized, err := amnezia.ParseDocument([]byte(text), 1)
	if !recognized {
		return nil, false, nil
	}
	if err == amnezia.ErrTooManyContainers {
		return nil, true, die("для ручного импорта нужен один клиентский контейнер; список добавьте как подписку")
	}
	if err != nil {
		return nil, true, die("неправильная клиентская конфигурация AmneziaWG")
	}
	rec, parseErr := buildAmneziaRecord(profiles[0], "", id, source)
	return rec, true, parseErr
}

func validateAmnezia(rec Record) *ParseError {
	bad := func() *ParseError {
		return die("неправильная сохранённая конфигурация AmneziaWG")
	}
	if !jsonEq(rec["schemaVersion"], 2) || !ValidID(asString(rec["id"])) || !isText(rec["name"]) || !isObject(rec["source"]) || !nonEmptyString(get(rec, "source", "type")) || rec["network"] != "udp" || rec["security"] != "amneziawg" {
		return bad()
	}
	b, err := json.Marshal(rec["amneziawg"])
	if err != nil || len(b) > 2*amnezia.MaxProfileBytes {
		return bad()
	}
	var p amnezia.Profile
	if json.Unmarshal(b, &p) != nil {
		return bad()
	}
	address, port, err := p.Validate()
	if err != nil || rec["address"] != address || !jsonEq(rec["port"], port) {
		return bad()
	}
	return nil
}

// Connection support is a runtime capability, not a persisted property of the
// profile. A future backend can consume the same stored model.
func connectionSupport(rec Record) map[string]any {
	if has(rec, "importProfile") {
		message := importedProfileUnavailable
		if validateUnavailableProfile(rec) != nil {
			message = "Сохранённая конфигурация импортированного сервера не прошла проверку."
		}
		return map[string]any{"supported": false, "code": "PROTOCOL_NOT_SUPPORTED", "message": message}
	}
	if message := clientTLSUnavailable(rec); message != "" {
		return map[string]any{"supported": false, "code": "PROTOCOL_NOT_SUPPORTED", "message": message}
	}
	if message := wireGuardUnavailable(rec); message != "" {
		return map[string]any{"supported": false, "code": "PROTOCOL_NOT_SUPPORTED", "message": message}
	}
	if rec["protocol"] == "amneziawg" {
		return map[string]any{"supported": false, "code": "PROTOCOL_NOT_SUPPORTED", "message": amneziaUnavailable}
	}
	if rec["protocol"] == "http" {
		return map[string]any{"supported": true, "udp": false, "message": "Прокси HTTP/HTTPS: только TCP. UDP не поддерживается."}
	}
	return map[string]any{"supported": true, "code": nil, "message": nil}
}

func requireConnectionSupport(rec Record) error {
	if has(rec, "importProfile") {
		if err := validateUnavailableProfile(rec); err != nil {
			return err
		}
		return cgi.E(400, "PROTOCOL_NOT_SUPPORTED", importedProfileUnavailable)
	}
	if message := clientTLSUnavailable(rec); message != "" {
		return cgi.E(400, "PROTOCOL_NOT_SUPPORTED", message)
	}
	if message := wireGuardUnavailable(rec); message != "" {
		return cgi.E(400, "PROTOCOL_NOT_SUPPORTED", message)
	}
	if rec["protocol"] == "amneziawg" {
		return cgi.E(400, "PROTOCOL_NOT_SUPPORTED", amneziaUnavailable)
	}
	return nil
}
