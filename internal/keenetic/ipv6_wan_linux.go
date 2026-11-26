package keenetic

import (
	"bufio"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const maxIPv6RouteTable = 4 << 20

var routeInterface = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)
var proxyRouteInterface = regexp.MustCompile(`^t2s[0-9]+$`)

func noAlternateIPv6WAN() bool {
	table, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return false
	}
	defer table.Close()
	return ipv6WANAbsent(table)
}

// An address on a home segment or ProxyN does not establish a second WAN.
// Treat a possible external route or an incomplete table as inconclusive.
func ipv6WANAbsent(table io.Reader) bool {
	input := &io.LimitedReader{R: table, N: maxIPv6RouteTable + 1}
	rows := bufio.NewScanner(input)
	rows.Buffer(make([]byte, 512), 512)
	for rows.Scan() {
		fields := strings.Fields(rows.Text())
		if len(fields) != 10 || !routeInterface.MatchString(fields[9]) ||
			len(fields[0]) != 32 || len(fields[4]) != 32 {
			return false
		}
		var dst, nextHop [16]byte
		_, dstErr := hex.Decode(dst[:], []byte(fields[0]))
		_, hopErr := hex.Decode(nextHop[:], []byte(fields[4]))
		prefix, prefixErr := strconv.ParseUint(fields[1], 16, 8)
		flags, flagErr := strconv.ParseUint(fields[8], 16, 32)
		if dstErr != nil || hopErr != nil || prefixErr != nil || prefix > 128 || flagErr != nil {
			return false
		}
		const up, reject = 0x1, 0x200
		if flags&up == 0 || flags&reject != 0 || fields[9] == "lo" || proxyRouteInterface.MatchString(fields[9]) {
			continue
		}
		// Source-specific defaults still allow traffic from their own prefix.
		// Public routes via a gateway or split public aggregates may also exit.
		public := dst[0]&0xe0 == 0x20 || (prefix < 3 && dst[0] == 0)
		if prefix == 0 || (public && (prefix < 32 || nextHop != [16]byte{})) {
			return false
		}
	}
	return rows.Err() == nil && input.N > 0
}
