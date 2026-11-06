package keenetic

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// R14C01 ProxyN grammar (lib/interface-owner.sh:558-803).

var (
	reProxyN    = regexp.MustCompile(`^Proxy[0-9]+$`)
	reBinding   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._/-]*$`)
	rePriority  = regexp.MustCompile(`^[1-9][0-9]*$`)
	reInterface = regexp.MustCompile(`^interface[ \t]+`)
)

func validName(n string) bool { return reProxyN.MatchString(n) }

func validIPv4(v string) bool {
	p := strings.Split(v, ".")
	if len(p) != 4 {
		return false
	}
	for _, x := range p {
		if x == "" || strings.Trim(x, "0123456789") != "" {
			return false
		}
		n, err := strconv.Atoi(x)
		if err != nil || n > 255 {
			return false
		}
	}
	return true
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

// validDescription is broray_interface_owner_description_valid.
func validDescription(d string) bool {
	if d == "" || utf8.RuneCountInString(d) > 128 {
		return false
	}
	if d != "BROray" && !strings.HasPrefix(d, "BROray - ") {
		return false
	}
	if strings.ContainsAny(d, "\"'\\;`$|&<") || strings.HasSuffix(d, ">") {
		return false
	}
	return !strings.Contains(d, "\n")
}

// descriptionLine is broray_interface_description_config_line.
func descriptionLine(d string) string {
	if d == "BROray" {
		return "    description BROray"
	}
	return `    description "` + d + `"`
}

// descriptionMatches is broray_interface_description_config_matches: the
// literal line, or the quoted form with UTF-8 bytes ≥0x80 written as \xHH.
func descriptionMatches(d, line string) bool {
	if !validDescription(d) {
		return false
	}
	if line == descriptionLine(d) {
		return true
	}
	dec, ok := decodeDescriptionLine(line)
	return ok && dec == d
}

func decodeDescriptionLine(line string) (string, bool) {
	const prefix = `    description "`
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, `"`) || len(line) < len(prefix)+1 {
		if line == "    description BROray" {
			return "BROray", true
		}
		return "", false
	}
	enc := line[len(prefix) : len(line)-1]
	var b []byte
	for i := 0; i < len(enc); i++ {
		if enc[i] != '\\' {
			b = append(b, enc[i])
			continue
		}
		if i+3 >= len(enc) || enc[i+1] != 'x' {
			return "", false
		}
		hx := enc[i+2 : i+4]
		if hx[0] < '8' || !strings.ContainsRune("0123456789abcdef", rune(hx[0])) || !strings.ContainsRune("0123456789abcdef", rune(hx[1])) {
			return "", false
		}
		v, err := strconv.ParseUint(hx, 16, 8)
		if err != nil {
			return "", false
		}
		b = append(b, byte(v))
		i += 3
	}
	return string(b), true
}

// blockResult is the rc of broray_interface_block_from_snapshot.
type blockResult int

const (
	blockOK blockResult = iota
	blockAbsent
	blockMalformed
)

// extractBlock returns the lines of "interface <name>" up to (without) the
// terminating "!". Exactly one unindented parent, exactly one "!", no other
// interface line inside.
func extractBlock(lines []string, name string) ([]string, blockResult) {
	parent := "interface " + name
	parents, terminators := 0, 0
	inside, malformed := false, false
	var out []string
	for _, raw0 := range lines {
		raw := strings.TrimSuffix(raw0, "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == parent {
			parents++
			if raw != parent || inside {
				malformed = true
			}
			inside = true
		}
		if inside && raw != "!" {
			out = append(out, raw)
		}
		if inside && raw == "!" {
			terminators++
			inside = false
		} else if inside && reInterface.MatchString(trimmed) && trimmed != parent {
			malformed = true
		}
	}
	if parents == 0 {
		return nil, blockAbsent
	}
	if malformed || parents != 1 || terminators != 1 || inside {
		return out, blockMalformed
	}
	return out, blockOK
}

// referenceCount is broray_interface_snapshot_reference_count: tokens equal
// to the name (quotes stripped) anywhere in the snapshot.
func referenceCount(lines []string, name string) int {
	n := 0
	for _, l := range lines {
		for _, tok := range strings.Fields(strings.TrimSuffix(l, "\r")) {
			if strings.TrimSuffix(strings.TrimPrefix(tok, `"`), `"`) == name {
				n++
			}
		}
	}
	return n
}

// declarationCount counts exact "interface <name>" lines.
func declarationCount(lines []string, name string) int {
	n := 0
	for _, l := range lines {
		if strings.TrimSuffix(l, "\r") == "interface "+name {
			n++
		}
	}
	return n
}

func bindingValid(line string) bool {
	const p = "    proxy connect via "
	if !strings.HasPrefix(line, p) {
		return false
	}
	tok := line[len(p):]
	return len(tok) >= 1 && len(tok) <= 64 && reBinding.MatchString(tok) && !strings.Contains(tok, "..") && !strings.Contains(tok, "//")
}

// socks5UDPLine is how the running/startup configuration carries
// "interface ProxyN proxy socks5-udp" (KeeneticOS 4.1+). Observed on KN-1811
// / KeeneticOS 5.02.B.2.0-2: this exact line (4-space indent) right after
// "proxy upstream", before "up", in both configurations. The grammar stays
// tolerant: it accepts the line once, anywhere after "proxy protocol
// socks5" and before "up".
const socks5UDPLine = "    proxy socks5-udp"

// stripUDP removes the SOCKS5 UDP line from an interface block. ok=false
// when the line is duplicated or misplaced (before the protocol, after "up").
func stripUDP(block []string) (rest []string, udp bool, ok bool) {
	idx, proto, up := -1, -1, -1
	for i, l := range block {
		switch l {
		case socks5UDPLine:
			if idx >= 0 {
				return nil, false, false
			}
			idx = i
		case "    proxy protocol socks5":
			if proto < 0 {
				proto = i
			}
		case "    up":
			up = i
		}
	}
	if idx < 0 {
		return block, false, true
	}
	if proto < 0 || idx < proto || (up >= 0 && idx > up) {
		return nil, false, false
	}
	return append(append([]string{}, block[:idx]...), block[idx+1:]...), true, true
}

// blockUDP reports whether a block carries SOCKS5 UDP.
func blockUDP(block []string) bool {
	_, udp, ok := stripUDP(block)
	return ok && udp
}

// snapshotUDP reports SOCKS5 UDP for name in one captured configuration.
func snapshotUDP(snapshot []string, name string) bool {
	block, rc := extractBlock(snapshot, name)
	return rc == blockOK && blockUDP(block)
}

// signatureExact is broray_interface_block_signature_exact, extended by the
// optional SOCKS5 UDP line (3.5.0).
func signatureExact(block []string, name, host string, port int, desc string) bool {
	block, _, okUDP := stripUDP(block)
	if !okUDP {
		return false
	}
	if !validName(name) || !validIPv4(host) || !validPort(port) || len(block) < 2 {
		return false
	}
	if !descriptionMatches(desc, block[1]) {
		return false
	}
	upstream := "    proxy upstream " + host + " " + strconv.Itoa(port)
	globals, up, bindings := 0, 0, 0
	invalid := false
	n := len(block)
	for i, line := range block {
		nr := i + 1
		switch {
		case nr == 1:
			if line != "interface "+name {
				invalid = true
			}
		case nr == 2:
		case nr == 3:
			if line != "    security-level public" {
				invalid = true
			}
		case nr == 4 && strings.HasPrefix(line, "    ip global "):
			pr := line[len("    ip global "):]
			if !rePriority.MatchString(pr) || len(pr) > 5 {
				invalid = true
			} else if v, _ := strconv.Atoi(pr); v > 65534 {
				invalid = true
			}
			globals = 1
		case nr == 4+globals:
			if line != "    proxy protocol socks5" {
				invalid = true
			}
		case nr == 5+globals:
			if line != upstream {
				invalid = true
			}
		case nr == 6+globals:
			if line == "    up" {
				up++
			} else if bindingValid(line) {
				bindings++
			} else {
				invalid = true
			}
		case nr == 7+globals:
			if bindings == 1 && line == "    up" {
				up++
			} else {
				invalid = true
			}
		default:
			invalid = true
		}
	}
	validLength := n == 6+globals || n == 7+globals
	return !invalid && validLength && up == 1 && bindings <= 1
}

// legacyBlockExact is broray_interface_legacy_block_exact (schema-1 receipts).
func legacyBlockExact(block []string, name, host string, port int) bool {
	block, _, okUDP := stripUDP(block)
	if !okUDP {
		return false
	}
	if len(block) == 0 || block[0] != "interface "+name {
		return false
	}
	upstream := "    proxy upstream " + host + " " + strconv.Itoa(port)
	descNull, sec, proto, ups, up := false, 0, 0, 0, 0
	for i, l := range block[1:] {
		switch {
		case i == 0 && l == "    description null":
			descNull = true
		case l == "    security-level public":
			sec++
		case l == "    proxy protocol socks5":
			proto++
		case l == upstream:
			ups++
		case l == "    up":
			up++
		default:
			return false
		}
	}
	expected := 5
	if descNull {
		expected = 6
	}
	return len(block) == expected && sec == 1 && proto == 1 && ups == 1 && up == 1
}

// blockSHA256 is broray_interface_block_sha256: the block without the
// Keenetic-owned "ip global" (line 4 only) and "proxy connect via" lines.
// 3.5.0: the SOCKS5 UDP line is excluded too, so receipts written by
// versions that created TCP-only interfaces stay valid after UDP is enabled.
func blockSHA256(block []string) (string, bool) {
	block, _, okUDP := stripUDP(block)
	if !okUDP {
		return "", false
	}
	var b strings.Builder
	globals, bindings := 0, 0
	for i, line := range block {
		if strings.HasPrefix(line, "    ip global ") {
			pr := line[len("    ip global "):]
			globals++
			v, _ := strconv.Atoi(pr)
			if i != 3 || globals > 1 || !rePriority.MatchString(pr) || len(pr) > 5 || v > 65534 {
				return "", false
			}
			continue
		}
		if strings.HasPrefix(line, "    proxy connect via ") {
			bindings++
			if !bindingValid(line) || bindings > 1 {
				return "", false
			}
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), true
}

// sourceExact is broray_interface_source_exact for one captured snapshot.
func sourceExact(snapshot []string, name, host string, port int, desc, wantSHA string) bool {
	block, rc := extractBlock(snapshot, name)
	if rc != blockOK || !signatureExact(block, name, host, port, desc) || declarationCount(snapshot, name) != 1 {
		return false
	}
	sum, ok := blockSHA256(block)
	return ok && (wantSHA == "" || sum == wantSHA)
}

// nameAbsent is broray_interface_source_name_absent for one snapshot.
func nameAbsent(snapshot []string, name string) bool {
	_, rc := extractBlock(snapshot, name)
	return rc == blockAbsent && referenceCount(snapshot, name) == 0
}

// blockDescription decodes line 2 of an interface block.
func blockDescription(block []string) (string, bool) {
	if len(block) < 2 {
		return "", false
	}
	return decodeDescriptionLine(block[1])
}
