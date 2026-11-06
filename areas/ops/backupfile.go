package ops

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"

	"github.com/BROadmin/broray/internal/compat"
)

// Settings backup file format (schemaVersion 1). The file is one JSON
// object, UTF-8, pretty-printed:
//
//	{
//	  "format": "broray-settings-backup",
//	  "schemaVersion": 1,
//	  "brorayVersion": "3.5.0-…",
//	  "createdAt": "2026-11-02T12:00:00Z",          // UTC
//	  "encrypted": true,
//	  "encryption": {
//	    "cipher": "AES-256-GCM",
//	    "kdf": "PBKDF2-HMAC-SHA256",
//	    "iterations": 600000,
//	    "salt": "<base64, 16 random bytes>",
//	    "nonce": "<base64, 12 random bytes>"
//	  },
//	  "ciphertext": "<base64 of GCM(payload) incl. 16-byte tag>"
//	}
//
// key = PBKDF2-HMAC-SHA256(password, salt, iterations, 32 bytes); the GCM
// additional data is compat.Canonical(header), header = every field above
// except "ciphertext", so a changed header (version, date, parameters)
// fails authentication like a changed ciphertext. The plaintext is the
// compact payload JSON. An unencrypted file has "encrypted": false, no
// "encryption"/"ciphertext", and carries "payload" plus "payloadSha256"
// (hex SHA-256 of the compact payload) which only detects accidental damage.
//
// payload = {"format","schemaVersion","brorayVersion","createdAt" (same as
// the header), "parts": {"<part>": <value of compat.BackupPart.Export>}}.

const (
	backupFormat       = "broray-settings-backup"
	backupSchema       = 1
	backupCipher       = "AES-256-GCM"
	backupKDF          = "PBKDF2-HMAC-SHA256"
	backupMinIter      = 100000
	backupMaxIter      = 2000000
	backupMaxFileBytes = 4 << 20
	backupMinPassword  = 8
	backupMaxPassword  = 256
)

// backupIterations is the PBKDF2 work factor of new files (~0.2 s on amd64,
// a few seconds on a mipsel router); tests lower it to backupMinIter.
var backupIterations = 600000

type backupCrypto struct {
	Cipher     string `json:"cipher"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
}

type backupHeader struct {
	Format        string        `json:"format"`
	SchemaVersion int           `json:"schemaVersion"`
	BROrayVersion string        `json:"brorayVersion"`
	CreatedAt     string        `json:"createdAt"`
	Encrypted     bool          `json:"encrypted"`
	Encryption    *backupCrypto `json:"encryption,omitempty"`
}

type backupFile struct {
	backupHeader
	Ciphertext    string          `json:"ciphertext,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	PayloadSHA256 string          `json:"payloadSha256,omitempty"`
}

type backupPayload struct {
	Format        string                     `json:"format"`
	SchemaVersion int                        `json:"schemaVersion"`
	BROrayVersion string                     `json:"brorayVersion"`
	CreatedAt     string                     `json:"createdAt"`
	Parts         map[string]json.RawMessage `json:"parts"`
}

// backupError is a "CODE: message" error of the backup endpoints.
type backupError struct {
	Status        int
	Code, Message string
}

func (e *backupError) Error() string { return e.Code + ": " + e.Message }

func berr(status int, code, msg string) *backupError { return &backupError{status, code, msg} }

var (
	errBackupFormat   = berr(400, "BACKUP_FORMAT_INVALID", "Файл не является резервной копией настроек BROray.")
	errBackupSchema   = berr(400, "BACKUP_SCHEMA_UNSUPPORTED", "Резервная копия создана несовместимой версией BROray.")
	errBackupPassword = berr(400, "BACKUP_PASSWORD_REQUIRED", "Резервная копия зашифрована: введите пароль.")
	errBackupDecrypt  = berr(400, "BACKUP_DECRYPT_FAILED", "Неверный пароль или файл резервной копии повреждён.")
	errBackupDamaged  = berr(400, "BACKUP_DAMAGED", "Файл резервной копии повреждён.")
	errBackupTooLarge = berr(413, "BACKUP_TOO_LARGE", "Файл резервной копии слишком большой (больше 4 МБ).")
)

func validBackupPassword(p string) bool {
	n := utf8.RuneCountInString(p)
	return utf8.ValidString(p) && n >= backupMinPassword && n <= backupMaxPassword
}

func marshalCompact(v any) ([]byte, error) {
	b, err := compat.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(b, "\n"), nil
}

func backupKey(password string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iter, 32)
}

func headerAAD(h backupHeader) ([]byte, error) { return compat.Canonical(h) }

// encodeBackupFile builds the file. password "" → unencrypted.
func encodeBackupFile(p backupPayload, password string) ([]byte, error) {
	plain, err := marshalCompact(p)
	if err != nil {
		return nil, err
	}
	f := backupFile{backupHeader: backupHeader{Format: backupFormat, SchemaVersion: backupSchema, BROrayVersion: p.BROrayVersion,
		CreatedAt: p.CreatedAt}}
	if password == "" {
		f.Payload = plain
		f.PayloadSHA256 = compat.SHA256Hex(plain)
	} else {
		salt, nonce := make([]byte, 16), make([]byte, 12)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
		f.Encrypted = true
		f.Encryption = &backupCrypto{Cipher: backupCipher, KDF: backupKDF, Iterations: backupIterations,
			Salt: base64.StdEncoding.EncodeToString(salt), Nonce: base64.StdEncoding.EncodeToString(nonce)}
		key, err := backupKey(password, salt, backupIterations)
		if err != nil {
			return nil, err
		}
		aad, err := headerAAD(f.backupHeader)
		if err != nil {
			return nil, err
		}
		gcm, err := newGCM(key)
		if err != nil {
			return nil, err
		}
		f.Ciphertext = base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, aad))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// decodeBackupFile validates the file and returns its payload. Errors are
// *backupError; a wrong password and any tampering both end in
// BACKUP_DECRYPT_FAILED (GCM cannot tell them apart).
func decodeBackupFile(b []byte, password string) (backupPayload, bool, error) {
	var p backupPayload
	if len(b) > backupMaxFileBytes {
		return p, false, errBackupTooLarge
	}
	var f backupFile
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&f); err != nil || dec.More() {
		return p, false, errBackupFormat
	}
	if f.Format != backupFormat {
		return p, false, errBackupFormat
	}
	if f.SchemaVersion != backupSchema {
		return p, f.Encrypted, errBackupSchema
	}
	var plain []byte
	if f.Encrypted {
		c := f.Encryption
		if c == nil || c.Cipher != backupCipher || c.KDF != backupKDF || c.Iterations < backupMinIter || c.Iterations > backupMaxIter {
			return p, true, errBackupDamaged
		}
		if password == "" {
			return p, true, errBackupPassword
		}
		salt, err1 := base64.StdEncoding.DecodeString(c.Salt)
		nonce, err2 := base64.StdEncoding.DecodeString(c.Nonce)
		ct, err3 := base64.StdEncoding.DecodeString(f.Ciphertext)
		if err1 != nil || err2 != nil || err3 != nil || len(salt) < 16 || len(salt) > 64 || len(nonce) != 12 || len(ct) < 16 {
			return p, true, errBackupDamaged
		}
		key, err := backupKey(password, salt, c.Iterations)
		if err != nil {
			return p, true, errBackupDamaged
		}
		aad, err := headerAAD(f.backupHeader)
		if err != nil {
			return p, true, errBackupDamaged
		}
		gcm, err := newGCM(key)
		if err != nil {
			return p, true, errBackupDamaged
		}
		if plain, err = gcm.Open(nil, nonce, ct, aad); err != nil {
			return p, true, errBackupDecrypt
		}
	} else {
		if f.Encryption != nil || f.Ciphertext != "" || len(f.Payload) == 0 {
			return p, false, errBackupDamaged
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, f.Payload); err != nil {
			return p, false, errBackupDamaged
		}
		plain = buf.Bytes()
		sum, err := hex.DecodeString(f.PayloadSHA256)
		want := sha256.Sum256(plain)
		if err != nil || !bytes.Equal(sum, want[:]) {
			return p, false, errBackupDamaged
		}
	}
	if err := json.Unmarshal(plain, &p); err != nil {
		return p, f.Encrypted, errBackupDamaged
	}
	if p.Format != f.Format || p.SchemaVersion != f.SchemaVersion || p.BROrayVersion != f.BROrayVersion || p.CreatedAt != f.CreatedAt || p.Parts == nil {
		return p, f.Encrypted, errBackupDamaged
	}
	return p, f.Encrypted, nil
}
