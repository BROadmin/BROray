package xray

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var errDirectEgress = errors.New("DIRECT_EGRESS_UNAVAILABLE: Не удалось однозначно определить WAN-интерфейс для выхода напрямую.")
var directInterfaceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$`)

// DirectInterface reads the same IPv4 default route used for server egress,
// without installing destination routes. Binding the socket avoids more-specific
// ProxyN/t2s routes. A missing binding fails closed in the pinned native Core.
func DirectInterface() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", errDirectEgress
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return "", errDirectEgress
	}
	name, err := parseDirectInterface(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Index <= 0 || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return "", errDirectEgress
	}
	return name, nil
}

func parseDirectInterface(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	best := ""
	var metric uint64
	ambiguous := false
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[1] != "00000000" {
			continue
		}
		if len(fields) < 8 {
			return "", errDirectEgress
		}
		if fields[7] != "00000000" {
			continue
		}
		flags, e1 := strconv.ParseUint(fields[3], 16, 32)
		m, e2 := strconv.ParseUint(fields[6], 10, 32)
		if e1 != nil || e2 != nil || !directInterfaceName.MatchString(fields[0]) {
			return "", errDirectEgress
		}
		if flags&1 == 0 || flags&0x200 != 0 {
			continue
		} // down/reject is not an egress
		if best == "" || m < metric {
			best, metric, ambiguous = fields[0], m, false
		} else if m == metric && fields[0] != best {
			ambiguous = true
		}
	}
	if sc.Err() != nil || best == "" || ambiguous || best == "lo" || strings.HasPrefix(best, "t2s") || strings.HasPrefix(best, "br") {
		return "", errDirectEgress
	}
	return best, nil
}
