package keenetic

// «Проверить UDP» (3.5.0): which public address UDP leaves from, proved
// with STUN Binding requests in two parts:
//
//	server — STUN through the Xray SOCKS inbound that ProxyN uses as its
//	         upstream (Xray SOCKS UDP + the VPN server): the VPN exit;
//	proxy  — STUN sent by the router itself to an address routed to the
//	         managed ProxyN (router-originated UDP follows `ip route …
//	         ProxyN`, confirmed on KN-1811): Keenetic → ProxyN (SOCKS5 UDP)
//	         → Xray → server. If no BROray route set covers the STUN
//	         address, a TEMPORARY /32 is added to the running configuration
//	         only (never "system configuration save") and removed afterwards;
//	direct — STUN to a different, non-routed server: the WAN address.
//
// The check runs as a background operation under the global fence and s.mu,
// so it never overlaps route installs or ProxyN writes while the temporary
// route exists. A run-file receipt records the temporary route before it is
// added; a leftover (daemon crash) is removed at daemon start and before the
// next check.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/udpprobe"
)

var (
	// STUNRoutedCandidates are tried in order for the server/ProxyN parts:
	// stun.cloudflare.com answered through the VPN on KN-1811.
	STUNRoutedCandidates = []string{"stun.cloudflare.com:3478", "stun.l.google.com:19302", "stun.nextcloud.com:3478", "stun.sipgate.net:3478"}
	// STUNDirectCandidates are tried in order for the WAN comparison (Google
	// STUN did not answer directly from KN-1811, hence the fallbacks).
	STUNDirectCandidates = []string{"stun.nextcloud.com:3478", "stun.sipgate.net:3478", "stun.l.google.com:19302", "stun.cloudflare.com:3478"}
	// STUNAllowLocal admits loopback/private STUN addresses: host tests and
	// dev builds only. On a router a resolved private address is refused so
	// that no LAN address is ever routed into ProxyN.
	STUNAllowLocal = false
	// Probes (replaced by tests and dev builds).
	STUNDirect   = udpprobe.Direct
	STUNViaSOCKS = udpprobe.ViaSOCKS5
	STUNResolve  = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	}

	udpProbeTimeout   = 3 * time.Second
	udpRoutedTimeout  = 5 * time.Second // the decisive probe gets a little longer
	udpResolveTimeout = 5 * time.Second
	udpRouteSettle    = 500 * time.Millisecond
	udpRecoverDelay   = 3 * time.Second
)

const (
	udpHostMask   = "255.255.255.255"
	udpMetric     = "1200" // the BROray route metric; the route is still never saved
	udpPerHostIPs = 2
)

var (
	cgnatPrefix     = netip.MustParsePrefix("100.64.0.0/10")
	benchmarkPrefix = netip.MustParsePrefix("198.18.0.0/15") // also Xray fakedns
)

// stunAddrAllowed: a public unicast IPv4 (any non-special IPv4 in tests).
func stunAddrAllowed(ip netip.Addr) bool {
	if !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return false
	}
	if STUNAllowLocal {
		return true
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnatPrefix.Contains(ip) && !benchmarkPrefix.Contains(ip)
}

// udpRouteCommands are the only two Keenetic writes of the check.
func udpRouteCommands(name string, ip netip.Addr) (add, remove string) {
	return "ip route " + ip.String() + " " + udpHostMask + " " + name + " " + udpMetric,
		"no ip route " + ip.String() + " " + udpHostMask + " " + name
}

// udpRouteCommandAllowed is the stage grammar: exactly a /32 of an allowed
// STUN address to the managed ProxyN, plus the shared routes allowlist.
func udpRouteCommandAllowed(name, ipText, cmd string) bool {
	ip, err := netip.ParseAddr(ipText)
	if err != nil || ip.String() != ipText || !validName(name) || !stunAddrAllowed(ip) {
		return false
	}
	add, remove := udpRouteCommands(name, ip)
	return (cmd == add || cmd == remove) && kn.Allow(kn.AreaRoutes, cmd) == nil
}

// ---------- running-config routes ----------

type cfgRoute struct {
	prefix netip.Prefix
	iface  string // "" for a gateway-only route
}

func maskBits(m netip.Addr) int {
	b := m.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	n := 0
	for n < 32 && v&(1<<(31-n)) != 0 {
		n++
	}
	if n < 32 && v<<n != 0 {
		return -1
	}
	return n
}

func isIPv4(s string) bool { a, err := netip.ParseAddr(s); return err == nil && a.Is4() }

// configRoutes parses the `ip route` lines of a running-config. complete is
// false when a route line is not understood: then no address can be proved
// free of routes and nothing is added.
func configRoutes(lines []string) (routes []cfgRoute, complete bool) {
	complete = true
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 2 || f[0] != "ip" || f[1] != "route" {
			continue
		}
		f = f[2:]
		if len(f) >= 2 && f[0] == "default" {
			// KeeneticOS also spells the default route without address/mask.
			f = append([]string{"0.0.0.0", "0.0.0.0"}, f[1:]...)
		}
		if len(f) < 2 || !isIPv4(f[0]) {
			complete = false
			continue
		}
		network := netip.MustParseAddr(f[0])
		bits, rest := 32, f[1:]
		if isIPv4(rest[0]) {
			if bits = maskBits(netip.MustParseAddr(rest[0])); bits < 0 {
				complete = false
				continue
			}
			rest = rest[1:]
		}
		iface := ""
		if len(rest) > 0 && isIPv4(rest[0]) { // gateway
			rest = rest[1:]
		}
		if len(rest) > 0 && !allDigitsStr(rest[0]) && rest[0] != "auto" && rest[0] != "exclusive" {
			iface = rest[0]
		}
		routes = append(routes, cfgRoute{netip.PrefixFrom(network, bits).Masked(), iface})
	}
	return routes, complete
}

func allDigitsStr(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Route coverage of one address.
const (
	routeFree    = "free"    // no route covers it
	routeOwned   = "owned"   // covered only by routes to the managed ProxyN
	routeForeign = "foreign" // covered by a route to anything else
)

func routeCoverage(routes []cfgRoute, ip netip.Addr, name string) string {
	state := routeFree
	for _, r := range routes {
		if !r.prefix.Contains(ip) {
			continue
		}
		// A static default route (0.0.0.0/0, e.g. a backup WAN) is not a
		// decision about this address: a temporary /32 does not override
		// anything the owner routed on purpose.
		if r.prefix.Bits() == 0 && r.iface != name {
			continue
		}
		if r.iface != name {
			return routeForeign
		}
		state = routeOwned
	}
	return state
}

func hostRoutePresent(lines []string, ip netip.Addr, name string) bool {
	routes, _ := configRoutes(lines)
	for _, r := range routes {
		if r.prefix == netip.PrefixFrom(ip, 32) && r.iface == name {
			return true
		}
	}
	return false
}

// ---------- STUN targets ----------

type stunTarget struct {
	Host string
	IP   netip.Addr
	Port int
}

func (t stunTarget) addr() string { return netip.AddrPortFrom(t.IP, uint16(t.Port)).String() }
func (t stunTarget) label() string {
	if t.Host == t.IP.String() {
		return t.addr()
	}
	return t.Host + " (" + t.addr() + ")"
}

// resolveSTUN resolves host:port candidates now (IPv4 only, at most two
// addresses per host, duplicates dropped).
func resolveSTUN(ctx context.Context, list []string) []stunTarget {
	var out []stunTarget
	seen := map[netip.Addr]bool{}
	for _, hp := range list {
		host, ps, err := net.SplitHostPort(hp)
		port, perr := strconv.Atoi(ps)
		if err != nil || perr != nil || !validPort(port) || ctx.Err() != nil {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, udpResolveTimeout)
		ips, err := STUNResolve(rctx, host)
		cancel()
		if err != nil {
			continue
		}
		n := 0
		for _, ip := range ips {
			ip = ip.Unmap()
			if n == udpPerHostIPs || seen[ip] || !stunAddrAllowed(ip) {
				continue
			}
			seen[ip] = true
			out = append(out, stunTarget{host, ip, port})
			n++
		}
	}
	return out
}

// ---------- result ----------

type udpPart struct {
	Ran    bool     `json:"ran"`
	OK     bool     `json:"ok"`
	Target string   `json:"target,omitempty"`
	Exit   string   `json:"exit,omitempty"`
	Error  string   `json:"error,omitempty"`
	Text   string   `json:"text"`
	Tried  []string `json:"tried,omitempty"`
}

type udpRouteInfo struct {
	Mode    string `json:"mode"` // none | existing | temporary
	Command string `json:"command,omitempty"`
	Added   bool   `json:"added"`
	Removed bool   `json:"removed"`
	Text    string `json:"text,omitempty"`
}

type udpCheckResult struct {
	Verdict       string       `json:"verdict"` // UDP_VIA_BROray | BYPASS_WAN | PROXY_UDP_FAILED | SERVER_UDP_FAILED | CHECK_FAILED
	Message       string       `json:"message"`
	Severity      string       `json:"severity"` // ok | error | warning
	InterfaceName string       `json:"interfaceName,omitempty"`
	Socks         string       `json:"socks,omitempty"`
	Socks5UDP     bool         `json:"socks5UdpEnabled"`
	Server        udpPart      `json:"server"`
	Proxy         udpPart      `json:"proxy"`
	Direct        udpPart      `json:"direct"`
	Route         udpRouteInfo `json:"route"`
	Warnings      []string     `json:"warnings"`
	CheckedAt     string       `json:"checkedAt"`
}

func (r *udpCheckResult) fail(why string) *udpCheckResult {
	r.Verdict, r.Severity, r.Message = "CHECK_FAILED", "warning", "Не удалось проверить: "+why
	return r
}

func ipOnly(a *net.UDPAddr) string {
	if a == nil {
		return ""
	}
	return a.IP.String()
}

// udpVerdict maps the three parts onto the owner-facing verdict.
func udpVerdict(r *udpCheckResult) {
	name := r.InterfaceName
	if name == "" {
		name = "ProxyN"
	}
	x, p, w := r.Server.Exit, r.Proxy.Exit, r.Direct.Exit
	switch {
	case r.Verdict == "CHECK_FAILED":
	case !r.Server.OK:
		r.Verdict, r.Severity, r.Message = "SERVER_UDP_FAILED", "error", "Не удалось подтвердить UDP через сервер: STUN-проверки через Xray SOCKS UDP не получили ответа"
	case !r.Proxy.Ran:
		r.fail(r.Proxy.Error)
	case !r.Proxy.OK:
		r.Verdict, r.Severity, r.Message = "PROXY_UDP_FAILED", "error", "Не удалось подтвердить UDP через "+name+": STUN-проверки не получили ответа"
		if !r.Socks5UDP {
			r.Message += " (в " + name + " выключен UDP через SOCKS5 — нажмите «Восстановить»)"
		}
	case r.Direct.OK && x == w:
		r.fail("выход сервера " + x + " совпадает с адресом провайдера — пути не различить")
	case p == x:
		r.Verdict, r.Severity, r.Message = "UDP_VIA_BROray", "ok", "UDP идёт через BROray (выход "+x+")"
	case r.Direct.OK && p == w:
		r.Verdict, r.Severity, r.Message = "BYPASS_WAN", "error", "UDP идёт мимо VPN — через провайдера ("+w+")"
	case r.Direct.OK:
		r.fail("адрес выхода " + p + " не совпадает ни с выходом сервера " + x + ", ни с адресом провайдера " + w)
	default:
		r.fail("адрес выхода " + p + " не совпадает с выходом сервера " + x + ", а адрес провайдера определить не удалось")
	}
}

// ---------- temporary route receipt (run/keenetic-udp-check-route.json) ----------

type udpRouteReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Purpose       string `json:"purpose"`
	Network       string `json:"network"`
	Mask          string `json:"mask"`
	Interface     string `json:"interface"`
	Metric        int    `json:"metric"`
	OperationID   string `json:"operationId"`
	CreatedAt     string `json:"createdAt"`
}

const udpReceiptPurpose = "udp-check-temporary-route"

func (s *service) udpReceiptPath() string { return s.d.RunPath("keenetic-udp-check-route.json") }
func (s *service) udpStatePath() string   { return s.d.RunPath("keenetic-udp-check.json") }

// runUDPRoute dispatches one allowlisted route command (no save, ever).
func (s *service) runUDPRoute(ctx context.Context, name string, ip netip.Addr, cmd string) error {
	if !udpRouteCommandAllowed(name, ip.String(), cmd) {
		return errors.New("KEENETIC_COMMAND_REFUSED: " + cmd)
	}
	res, err := s.d.App.T.Parse(ctx, []string{cmd})
	if err != nil {
		return err
	}
	if len(res) != 1 || !res[0].OK {
		msg := "rejected"
		if len(res) == 1 && res[0].Error() != "" {
			msg = res[0].Error()
		}
		return errors.New(cmd + ": " + msg)
	}
	return nil
}

// waitHostRoute re-reads running-config until the /32 is present/absent.
func (s *service) waitHostRoute(ctx context.Context, name string, ip netip.Addr, present bool) bool {
	for attempt := 1; attempt <= convergenceAttempts; attempt++ {
		if lines, err := s.captureConfig(ctx, "running"); err == nil && hostRoutePresent(lines, ip, name) == present {
			return true
		}
		if attempt < convergenceAttempts && sleepCtx(ctx, convergenceDelay) != nil {
			return false
		}
	}
	return false
}

// removeTempRoute removes the temporary /32 and proves it is gone; the
// receipt is dropped only then. The routes summary cache is invalidated.
func (s *service) removeTempRoute(ctx context.Context, name string, ip netip.Addr) error {
	defer func() { _ = os.Remove(s.d.RunPath("routes-router-config-cache.json")) }()
	lines, err := s.captureConfig(ctx, "running")
	if err != nil {
		return err
	}
	if hostRoutePresent(lines, ip, name) {
		_, rm := udpRouteCommands(name, ip)
		if err := s.runUDPRoute(ctx, name, ip, rm); err != nil && !s.waitHostRoute(ctx, name, ip, false) {
			return err
		}
	}
	if !s.waitHostRoute(ctx, name, ip, false) {
		return errors.New("route still present")
	}
	if err := os.Remove(s.udpReceiptPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ownedRouteKey reports whether a BROray route set now owns the /32. A
// present but unreadable registry is ambiguous and must block route removal.
func (s *service) ownedRouteKey(ip netip.Addr, name string) (bool, error) {
	path := s.d.Path("routes", "installed", "routes.json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	m, ok := compat.ReadObject(path)
	if !ok {
		return false, fmt.Errorf("installed route registry is unreadable: %s", path)
	}
	list, ok := m["routes"].([]any)
	if !ok {
		return false, fmt.Errorf("installed route registry has no routes array: %s", path)
	}
	key := "ipv4|" + ip.String() + "/32|" + name + "|"
	for _, x := range list {
		if r, ok := x.(map[string]any); ok {
			if k, _ := r["key"].(string); strings.HasPrefix(k, key) {
				return true, nil
			}
		}
	}
	return false, nil
}

// recoverUDPRoute removes a temporary route left by an interrupted check
// (caller holds the fence and s.mu). No receipt → nothing to do.
func (s *service) recoverUDPRoute(ctx context.Context) error {
	var rc udpRouteReceipt
	if err := compat.ReadJSON(s.udpReceiptPath(), &rc); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("temporary UDP route receipt is unreadable: %w", err)
	}
	ip, err := netip.ParseAddr(rc.Network)
	if err != nil || rc.SchemaVersion != 1 || rc.Purpose != udpReceiptPurpose || rc.Mask != udpHostMask || !validName(rc.Interface) || !stunAddrAllowed(ip) {
		return errors.New("temporary UDP route receipt has invalid ownership fields")
	}
	owned, err := s.ownedRouteKey(ip, rc.Interface)
	if err != nil {
		return err
	}
	if owned {
		if err := os.Remove(s.udpReceiptPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := s.removeTempRoute(ctx, rc.Interface, ip); err != nil {
		s.d.Log.Printf("keenetic: temporary UDP check route %s/32 %s not removed: %v", rc.Network, rc.Interface, err)
		return err
	}
	s.d.Log.Printf("keenetic: removed leftover temporary UDP check route %s/32 %s", rc.Network, rc.Interface)
	return nil
}

// recoverUDPRouteAtStart runs once at daemon start when a receipt exists.
func (s *service) recoverUDPRouteAtStart(ctx context.Context) {
	if _, err := os.Lstat(s.udpReceiptPath()); err != nil {
		return
	}
	for attempt := 1; attempt <= reconcileAttempts; attempt++ {
		if sleepCtx(ctx, udpRecoverDelay) != nil {
			return
		}
		release, ok := s.d.Lock.TryAcquire("keenetic", "keenetic:check-udp-recovery", "keenetic")
		if !ok {
			continue
		}
		s.mu.Lock()
		err := s.recoverUDPRoute(ctx)
		s.mu.Unlock()
		release()
		if err == nil {
			return
		}
		s.d.Log.Printf("keenetic: temporary UDP route recovery deferred: %v", err)
	}
}

// ---------- the check ----------

// probeViaProxy sends STUN to t from the router; for a free address a
// temporary /32 to ProxyN exists only around the probe.
func (s *service) probeViaProxy(ctx context.Context, opID, name string, t stunTarget, coverage string, r *udpCheckResult) {
	r.Proxy.Target = t.label()
	if coverage == routeOwned {
		r.Route = udpRouteInfo{Mode: "existing", Text: "Адрес " + t.IP.String() + " уже направлен в " + name + " набором маршрутов BROray — маршруты не менялись."}
		r.Proxy.Ran = true
		a, err := STUNDirect(t.addr(), udpRoutedTimeout)
		s.setProxyResult(r, name, a, err)
		return
	}
	add, rm := udpRouteCommands(name, t.IP)
	r.Route = udpRouteInfo{Mode: "temporary", Command: add}
	rc := udpRouteReceipt{SchemaVersion: 1, Purpose: udpReceiptPurpose, Network: t.IP.String(), Mask: udpHostMask, Interface: name,
		Metric: 1200, OperationID: opID, CreatedAt: compat.Now()}
	_ = os.MkdirAll(s.d.Root.Run(), 0o700)
	if err := compat.WriteJSON(s.udpReceiptPath(), rc); err != nil {
		r.Proxy.Error = "не удалось записать квитанцию временного маршрута"
		return
	}
	// Removal happens whatever happens next (error, cancel): fresh context.
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		if err := s.removeTempRoute(rctx, name, t.IP); err != nil {
			r.Route.Removed = false
			r.Warnings = append(r.Warnings, "Временный маршрут «"+add+"» не удалось удалить ("+err.Error()+"). BROray удалит его при следующей проверке или запуске; вручную: «"+rm+"».")
			r.Route.Text = "Временный маршрут добавлен, но не удалён."
			return
		}
		r.Route.Removed = true
		if r.Route.Added {
			r.Route.Text = "Временный маршрут " + t.IP.String() + "/32 → " + name + " добавлен только в текущую конфигурацию (без сохранения) и удалён после проверки."
		}
		// Saved configuration must not carry it (BROray never saves here).
		if st, err := s.captureConfig(rctx, "startup"); err == nil && hostRoutePresent(st, t.IP, name) {
			r.Warnings = append(r.Warnings, "Маршрут "+t.IP.String()+"/32 → "+name+" есть в сохранённой конфигурации KeeneticOS (сохранена не BROray).")
		}
	}()
	if err := s.runUDPRoute(ctx, name, t.IP, add); err != nil {
		r.Proxy.Error = "KeeneticOS не добавила временный маршрут (" + err.Error() + ")"
		return
	}
	if !s.waitHostRoute(ctx, name, t.IP, true) {
		r.Proxy.Error = "временный маршрут не появился в конфигурации KeeneticOS"
		return
	}
	r.Route.Added = true
	if sleepCtx(ctx, udpRouteSettle) != nil {
		r.Proxy.Error = "проверка остановлена"
		return
	}
	r.Proxy.Ran = true
	a, err := STUNDirect(t.addr(), udpRoutedTimeout)
	s.setProxyResult(r, name, a, err)
}

func (s *service) setProxyResult(r *udpCheckResult, name string, a *net.UDPAddr, err error) {
	if err != nil {
		r.Proxy.Error = err.Error()
		r.Proxy.Text = "UDP через " + name + ": ответа нет от " + r.Proxy.Target
		return
	}
	r.Proxy.OK, r.Proxy.Exit = true, ipOnly(a)
	r.Proxy.Text = "UDP через " + name + ": выход " + r.Proxy.Exit + " (" + r.Proxy.Target + ")"
}

// udpCheck runs the whole check; the caller holds the global fence.
func (s *service) udpCheck(ctx context.Context, op *compat.Operation) *udpCheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &udpCheckResult{Warnings: []string{}, Route: udpRouteInfo{Mode: "none"}}
	defer func() { r.CheckedAt = compat.Now() }()
	opID := ""
	if op != nil {
		opID = op.ID
		op.Phase("checking")
	}
	if err := s.recoverUDPRoute(ctx); err != nil {
		return r.fail("не удалось удалить временный маршрут прошлой проверки (" + err.Error() + ")")
	}
	sn := s.capture(ctx)
	if sn.err != nil {
		return r.fail("конфигурация KeeneticOS недоступна")
	}
	name := s.ownerName(sn)
	if name == "" {
		return r.fail("управляемый ProxyN не создан или не подтверждён как принадлежащий BROray")
	}
	r.InterfaceName = name
	r.Socks5UDP = snapshotUDP(sn.running, name) && snapshotUDP(sn.startup, name)
	host, port, ok := s.socksTarget(ctx)
	if !ok {
		return r.fail("не получены параметры SOCKS Xray")
	}
	socks := net.JoinHostPort(host, strconv.Itoa(port))
	r.Socks = socks
	routes, complete := configRoutes(sn.running)

	// (a) UDP through the server: Xray SOCKS UDP ASSOCIATE.
	r.Server.Ran = true
	var target *stunTarget
	var coverage string
	eligible := 0
	for _, t := range resolveSTUN(ctx, STUNRoutedCandidates) {
		if ctx.Err() != nil {
			return r.fail("проверка остановлена")
		}
		cov := routeCoverage(routes, t.IP, name)
		if cov == routeForeign {
			continue // routed elsewhere: neither added to nor probed through ProxyN
		}
		eligible++
		r.Server.Tried = append(r.Server.Tried, t.label())
		a, err := STUNViaSOCKS(socks, t.addr(), udpProbeTimeout)
		if err != nil {
			r.Server.Error = err.Error()
			continue
		}
		tt := t
		target, coverage = &tt, cov
		r.Server.OK, r.Server.Exit, r.Server.Target, r.Server.Error = true, ipOnly(a), t.label(), ""
		r.Server.Text = "UDP через сервер: выход " + r.Server.Exit + " (" + t.label() + ", через SOCKS Xray " + socks + ")"
		break
	}
	if eligible == 0 {
		return r.fail("не найден доступный STUN-сервер (имена не разрешились или адреса уже направлены другими маршрутами)")
	}
	if target == nil {
		r.Server.Text = "UDP через сервер: Xray SOCKS UDP не ответил (" + socks + ")"
	}

	// Direct comparison: a different STUN address no route covers (WAN).
	r.Direct.Ran = true
	for _, t := range resolveSTUN(ctx, STUNDirectCandidates) {
		if ctx.Err() != nil {
			return r.fail("проверка остановлена")
		}
		if (target != nil && t.IP == target.IP) || !complete || routeCoverage(routes, t.IP, name) != routeFree {
			continue
		}
		r.Direct.Tried = append(r.Direct.Tried, t.label())
		a, err := STUNDirect(t.addr(), udpProbeTimeout)
		if err != nil {
			r.Direct.Error = err.Error()
			continue
		}
		r.Direct.OK, r.Direct.Exit, r.Direct.Target, r.Direct.Error = true, ipOnly(a), t.label(), ""
		r.Direct.Text = "Напрямую через провайдера: " + r.Direct.Exit + " (" + t.label() + ")"
		break
	}
	if !r.Direct.OK {
		r.Direct.Text = "Напрямую через провайдера: STUN-серверы не ответили"
	}

	// (b) UDP through ProxyN.
	switch {
	case target == nil:
		r.Proxy.Error = "UDP через сервер не подтверждён"
		r.Proxy.Text = "UDP через " + name + ": не проверялось — STUN-проверки через сервер не получили ответа"
	case !complete:
		r.Proxy.Error = "в таблице маршрутов KeeneticOS есть строки, которые BROray не распознал"
		r.Proxy.Text = "UDP через " + name + ": не проверялось"
	default:
		if op != nil {
			op.Phase("working")
		}
		s.probeViaProxy(ctx, opID, name, *target, coverage, r)
		if !r.Proxy.Ran && r.Proxy.Text == "" {
			r.Proxy.Text = "UDP через " + name + ": не проверялось"
		}
	}
	udpVerdict(r)
	return r
}

// ---------- endpoints ----------

func (s *service) writeUDPState(running bool, opID string, started time.Time, res *udpCheckResult) {
	st := map[string]any{"running": running, "operationId": opID, "startedAt": compat.StampUTC(started), "finishedAt": nil, "result": res}
	if !running {
		st["finishedAt"] = compat.StampUTC(time.Now())
	}
	_ = os.MkdirAll(s.d.Root.Run(), 0o700)
	_ = compat.WriteJSON(s.udpStatePath(), st)
}

// waitUDPJobs closes admission before waiting, so WaitGroup.Add cannot race
// a shutdown Wait. Each job removes its temporary /32 before Done.
func (s *service) waitUDPJobs(ctx context.Context) error {
	s.udpMu.Lock()
	s.udpStopping = true
	s.udpMu.Unlock()
	done := make(chan struct{})
	go func() {
		s.udpJobs.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}

// handleUDPCheck starts the check in the background (202 + operationId);
// the page follows it with check-udp-status.cgi.
func (s *service) handleUDPCheck(c *cgi.Req) (any, error) {
	if c.Header("X-BROray-Request") != "operations" {
		return nil, cgi.E(403, "CSRF_REJECTED", "Обновите страницу и повторите действие.")
	}
	if !compat.OriginAllowed(c.R) {
		return nil, cgi.E(403, "ORIGIN_REJECTED", "Источник запроса не подтверждён.")
	}
	release, ok := s.d.Lock.TryAcquire("keenetic", "keenetic:check-udp", "keenetic")
	if !ok {
		return nil, cgi.E(409, "OPERATION_BUSY", "Другая конфликтующая операция BROray уже выполняется.")
	}
	s.udpMu.Lock()
	if s.udpStopping {
		s.udpMu.Unlock()
		release()
		return nil, cgi.E(503, "SERVICE_STOPPING", "BROray завершает работу; проверка UDP не запускается.")
	}
	op := s.d.Queue.StartOperation("", "router_operation", "keenetic:check-udp", "USER", "cooperative", "")
	ctx, cancel := context.WithCancel(s.d.Ctx)
	op.SetCancel(cancel)
	started := time.Now()
	s.udpRunning, s.udpOp = true, op.ID
	s.udpJobs.Add(1)
	s.udpMu.Unlock()
	s.writeUDPState(true, op.ID, started, nil)
	go func() {
		defer s.udpJobs.Done()
		defer release()
		defer cancel()
		res := s.udpCheck(ctx, op)
		s.writeUDPState(false, op.ID, started, res)
		s.udpMu.Lock()
		s.udpRunning = false
		s.udpMu.Unlock()
		switch {
		case ctx.Err() != nil:
			op.Finish(context.Canceled)
		case res.Verdict == "CHECK_FAILED" || (res.Route.Added && !res.Route.Removed):
			op.Finish(errors.New("KEENETIC_UDP_CHECK_FAILED: " + res.Message))
		default:
			op.Finish(nil)
		}
	}()
	return map[string]any{"accepted": true, "operation": "check-udp", "operationId": op.ID}, nil
}

func (s *service) handleUDPStatus(c *cgi.Req) (any, error) {
	s.udpMu.Lock()
	running, opID := s.udpRunning, s.udpOp
	s.udpMu.Unlock()
	st, ok := compat.ReadObject(s.udpStatePath())
	if !ok {
		st = map[string]any{"operationId": nil, "startedAt": nil, "finishedAt": nil, "result": nil}
	}
	// A record left "running" by a stopped daemon is not running.
	st["running"] = running
	if running {
		st["operationId"] = opID
	}
	return st, nil
}
