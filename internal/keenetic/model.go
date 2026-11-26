package keenetic

import (
	"regexp"
	"strings"
)

var routerModelCode = regexp.MustCompile(`^(KN|NC)-[0-9]{4}$`)

// IsRouterModelCode recognizes canonical Keenetic and Netcraze hardware codes.
func IsRouterModelCode(code string) bool {
	return routerModelCode.MatchString(code)
}

// RouterModelSuffix extracts a hardware code at the end of a model display name.
func RouterModelSuffix(model string) string {
	model, found := strings.CutSuffix(model, ")")
	if !found {
		return ""
	}
	index := strings.LastIndexByte(model, '(')
	if index >= 0 && IsRouterModelCode(model[index+1:]) {
		return model[index+1:]
	}
	return ""
}
