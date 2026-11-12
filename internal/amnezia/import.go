// Package amnezia reads AmneziaWG share profiles without applying them to a
// particular VPN backend. Unknown protocol parameters are preserved, never run.
package amnezia

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxLinkBytes = 65536
const MaxProfileBytes = 256 * 1024

// Profile version 1 is backend-neutral. Parameters retains the original AWG
// container, including last_config and extensions not understood by this build.
// It is private data, like the share URI and WireGuard private/preshared keys.
type Profile struct {
	SchemaVersion int `json:"schemaVersion"`
	// ProtocolVersion is the explicit Mihomo implementation selector, not an
	// invented INI directive or an inferred native AmneziaWG version.
	ProtocolVersion *int                  `json:"protocolVersion,omitempty"`
	Container       string                `json:"container"`
	Interface       map[string][]string   `json:"interface"`
	Peers           []map[string][]string `json:"peers"`
	NativeConfig    string                `json:"nativeConfig"`
	Parameters      map[string]any        `json:"parameters"`
}

type Imported struct {
	Name, Address string
	Port          int
	Profile       Profile
}

func invalid(message string) error { return errors.New("AmneziaWG: " + message) }

func object(data []byte) (map[string]any, error) {
	if !utf8.Valid(data) {
		return nil, invalid("неправильный JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	value, err := jsonValue(d, 0)
	if err != nil {
		return nil, err
	}
	out, ok := value.(map[string]any)
	if !ok {
		return nil, invalid("неправильный JSON")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, invalid("лишние данные после JSON")
	}
	return out, nil
}

// Decode objects without silently overwriting repeated member names. This also
// covers unknown extensions and the separately decoded last_config JSON string.
func jsonValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, invalid("слишком глубокая структура JSON")
	}
	token, err := d.Token()
	if err != nil {
		return nil, invalid("неправильный JSON")
	}
	switch token {
	case json.Delim('{'):
		out := map[string]any{}
		for d.More() {
			token, err := d.Token()
			name, ok := token.(string)
			if err != nil || !ok {
				return nil, invalid("неправильный JSON")
			}
			if _, exists := out[name]; exists {
				return nil, invalid("повторное поле JSON")
			}
			value, err := jsonValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			out[name] = value
		}
		if end, err := d.Token(); err != nil || end != json.Delim('}') {
			return nil, invalid("неправильный JSON")
		}
		return out, nil
	case json.Delim('['):
		out := []any{}
		for d.More() {
			value, err := jsonValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		if end, err := d.Token(); err != nil || end != json.Delim(']') {
			return nil, invalid("неправильный JSON")
		}
		return out, nil
	case json.Delim('}'), json.Delim(']'):
		return nil, invalid("неправильный JSON")
	default:
		return token, nil
	}
}

// ParseLink supports Base64 client JSON, Qt qCompress JSON (big-endian length
// followed by zlib), and provider exports containing a plain AWG INI profile.
func ParseLink(link string) (Imported, error) {
	profiles, err := ParseLinks(link, 1)
	if err != nil {
		return Imported{}, err
	}
	return profiles[0], nil
}

// ParseLinks expands only typed native containers. Every container is validated
// before any result is returned, so a damaged export cannot silently lose nodes.
func ParseLinks(link string, maxNodes int) ([]Imported, error) {
	var out []Imported
	if !strings.HasPrefix(link, "vpn://") || len(link) > MaxLinkBytes {
		return out, invalid("неправильная ссылка или превышен размер")
	}
	encoded := strings.TrimPrefix(link, "vpn://")
	var data []byte
	var err error
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		data, err = enc.Strict().DecodeString(encoded)
		if err == nil {
			break
		}
	}
	if err != nil || len(data) == 0 {
		return out, invalid("неправильная кодировка ссылки")
	}
	if nativeINIHeader(data) {
		native := string(data)
		iface, peers, err := parseNative(native)
		if err != nil {
			return out, err
		}
		awg := false
		for k := range iface {
			if nativeAWGField(k) {
				awg = true
				break
			}
		}
		if !awg {
			return out, invalid("INI-профиль не содержит параметров AmneziaWG")
		}
		address, port, err := endpoint(peers[0]["Endpoint"])
		if err != nil {
			return out, err
		}
		if maxNodes < 1 {
			return nil, ErrTooManyContainers
		}
		return []Imported{{Name: "AmneziaWG", Address: address, Port: port, Profile: Profile{
			SchemaVersion: 1, Container: "amnezia-awg", Interface: iface, Peers: peers, NativeConfig: native,
			Parameters: map[string]any{"container": "amnezia-awg", "sourceFormat": "base64-ini"},
		}}}, nil
	}
	if !bytes.HasPrefix(bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\ufeff"))), []byte("{")) {
		if len(data) < 6 {
			return out, invalid("повреждённый сжатый профиль")
		}
		size := binary.BigEndian.Uint32(data[:4])
		if size == 0 || size > MaxProfileBytes {
			return out, invalid("превышен размер распакованного профиля")
		}
		raw := bytes.NewReader(data[4:])
		zr, err := zlib.NewReader(raw)
		if err != nil {
			return out, invalid("повреждённый сжатый профиль")
		}
		decoded, readErr := io.ReadAll(io.LimitReader(zr, MaxProfileBytes+1))
		closeErr := zr.Close()
		if readErr != nil || closeErr != nil || len(decoded) != int(size) || raw.Len() != 0 {
			return out, invalid("неверный размер или повреждение сжатого профиля")
		}
		data = decoded
	}
	profiles, recognized, err := ParseDocument(data, maxNodes)
	if err != nil {
		return nil, err
	}
	if !recognized {
		return nil, invalid("ожидался документ с клиентскими контейнерами")
	}
	return profiles, nil
}

func parseContainer(container map[string]any, name string) (Imported, error) {
	var out Imported
	kind, _ := container["container"].(string)
	if kind != "amnezia-awg" && kind != "amnezia-awg2" {
		return out, invalid("тип контейнера пока не поддерживается")
	}
	params, ok := container["awg"].(map[string]any)
	if !ok {
		return out, invalid("нет параметров AWG")
	}
	last, ok := params["last_config"].(string)
	if !ok || len(last) > MaxProfileBytes {
		return out, invalid("нет клиентской конфигурации")
	}
	client, err := object([]byte(last))
	if err != nil {
		return out, err
	}
	native, ok := client["config"].(string)
	if !ok {
		return out, invalid("нет текста клиентской конфигурации")
	}
	iface, peers, err := parseNative(native)
	if err != nil {
		return out, err
	}
	address, port, err := endpoint(peers[0]["Endpoint"])
	if err != nil {
		return out, err
	}
	out = Imported{Name: name, Address: address, Port: port, Profile: Profile{SchemaVersion: 1, Container: kind, Interface: iface, Peers: peers, NativeConfig: native, Parameters: container}}
	out.Profile.ProtocolVersion, err = clashProtocolVersion(container)
	if err != nil {
		return Imported{}, err
	}
	return out, nil
}

func clashProtocolVersion(container map[string]any) (*int, error) {
	awg, _ := container["awg"].(map[string]any)
	if awg["sourceFormat"] != "clash-yaml" {
		return nil, nil
	}
	source, _ := awg["clashOptions"].(map[string]any)
	options, _ := source["amnezia-wg-option"].(map[string]any)
	v, exists := options["version"]
	if !exists {
		return nil, nil
	}
	var value int64
	switch x := v.(type) {
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return nil, invalid("неправильная версия протокола")
		}
		value = n
	case float64:
		if x != float64(int64(x)) {
			return nil, invalid("неправильная версия протокола")
		}
		value = int64(x)
	default:
		return nil, invalid("неправильная версия протокола")
	}
	if value < -2147483648 || value > 2147483647 {
		return nil, invalid("неправильная версия протокола")
	}
	n := int(value)
	return &n, nil
}

func nativeINIHeader(data []byte) bool {
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		return line == "[Interface]"
	}
	return false
}

func plain(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

func key(values []string) bool {
	if len(values) != 1 {
		return false
	}
	b, err := base64.StdEncoding.Strict().DecodeString(values[0])
	return err == nil && len(b) == 32 && !bytes.Equal(b, make([]byte, 32))
}

func endpoint(values []string) (string, int, error) {
	if len(values) != 1 {
		return "", 0, invalid("ожидался адрес пира")
	}
	host, port, err := net.SplitHostPort(values[0])
	if err != nil || host == "" {
		return "", 0, invalid("неправильный адрес пира")
	}
	if ip, e := netip.ParseAddr(host); e != nil {
		if len(host) > 253 {
			return "", 0, invalid("неправильный адрес пира")
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", 0, invalid("неправильный адрес пира")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return "", 0, invalid("неправильный адрес пира")
				}
			}
		}
	} else if ip.Zone() != "" {
		return "", 0, invalid("неподдерживаемая зона адреса")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return "", 0, invalid("неправильный порт пира")
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, invalid("неправильный порт пира")
	}
	return host, n, nil
}

func prefixes(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			if _, err := netip.ParsePrefix(strings.TrimSpace(p)); err != nil {
				return false
			}
		}
	}
	return true
}

func parseNative(text string) (map[string][]string, []map[string][]string, error) {
	if len(text) == 0 || len(text) > MaxProfileBytes || !utf8.ValidString(text) {
		return nil, nil, invalid("неправильный размер клиентской конфигурации")
	}
	var iface, current map[string][]string
	var peers []map[string][]string
	for _, line := range strings.Split(strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if len(line) > 16384 || !plain(line) {
			return nil, nil, invalid("неправильная строка конфигурации")
		}
		switch line {
		case "[Interface]":
			if iface != nil || len(peers) != 0 {
				return nil, nil, invalid("повторный или неверный раздел Interface")
			}
			iface = map[string][]string{}
			current = iface
			continue
		case "[Peer]":
			if iface == nil || len(peers) >= 32 {
				return nil, nil, invalid("неправильное число пиров")
			}
			current = map[string][]string{}
			peers = append(peers, current)
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || current == nil || k == "" || v == "" && (len(peers) != 0 || !emptyNativeAWGField(k)) || len(k) > 64 {
			return nil, nil, invalid("неправильное поле конфигурации")
		}
		for _, c := range k {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
				return nil, nil, invalid("неправильное имя поля")
			}
		}
		if len(current[k]) > 0 && k != "Address" && k != "DNS" && k != "AllowedIPs" {
			return nil, nil, invalid("повторное поле конфигурации")
		}
		if len(current) >= 128 || len(current[k]) >= 32 {
			return nil, nil, invalid("слишком много полей")
		}
		current[k] = append(current[k], v)
	}
	if !key(iface["PrivateKey"]) || !prefixes(iface["Address"]) || len(peers) == 0 {
		return nil, nil, invalid("неполная клиентская конфигурация")
	}
	for _, p := range peers {
		if !key(p["PublicKey"]) || !prefixes(p["AllowedIPs"]) {
			return nil, nil, invalid("неполная конфигурация пира")
		}
		if v, ok := p["PresharedKey"]; ok && !key(v) {
			return nil, nil, invalid("неправильный ключ пира")
		}
		if _, _, err := endpoint(p["Endpoint"]); err != nil {
			return nil, nil, err
		}
	}
	return iface, peers, nil
}

// Validate checks a stored model, including consistency with the preserved INI.
// It deliberately does not claim any backend supports these parameters.
func (p Profile) Validate() (string, int, error) {
	if p.SchemaVersion != 1 || (p.Container != "amnezia-awg" && p.Container != "amnezia-awg2") || p.Parameters == nil {
		return "", 0, invalid("неправильная модель профиля")
	}
	if p.ProtocolVersion != nil {
		expected, err := clashProtocolVersion(p.Parameters)
		if err != nil || expected == nil || *expected != *p.ProtocolVersion {
			return "", 0, invalid("версия протокола не совпадает с источником")
		}
	}
	i, peers, err := parseNative(p.NativeConfig)
	if err != nil {
		return "", 0, err
	}
	a, _ := json.Marshal(i)
	b, _ := json.Marshal(p.Interface)
	c, _ := json.Marshal(peers)
	d, _ := json.Marshal(p.Peers)
	if !bytes.Equal(a, b) || !bytes.Equal(c, d) {
		return "", 0, invalid("поля профиля не совпадают с конфигурацией")
	}
	return endpoint(peers[0]["Endpoint"])
}
