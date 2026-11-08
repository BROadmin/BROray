package subscriptions

import (
	"errors"
	"fmt"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

// Node staging: broray_subscription_stage_nodes (svc:969-1106).

const jsonImportWarning = "Из JSON импортируются отдельные серверы. DNS, маршрутизация, балансировка и фрагментация профиля не применяются. Служебные поля email, level=0, REALITY show и gRPC mode=false не переносятся."

// staged is the outcome of the parse step.
type staged struct {
	Received, Parsed, Accepted, Rejected int
	Warnings                             []string
	Records                              []servers.Record // accepted, in node order
}

// denialMarker is broray_subscription_provider_denial_marker.
func denialMarker(rec servers.Record) bool {
	port, ok := num(rec["port"])
	if rec["address"] != "0.0.0.0" || !ok || port != 1 {
		return false
	}
	name := ""
	if v := rec["name"]; truthy(v) {
		name = jqRaw(v)
	}
	if strings.Contains(name, "Приложение не поддерж") {
		return true
	}
	ascii := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(asciiLower(name))
	return strings.Contains(ascii, "appnotsupported") || strings.Contains(ascii, "applicationnotsupported") || strings.Contains(ascii, "enablehwid")
}

// parseError text of a rejected node: last stderr line, ≤180 bytes.
func parseReason(err error) string {
	var pe *servers.ParseError
	reason := ""
	if errors.As(err, &pe) && len(pe.Lines) > 0 {
		reason = pe.Lines[len(pe.Lines)-1]
	}
	reason = cutBytes(reason, 180)
	if reason == "" {
		reason = "формат узла не поддерживается"
	}
	return reason
}

// stageNodes parses every node line into a subscription server record.
// progress(processed) is called before each node and at the end.
func stageNodes(subID string, ex *extracted, enabled bool, progress func(int, int)) (*staged, *subErr) {
	st := &staged{Received: len(ex.Lines)}
	if ex.JSONImport {
		st.Warnings = append(st.Warnings, jsonImportWarning)
	}
	denials := 0
	seen := map[string]bool{}
	reject := func(format string, args ...any) {
		st.Rejected++
		st.Warnings = append(st.Warnings, fmt.Sprintf(format, args...))
	}
	if progress != nil {
		progress(0, len(ex.Lines))
	}
	for i0, line := range ex.Lines {
		if progress != nil {
			progress(i0, len(ex.Lines))
		}
		i := i0 + 1
		if strings.HasPrefix(line, unsupportedMarker+"#") {
			reject("Узел %d: неподдерживаемое поле JSON: %s.", i, safeJSONField(strings.TrimPrefix(line, unsupportedMarker+"#")))
			continue
		}
		if line == unsupportedMarker {
			reject("Узел %d: параметры JSON-конфигурации не поддерживаются.", i)
			continue
		}
		rawID := fmt.Sprintf("subscription-%s-%04d", subID, i)
		rec, err := servers.ParseLink(line, rawID, map[string]any{"type": "subscription", "subscriptionId": subID, "nodeIndex": i})
		if err != nil {
			reason := parseReason(err)
			// 3.x leaves the protocol name empty for unknown schemes; name it
			// (scheme only, never the link) so the owner sees what was refused.
			if strings.Contains(reason, "для протокола  отсутствует импортёр") {
				if sch := linkScheme(line); sch != "" {
					reason = strings.Replace(reason, "для протокола  отсутствует импортёр", "для протокола "+sch+" отсутствует импортёр", 1)
				}
			}
			reject("Узел %d: %s", i, reason)
			continue
		}
		st.Parsed++
		if denialMarker(rec) {
			denials++
			reject("Узел %d: провайдер вернул служебный отказ вместо сервера.", i)
			continue
		}
		key, err := servers.CanonicalKey(rec)
		if err != nil || key == "" {
			reject("Узел %d: не удалось вычислить importKey.", i)
			continue
		}
		stableID := "subscription-" + subID + "-" + key[:16]
		if seen[stableID] {
			reject("Узел %d: дубликат уже полученного сервера.", i)
			continue
		}
		rec["id"] = stableID
		rec["source"] = map[string]any{"type": "subscription", "subscriptionId": subID, "importKey": key,
			"nodeIndex": i, "enabled": enabled, "updatedAt": compat.Stamp(now())}
		if err := servers.Validate(rec, stableID+".json"); err != nil {
			reject("Узел %d: нормализованный сервер не прошёл проверку.", i)
			continue
		}
		seen[stableID] = true
		if rec["protocol"] == "amneziawg" {
			st.Warnings = append(st.Warnings, fmt.Sprintf("Узел %d: конфигурация AmneziaWG сохранена. Подключение пока не поддерживается BROray.", i))
		}
		if warnings, ok := rec["importWarnings"].([]any); ok {
			for _, w := range warnings {
				if text, ok := w.(string); ok {
					st.Warnings = append(st.Warnings, fmt.Sprintf("Узел %d: %s", i, text))
				}
			}
		}
		st.Accepted++
		st.Records = append(st.Records, rec)
	}
	if progress != nil {
		progress(len(ex.Lines), len(ex.Lines))
	}
	if st.Accepted == 0 {
		if denials > 0 {
			return st, fail("SUBSCRIPTION_ACCESS_DENIED", "Провайдер не выдал серверы: приложение или идентификатор устройства не поддержан.")
		}
		return st, fail("NO_VALID_NODES", "Подписка не содержит ни одного поддерживаемого и валидного узла.")
	}
	return st, nil
}

// linkScheme returns the URI scheme of a share link ("tuic", "hysteria", …)
// when it is a plain token, else "".
func linkScheme(line string) string {
	i := strings.Index(line, "://")
	if i <= 0 || i > 20 {
		return ""
	}
	sch := strings.ToLower(line[:i])
	for _, r := range sch {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return ""
		}
	}
	return sch
}
