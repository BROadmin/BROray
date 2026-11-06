package routes

import (
	"sort"
	"strconv"
	"strings"
)

// batParse is the 3.x Windows "route add" BAT parser
// (lib/routes-user-import.sh:180-336 = contract A.R1; the catalog variant
// lib/routes-source-check.sh:203-423 differs only in the default-route
// message and keeps every error line).
type batParse struct {
	Routes          []string // "network/prefix" per valid route line, in file order (not de-duplicated)
	RouteLines      int
	NormalizedLines int
	Errors          []string // "Строка N: reason" (at most maxErrors kept)
	ErrorCount      int
}

const (
	msgOnlyRouteAdd   = "разрешена только команда route add <IPv4> mask <маска> 0.0.0.0"
	msgOnlyRem        = "после маршрута разрешён только комментарий & rem"
	msgBadIPv4        = "некорректный IPv4-адрес сети"
	msgBadMask        = "некорректная или непоследовательная маска"
	msgOnlyGateway    = "разрешён только шлюз 0.0.0.0"
	msgDefaultCustom  = "маршрут по умолчанию 0.0.0.0/0 запрещён"
	msgDefaultCatalog = "маршрут по умолчанию запрещён"
)

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// awkRecords splits like awk with RS="\n": a trailing newline does not make
// an extra empty record.
func awkRecords(data []byte) []string {
	s := string(data)
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func trimSpTab(s string) string { return strings.Trim(s, " \t") }

func splitSpTab(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' })
}

// keyword matches awk `lower ~ /^kw([ \t]|$)/`.
func keyword(lower, kw string) bool {
	if !strings.HasPrefix(lower, kw) {
		return false
	}
	rest := lower[len(kw):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t'
}

func skipLine(line string) bool {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "::") {
		return true
	}
	l := asciiLower(line)
	for _, kw := range []string{"rem", "echo", "@echo", "chcp", "title", "pause", "cls", "setlocal", "endlocal"} {
		if keyword(l, kw) {
			return true
		}
	}
	// ^exit[ \t]+/b([ \t]|$)
	if strings.HasPrefix(l, "exit") {
		rest := l[4:]
		t := strings.TrimLeft(rest, " \t")
		if len(t) < len(rest) && keyword(t, "/b") {
			return true
		}
	}
	return false
}

// octetValue is awk valid_octet: /^[0-9]+$/ and 0 <= value <= 255.
func octetValue(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	t := strings.TrimLeft(s, "0")
	if len(t) > 3 {
		return 0, false
	}
	if t == "" {
		return 0, true
	}
	n, _ := strconv.Atoi(t)
	return n, n <= 255
}

func parseIPv4Octets(s string) ([4]int, bool) {
	var out [4]int
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return out, false
	}
	for i, p := range parts {
		v, ok := octetValue(p)
		if !ok {
			return out, false
		}
		out[i] = v
	}
	return out, true
}

var maskBitsByOctet = map[int]int{255: 8, 254: 7, 252: 6, 248: 5, 240: 4, 224: 3, 192: 2, 128: 1, 0: 0}

// maskPrefix is awk mask_prefix: -1 for an invalid or non-contiguous mask.
func maskPrefix(mask string) int {
	oct, ok := parseIPv4Octets(mask)
	if !ok {
		return -1
	}
	prefix, zeroSeen := 0, false
	for _, o := range oct {
		bits, ok := maskBitsByOctet[o]
		if !ok {
			return -1
		}
		if zeroSeen && bits != 0 {
			return -1
		}
		if bits < 8 {
			zeroSeen = true
		}
		prefix += bits
	}
	return prefix
}

func normalizedNetwork(ip, mask [4]int) string {
	var parts [4]string
	for i := 0; i < 4; i++ {
		o := ip[i]
		if mask[i] != 255 {
			block := 256 - mask[i]
			o = (ip[i] / block) * block
		}
		parts[i] = strconv.Itoa(o)
	}
	return strings.Join(parts[:], ".")
}

// parseBat parses one BAT file. catalog selects the catalog wording of the
// default-route error; maxErrors limits the kept error lines (custom: 20).
func parseBat(data []byte, catalog bool, maxErrors int) batParse {
	var r batParse
	report := func(nr int, msg string) {
		if maxErrors <= 0 || len(r.Errors) < maxErrors {
			r.Errors = append(r.Errors, "Строка "+strconv.Itoa(nr)+": "+msg)
		}
		r.ErrorCount++
	}
	for i, raw := range awkRecords(data) {
		nr := i + 1
		line := trimSpTab(strings.TrimSuffix(raw, "\r"))
		if skipLine(line) {
			continue
		}
		f := splitSpTab(line)
		if len(f) < 6 || asciiLower(f[0]) != "route" || asciiLower(f[1]) != "add" || asciiLower(f[3]) != "mask" {
			report(nr, msgOnlyRouteAdd)
			continue
		}
		if len(f) > 6 && (len(f) < 8 || f[6] != "&" || asciiLower(f[7]) != "rem") {
			report(nr, msgOnlyRem)
			continue
		}
		ip, ok := parseIPv4Octets(f[2])
		if !ok {
			report(nr, msgBadIPv4)
			continue
		}
		prefix := maskPrefix(f[4])
		if prefix < 0 {
			report(nr, msgBadMask)
			continue
		}
		if f[5] != "0.0.0.0" {
			report(nr, msgOnlyGateway)
			continue
		}
		mask, _ := parseIPv4Octets(f[4])
		network := normalizedNetwork(ip, mask)
		if network == "0.0.0.0" && prefix == 0 {
			if catalog {
				report(nr, msgDefaultCatalog)
			} else {
				report(nr, msgDefaultCustom)
			}
			continue
		}
		r.RouteLines++
		if network != f[2] {
			r.NormalizedLines++
		}
		r.Routes = append(r.Routes, network+"/"+strconv.Itoa(prefix))
	}
	return r
}

// ---------- CIDR helpers ----------

func ipToNum(s string) (uint32, bool) {
	oct, ok := parseIPv4Octets(s)
	if !ok {
		return 0, false
	}
	return uint32(oct[0])<<24 | uint32(oct[1])<<16 | uint32(oct[2])<<8 | uint32(oct[3]), true
}

func numToIP(n uint32) string {
	return strconv.Itoa(int(n>>24)) + "." + strconv.Itoa(int(n>>16&255)) + "." + strconv.Itoa(int(n>>8&255)) + "." + strconv.Itoa(int(n&255))
}

func maskString(prefix int) string {
	var v uint32
	if prefix > 0 {
		v = ^uint32(0) << (32 - prefix)
	}
	return numToIP(v)
}

func splitCIDR(s string) (string, int, bool) {
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return "", 0, false
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p < 0 || p > 32 {
		return "", 0, false
	}
	return s[:i], p, true
}

type cidr struct {
	n uint32
	p int
}

func blockSize(p int) uint64 { return uint64(1) << uint(32-p) }

// collapseCIDR is the lossless aggregation of contract A.R2
// (lib/routes-user-import.sh:341-439): drop networks covered by a broader
// kept network, merge exact siblings from /32 up to /1 (never /0), sort by
// numeric network then prefix.
func collapseCIDR(canonical []string) []string {
	cands := map[cidr]bool{}
	byPrefix := make([][]cidr, 33)
	for _, s := range canonical {
		ip, p, ok := splitCIDR(s)
		if !ok {
			continue
		}
		n, ok := ipToNum(ip)
		if !ok {
			continue
		}
		c := cidr{n, p}
		if !cands[c] {
			cands[c] = true
			byPrefix[p] = append(byPrefix[p], c)
		}
	}
	kept := map[cidr]bool{}
	for p := 0; p <= 32; p++ {
		for _, c := range byPrefix[p] {
			covered := false
			for q := 0; q < p; q++ {
				parent := uint32(uint64(c.n) / blockSize(q) * blockSize(q))
				if kept[cidr{parent, q}] {
					covered = true
					break
				}
			}
			if !covered {
				kept[c] = true
			}
		}
	}
	for p := 32; p >= 2; p-- {
		block := blockSize(p)
		parents := map[uint32]bool{}
		for c := range kept {
			if c.p != p {
				continue
			}
			parents[uint32(uint64(c.n)/(block*2)*(block*2))] = true
		}
		for left := range parents {
			right := uint32(uint64(left) + block)
			if kept[cidr{left, p}] && kept[cidr{right, p}] {
				delete(kept, cidr{left, p})
				delete(kept, cidr{right, p})
				kept[cidr{left, p - 1}] = true
			}
		}
	}
	list := make([]cidr, 0, len(kept))
	for c := range kept {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n < list[j].n
		}
		return list[i].p < list[j].p
	})
	out := make([]string, len(list))
	for i, c := range list {
		out[i] = numToIP(c.n) + "/" + strconv.Itoa(c.p)
	}
	return out
}
