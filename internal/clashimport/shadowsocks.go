package clashimport

import (
	"encoding/base64"
	"strings"
)

// Mihomo's pinned go.mod selects MetaCubeX/sing-shadowsocks2 v0.2.8.
// cipher/method_registry.go performs an exact-name lookup; these names are
// registered by cipher/method_none.go and the three method.go implementations.
// Source validity and target cipher availability are separate checks.
func validateShadowsocksSource(p map[string]any) {
	method := stringField(p, "cipher", "")
	need(inList(method,
		"none",
		"aes-128-gcm", "aes-192-gcm", "aes-256-gcm",
		"chacha20-ietf-poly1305", "xchacha20-ietf-poly1305",
		"chacha8-ietf-poly1305", "xchacha8-ietf-poly1305", "rabbit128-poly1305",
		"aes-128-ccm", "aes-192-ccm", "aes-256-ccm",
		"aes-128-gcm-siv", "aes-256-gcm-siv", "aegis-128l", "aegis-256",
		"aez-384", "deoxys-ii-256-128", "lea-128-gcm", "lea-192-gcm", "lea-256-gcm", "ascon128", "ascon128a",
		"aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
		"rc4-md5", "chacha20-ietf", "xchacha20", "chacha20",
		"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
		"2022-blake3-chacha8-poly1305", "2022-blake3-aes-128-ccm", "2022-blake3-aes-256-ccm"))
	password := stringField(p, "password", "")
	if method == "none" {
		return
	}
	need(password != "")
	if !strings.HasPrefix(method, "2022-") {
		return
	}
	keys := strings.Split(password, ":")
	if strings.Contains(method, "chacha") {
		need(len(keys) == 1)
	}
	keySize := 32
	if strings.HasPrefix(method, "2022-blake3-aes-128-") {
		keySize = 16
	}
	for _, key := range keys {
		decoded, err := base64.StdEncoding.DecodeString(key)
		need(err == nil && len(decoded) == keySize)
	}
}
