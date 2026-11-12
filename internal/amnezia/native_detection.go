package amnezia

import "strings"

// HasNativeINIParameters identifies an AmneziaWG INI without validating or
// changing it. Only actual Interface keys count; comments and peer keys do not.
// The native parser still owns duplicate, credential and endpoint validation.
func HasNativeINIParameters(text string) bool {
	if !nativeINIHeader([]byte(text)) {
		return false
	}
	section := ""
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[Interface]" {
			continue
		}
		key, _, found := strings.Cut(line, "=")
		if found && nativeAWGField(strings.TrimSpace(key)) {
			return true
		}
	}
	return false
}
