package keenetic

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/areas/xray"
	"github.com/BROadmin/broray/internal/compat"
)

var statusCacheTTL = 30 * time.Second

func (s *service) statusFile() string { return s.d.RunPath("keenetic-status.json") }

// expected is broray_keenetic_read_expected_json.
type expected struct {
	Ready         bool
	InterfaceName string
	Description   string
	Upstream      string
	Listen        string
	Port          int
	Active        bool
	ServerName    string
}

func (e expected) object() map[string]any {
	return map[string]any{"ready": e.Ready, "interfaceName": e.InterfaceName, "description": e.Description, "protocol": "socks5", "socks5Udp": true,
		"upstream": e.Upstream, "socks": map[string]any{"listen": e.Listen, "port": e.Port, "active": e.Active},
		"activeServer": map[string]any{"name": e.ServerName}}
}

var reCollapse = regexp.MustCompile(`[ \t\n\r\v\f]+`)

// sanitizeServerName: whitespace collapsed and trimmed, `" \ ; | & ` $ < >` removed.
func sanitizeServerName(n string) string {
	n = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(n)
	n = strings.TrimSpace(reCollapse.ReplaceAllString(n, " "))
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("\"\\;|&`$<>", r) {
			return -1
		}
		return r
	}, n)
}

// activeServerName is `.name // .id` of the active server record.
func (s *service) activeServerName() string {
	b, err := os.ReadFile(s.d.Path("config", "active-server"))
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		return ""
	}
	m, ok := compat.ReadObject(s.d.Path("servers", id+".json"))
	if !ok {
		return ""
	}
	name, _ := m["name"].(string)
	if name == "" {
		name, _ = m["id"].(string)
	}
	return sanitizeServerName(name)
}

// socksTarget is the ProxyN upstream: the SOCKS inbound of the active Xray
// config (3.5.0 reads only d.Root.XrayConfig(); 3.x mixed settings.json and
// config.json). An unspecified listen address is replaced by the LAN IP.
func (s *service) socksTarget(ctx context.Context) (string, int, bool) {
	listen, port, ok := xray.SocksInbound(s.d.Root.XrayConfig())
	if !ok || !validPort(port) {
		return "", 0, false
	}
	if listen == "" || listen == "0.0.0.0" {
		lan, err := s.d.App.LAN(ctx)
		if err != nil {
			return "", 0, false
		}
		listen = lan
	}
	if !validIPv4(listen) {
		return "", 0, false
	}
	return listen, port, true
}

// expectedDescription is broray_interface_expected_description (interface layer).
func (s *service) expectedDescription() (string, bool) {
	n := s.activeServerName()
	if n == "" {
		return "BROray", true
	}
	d := "BROray - " + n
	return d, validDescription(d)
}

func (s *service) readExpected(ctx context.Context, name string) expected {
	e := expected{InterfaceName: name}
	if host, port, ok := s.socksTarget(ctx); ok {
		e.Listen, e.Port = host, port
		e.Active = s.d.Svc.IsXrayRunning() && xray.SocksListening(host, port)
	}
	e.ServerName = s.activeServerName()
	if e.ServerName != "" {
		if d := "BROray - " + e.ServerName; validDescription(d) {
			e.Description = d
		}
	}
	if e.Listen != "" && e.Port > 0 {
		e.Upstream = e.Listen + ":" + strconv.Itoa(e.Port)
	}
	e.Ready = e.Listen != "" && e.Port > 0 && e.ServerName != "" && e.Description != ""
	return e
}

// readActual is broray_keenetic_read_actual_json.
func (s *service) readActual(ctx context.Context, sn snapshots, name string) map[string]any {
	exists := declarationCount(sn.running, name) > 0
	a := map[string]any{"exists": exists, "interfaceName": name, "description": "", "type": "", "protocol": "", "upstream": "", "socks5UdpEnabled": false,
		"link": false, "connected": false, "state": "", "mtu": nil, "via": "", "localAddress": "", "remoteAddress": "", "adminOnly": false,
		"raw": map[string]any{"link": "", "connected": "", "state": ""}}
	if !exists {
		return a
	}
	// SOCKS5 UDP: running and startup configuration must both carry it.
	a["socks5UdpEnabled"] = snapshotUDP(sn.running, name) && snapshotUDP(sn.startup, name)
	kv := map[string]string{}
	if lines, err := s.showText(ctx, "show interface "+name); err == nil {
		kv = keyValues(lines)
	}
	truthy := func(v string, set ...string) bool {
		for _, x := range set {
			if v == x {
				return true
			}
		}
		return false
	}
	a["description"], a["type"] = kv["description"], kv["type"]
	a["link"] = truthy(kv["link"], "up", "yes", "true", "on")
	a["connected"] = truthy(kv["connected"], "up", "yes", "true", "on")
	a["state"] = kv["state"]
	if n, err := strconv.Atoi(kv["mtu"]); err == nil {
		a["mtu"] = n
	}
	a["via"], a["localAddress"], a["remoteAddress"] = kv["via"], kv["local-endpoint-address"], kv["remote-endpoint-address"]
	a["adminOnly"] = truthy(kv["admin-only"], "yes", "true", "on")
	a["raw"] = map[string]any{"link": kv["link"], "connected": kv["connected"], "state": kv["state"]}
	// protocol/upstream from the running-config block (first `interface <name>` to `!`)
	inside := false
	for _, l := range sn.running {
		if l == "interface "+name {
			inside = true
		}
		if !inside {
			continue
		}
		f := strings.Fields(l)
		if len(f) >= 3 && f[0] == "proxy" && f[1] == "protocol" && a["protocol"] == "" {
			a["protocol"] = strings.ToLower(f[2])
		}
		if len(f) >= 4 && f[0] == "proxy" && f[1] == "upstream" && a["upstream"] == "" {
			a["upstream"] = f[2] + ":" + f[3]
		}
		if l == "!" {
			break
		}
	}
	return a
}

var dashes = strings.NewReplacer(" ", " ", "‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "―", "-",
	"−", "-", "﹘", "-", "﹣", "-", "－", "-", "\t", " ", "\r", " ", "\n", " ")
var reDash = regexp.MustCompile(`[ \t\v\f]*-[ \t\v\f]*`)
var reSpaces = regexp.MustCompile(`[ \t\v\f]+`)

// normalizeDescription is broray_keenetic_normalize_description.
func normalizeDescription(d string) string {
	d = dashes.Replace(d)
	d = reDash.ReplaceAllString(d, " - ")
	d = reSpaces.ReplaceAllString(d, " ")
	return strings.TrimSpace(d)
}

func uniqueSorted(in []string) []string {
	m := map[string]bool{}
	out := []string{}
	for _, x := range in {
		if !m[x] {
			m[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// computeStatus is broray_keenetic_status_json (keenetic-page.sh:629-955).
// It returns an error only when the router configuration cannot be read.
func (s *service) computeStatus(ctx context.Context) (map[string]any, error) {
	sn := s.capture(ctx)
	if sn.err != nil {
		return nil, sn.err
	}
	name := s.selectedName(sn)
	exp := s.readExpected(ctx, name)
	act := s.readActual(ctx, sn, name)
	exists := act["exists"] == true
	// jq `.description // "BROray"` keeps an empty string: with no active
	// server the problem texts contain a double space, like 3.x.
	displayName := exp.Description
	descMatch := normalizeDescription(act["description"].(string)) == normalizeDescription(exp.Description)
	protocolMatch := exp.Ready && exists && act["protocol"] == "socks5"
	upstreamMatch := exp.Ready && exists && act["upstream"] == exp.Upstream
	udpOK := exists && act["socks5UdpEnabled"] == true
	// KeeneticOS before 4.1 has no SOCKS5 UDP: TCP-only is all it can do
	udpUnsupported := exists && !udpOK && s.udpUnsupported(ctx)
	linkOK := exists && act["link"] == true
	connectedOK := exists && act["connected"] == true
	stateOK := exists && act["state"] == "up"
	reachable := exp.Active
	matches := exp.Ready && exists && protocolMatch && upstreamMatch && descMatch && (udpOK || udpUnsupported)
	healthy := exists && reachable && linkOK && connectedOK && stateOK
	var problems []string
	add := func(cond bool, text string) {
		if cond {
			problems = append(problems, text)
		}
	}
	add(!exp.Ready, "Не получены параметры SOCKS Xray или имя активного сервера.")
	add(!exists, "Интерфейс "+displayName+" не создан.")
	add(exists && !protocolMatch, "Протокол "+displayName+" не совпадает с ожидаемым.")
	add(exists && !upstreamMatch, "Адрес Xray в "+displayName+" настроен неправильно.")
	add(exists && !descMatch, "Описание "+displayName+" не совпадает с активным сервером.")
	add(exists && !udpOK && !udpUnsupported, "UDP через SOCKS5 в "+displayName+" выключен: звонки, QUIC и другой UDP-трафик не идут через BROray. Нажмите «Исправить».")
	add(udpUnsupported, udpUnsupportedMsg)
	add(!reachable, "Прокси Xray недоступен.")
	add(exists && !linkOK, "Связь интерфейса "+displayName+" не установлена.")
	add(exists && !connectedOK, "Интерфейс "+displayName+" не подключён.")
	add(exists && !stateOK, "Интерфейс "+displayName+" не работает.")

	ownershipConfirmed := false
	if exists {
		_, ownershipConfirmed = s.ownerValid(sn, name)
	}
	receiptPresent := s.receiptPresent(name)
	if exists && !ownershipConfirmed {
		healthy, matches = false, false
		add(true, "Интерфейс "+displayName+" не подтверждён как принадлежащий BROray.")
	}
	updatedAt := compat.Now()
	var udpCheck any = udpOK
	if udpUnsupported {
		udpCheck = nil // not a mismatch BROray can fix (keenetic.js: !== false)
	}
	severity := "ok"
	switch {
	case !exp.Ready || !exists || !ownershipConfirmed || !healthy:
		severity = "error"
	case !matches, udpUnsupported:
		severity = "warning"
	}
	// nothing to fix on the BROray side for a firmware without SOCKS5 UDP
	actionRequired := severity != "ok" && !(severity == "warning" && matches && udpUnsupported)
	type reason struct{ code, msg string }
	var rs []reason
	if !exp.Ready {
		rs = append(rs, reason{"KEENETIC_EXPECTED_UNAVAILABLE", "Не получены параметры SOCKS Xray или активного сервера."})
	}
	if !exists {
		rs = append(rs, reason{"PROXY0_MISSING", "Интерфейс " + displayName + " не создан."})
	}
	if exists && !ownershipConfirmed {
		rs = append(rs, reason{"PROXY0_OWNERSHIP_UNCONFIRMED", "Интерфейс " + displayName + " не подтверждён как принадлежащий BROray."})
	}
	if !reachable {
		rs = append(rs, reason{"XRAY_SOCKS_UNAVAILABLE", "Прокси Xray недоступен."})
	}
	if exists && (!protocolMatch || !upstreamMatch || !descMatch) {
		rs = append(rs, reason{"PROXY0_CONFIGURATION_MISMATCH", "Настройки " + displayName + " отличаются от ожидаемых."})
	}
	if udpUnsupported {
		rs = append(rs, reason{"PROXY_SOCKS5_UDP_UNSUPPORTED", udpUnsupportedMsg})
	} else if exists && !udpOK {
		rs = append(rs, reason{"PROXY_SOCKS5_UDP_DISABLED", "UDP через SOCKS5 в " + displayName + " выключен: звонки и другой UDP-трафик не идут через BROray."})
	}
	if exists && (!linkOK || !connectedOK || !stateOK) {
		rs = append(rs, reason{"PROXY0_DOWN", "Интерфейс " + displayName + " не находится в рабочем состоянии."})
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].code < rs[j].code })
	reasons := []any{}
	for _, r := range rs {
		reasons = append(reasons, map[string]any{"code": r.code, "message": r.msg, "details": nil})
	}
	var state any = act["state"]
	if state == "" {
		state = nil
	}
	return map[string]any{
		"exists": exists, "healthy": healthy, "matchesExpected": matches, "interfaceName": name,
		"description": act["description"], "protocol": act["protocol"], "upstream": act["upstream"], "link": act["link"],
		"connected": act["connected"], "state": act["state"], "updatedAt": updatedAt, "expectedReady": exp.Ready,
		"upstreamReachable": reachable, "expected": exp.object(), "actual": act,
		"socks5UdpEnabled": act["socks5UdpEnabled"],
		// Additive (3.5.0): false once KeeneticOS rejected "proxy socks5-udp" as unknown
		"socks5UdpSupported": !udpUnsupported,
		// Additive (3.5.0): IPv6 is informational and never changes severity
		// or actionRequired — BROray only warns about it (owner decision).
		"ipv6": s.ipv6Fact(ctx),
		"checks": map[string]any{"exists": exists, "protocol": protocolMatch, "upstream": upstreamMatch, "description": descMatch, "socks5Udp": udpCheck,
			"upstreamReachable": reachable, "link": linkOK, "connected": connectedOK, "state": stateOK,
			"ownership": !exists || ownershipConfirmed},
		"technical": map[string]any{"type": act["type"], "mtu": act["mtu"], "via": act["via"], "localAddress": act["localAddress"],
			"remoteAddress": act["remoteAddress"], "adminOnly": act["adminOnly"]},
		"problems":           uniqueSorted(problems),
		"ownershipConfirmed": ownershipConfirmed, "ownershipReceiptPresent": receiptPresent,
		"interfaceType": "managed-socks-proxy", "interfaceDisplayName": displayName,
		"health": map[string]any{"schemaVersion": 1, "module": "keenetic", "availability": "available", "severity": severity,
			"operational": healthy, "consistent": matches && (!exists || ownershipConfirmed), "actionRequired": actionRequired,
			"freshness": map[string]any{"state": "fresh", "checkedAt": updatedAt}, "reasons": reasons,
			"facts": map[string]any{"interfaceName": name, "exists": exists, "ownershipConfirmed": ownershipConfirmed,
				"ownershipReceiptPresent": receiptPresent, "socksActive": reachable, "link": act["link"], "connected": act["connected"],
				"state": state, "matchesExpected": matches, "socks5UdpEnabled": act["socks5UdpEnabled"]},
			"lastOperation": nil},
	}, nil
}

// status computes and records run/keenetic-status.json.
func (s *service) status(ctx context.Context) (map[string]any, error) {
	st, err := s.computeStatus(ctx)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(s.d.Root.Run(), 0o700)
	_ = compat.WriteJSON(s.statusFile(), st)
	return st, nil
}

// statusCached returns run/keenetic-status.json while younger than 30 s.
func (s *service) statusCached(ctx context.Context) (map[string]any, error) {
	if st, err := os.Lstat(s.statusFile()); err == nil && st.Mode().IsRegular() && st.Size() > 0 && time.Since(st.ModTime()) < statusCacheTTL {
		if m, ok := compat.ReadObject(s.statusFile()); ok {
			return m, nil
		}
	}
	return s.status(ctx)
}
