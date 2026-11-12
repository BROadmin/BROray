package xrayschema

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"strings"
)

// This checks only inline bytes. A provider file path is never dereferenced.
func validateInlineCertificate(m map[string]any, path string) error {
	lines := func(field string) []byte {
		var out []string
		for _, v := range arrayValue(m[field]) {
			out = append(out, textValue(v))
		}
		return []byte(strings.Join(out, "\n"))
	}
	cert, key := lines("certificate"), lines("key")
	if len(arrayValue(m["key"])) > 0 && len(key) == 0 {
		return invalid(path + ".key")
	}
	if len(arrayValue(m["certificate"])) > 0 {
		rest, count := cert, 0
		for len(strings.TrimSpace(string(rest))) > 0 {
			block, next := pem.Decode(rest)
			if block == nil || block.Type != "CERTIFICATE" {
				return invalid(path + ".certificate")
			}
			if _, err := x509.ParseCertificate(block.Bytes); err != nil {
				return invalid(path + ".certificate")
			}
			rest, count = next, count+1
		}
		if count == 0 {
			return invalid(path + ".certificate")
		}
	}
	if len(key) > 0 {
		if len(cert) > 0 {
			if _, err := tls.X509KeyPair(cert, key); err != nil {
				return invalid(path + ".key")
			}
		} else {
			block, rest := pem.Decode(key)
			if block == nil || len(strings.TrimSpace(string(rest))) > 0 {
				return invalid(path + ".key")
			}
			var parsed any
			var err error
			switch block.Type {
			case "PRIVATE KEY":
				parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
			case "RSA PRIVATE KEY":
				parsed, err = x509.ParsePKCS1PrivateKey(block.Bytes)
			case "EC PRIVATE KEY":
				parsed, err = x509.ParseECPrivateKey(block.Bytes)
			default:
				return invalid(path + ".key")
			}
			if _, ok := parsed.(crypto.Signer); err != nil || !ok {
				return invalid(path + ".key")
			}
		}
	}
	if strings.EqualFold(textValue(m["usage"]), "issue") &&
		(len(cert) == 0 && !active(m["certificateFile"]) || len(key) == 0 && !active(m["keyFile"])) {
		return invalid(path + ".certificate")
	}
	return nil
}

func arrayValue(v any) []any { a, _ := v.([]any); return a }
