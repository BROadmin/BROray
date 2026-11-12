// Package xrayid validates the client ID forms of pinned Xray 26.11.9.
package xrayid

import (
	"crypto/sha1"
	"encoding/hex"
)

// The short-name form is UUIDv5 in the nil namespace, counted in UTF-8 bytes.
// For hexadecimal forms, optional separators occur only at UUID boundaries.
// Unlike the core's permissive trailing-byte quirk, ambiguous suffixes fail.
func Canonical(s string) (string, bool) {
	var raw [16]byte
	if len(s) > 0 && len(s) <= 30 {
		h := sha1.New()
		_, _ = h.Write(raw[:])
		_, _ = h.Write([]byte(s))
		copy(raw[:], h.Sum(nil))
		raw[6] = (raw[6] & 15) | 80
		raw[8] = (raw[8] & 63) | 128
	} else {
		if len(s) < 32 || len(s) > 36 {
			return "", false
		}
		pos := 0
		for _, n := range []int{8, 4, 4, 4, 12} {
			if len(s) > 0 && s[0] == '-' {
				s = s[1:]
			}
			if len(s) < n {
				return "", false
			}
			if _, e := hex.Decode(raw[pos:pos+n/2], []byte(s[:n])); e != nil {
				return "", false
			}
			s = s[n:]
			pos += n / 2
		}
		if s != "" {
			return "", false
		}
	}
	h := hex.EncodeToString(raw[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], true
}
func Valid(s string) bool { _, ok := Canonical(s); return ok }
