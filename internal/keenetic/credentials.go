package keenetic

import "strings"

// NormalizeLogin applies BROray's loginInput.value.trim() at the shared input
// boundary, including callers which do not go through the browser (the probe).
// This is ECMAScript whitespace, not Unicode White_Space: FEFF is trimmed, NEL
// is not. Passwords never pass through this function.
func NormalizeLogin(login string) string {
	return strings.TrimFunc(login, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
			'\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
			return true
		}
		return r >= '\u2000' && r <= '\u200a'
	})
}
