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
	return verifyDirectInterface(name)
}

func verifyDirectInterface(name string) (string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Index <= 0 || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return "", errDirectEgress
	}
	return name, nil
}

// DirectIPv6Interface requires an unambiguous, general IPv6 default route.
func DirectIPv6Interface() (string, error) {
	f, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return "", errDirectEgress
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return "", errDirectEgress
	}
	name, err := parseDirectIPv6Interface(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	return verifyDirectInterface(name)
}

func parseDirectIPv6Interface(r io.Reader) (string, error) {
	const zero = "00000000000000000000000000000000"
	sc := bufio.NewScanner(r)
	var metric uint64
	name := ""
	ambiguous := false
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 10 || f[0] != zero || f[1] != "00" || f[2] != zero || f[3] != "00" {
			continue
		}
		flags, flagErr := strconv.ParseUint(f[8], 16, 32)
		m, metricErr := strconv.ParseUint(f[5], 16, 32)
		if flagErr != nil || metricErr != nil || !directInterfaceName.MatchString(f[9]) {
			return "", errDirectEgress
		}
		if flags&1 == 0 || flags&0x200 != 0 {
			continue
		}
		if name == "" || m < metric {
			name, metric, ambiguous = f[9], m, false
		} else if m == metric && f[9] != name {
			ambiguous = true
		}
	}
	if sc.Err() != nil || ambiguous || name == "" || name == "lo" || strings.HasPrefix(name, "t2s") || strings.HasPrefix(name, "br") {
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
