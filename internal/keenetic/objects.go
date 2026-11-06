package keenetic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// Owner is the string every BROray receipt carries.
const Owner = "BROray"

// Adapter is the typed, receipt-keeping view of the Keenetic objects BROray
// owns: one ProxyN interface, static routes with one metric, DoT upstreams and
// the KeenDNS web application. Nothing without a receipt is ever modified.
type Adapter struct {
	T        Transport
	Receipts string // directory for receipts
	Metric   int
	AppName  string // KeenDNS web app name ("broray")
}

func NewAdapter(t Transport, receiptsDir string, metric int) *Adapter {
	return &Adapter{T: t, Receipts: receiptsDir, Metric: metric, AppName: "broray"}
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---------- Proxy interface ----------

// InterfaceReceipt is state/keenetic/interface.json. Field names follow the
// 3.x config/interface.json so migrate-legacy can map one to the other.
type InterfaceReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Contract      string `json:"contract"`
	Owner         string `json:"owner"`
	InterfaceName string `json:"interfaceName"`
	Protocol      string `json:"protocol"`
	Upstream      struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"upstream"`
	Description     string `json:"description"`
	SelectionMode   string `json:"selectionMode"` // allocated | adopted-receipt | repaired
	RunningBlockSHA string `json:"runningBlockSha256"`
	UpdatedAt       string `json:"updatedAt"`
}

func (a *Adapter) interfacePath() string { return filepath.Join(a.Receipts, "interface.json") }

func (a *Adapter) LoadInterface() (InterfaceReceipt, bool) {
	var r InterfaceReceipt
	if err := platform.ReadJSON(a.interfacePath(), &r); err != nil {
		return r, false
	}
	return r, r.Owner == Owner && ValidProxyName(r.InterfaceName)
}

// InterfaceState is what running-config says about an interface block.
type InterfaceState struct {
	Name        string `json:"name"`
	Exists      bool   `json:"exists"`
	Description string `json:"description"`
	Protocol    string `json:"protocol"`
	Upstream    string `json:"upstream"`
	Up          bool   `json:"up"`
	Connected   bool   `json:"connected"`
	BlockSHA    string `json:"blockSha256"`
	Matches     bool   `json:"matchesReceipt"`
}

func parseInterfaceBlock(b Block) InterfaceState {
	st := InterfaceState{Name: strings.TrimPrefix(b.Header, "interface "), Exists: true, BlockSHA: sha(b.Text())}
	for _, l := range b.Body {
		switch {
		case strings.HasPrefix(l, "description "):
			st.Description = strings.Trim(strings.TrimPrefix(l, "description "), "\"")
		case strings.HasPrefix(l, "proxy protocol "):
			st.Protocol = strings.TrimPrefix(l, "proxy protocol ")
		case strings.HasPrefix(l, "proxy upstream "):
			st.Upstream = strings.TrimPrefix(l, "proxy upstream ")
		case l == "up":
			st.Up = true
		}
	}
	return st
}

// InterfaceStatus reads the running configuration and the live connection
// state of the receipt's interface (or of the candidate name).
func (a *Adapter) InterfaceStatus(ctx context.Context) (InterfaceState, InterfaceReceipt, error) {
	rec, hasRec := a.LoadInterface()
	lines, err := a.T.RunningConfig(ctx)
	if err != nil {
		return InterfaceState{}, rec, err
	}
	blocks := ParseBlocks(lines)
	name := rec.InterfaceName
	if !hasRec {
		return InterfaceState{}, rec, nil
	}
	b, ok := FindBlock(blocks, "interface "+name)
	if !ok {
		return InterfaceState{Name: name}, rec, nil
	}
	st := parseInterfaceBlock(b)
	st.Matches = st.Protocol == "socks5" && st.Upstream == rec.Upstream.Host+" "+strconv.Itoa(rec.Upstream.Port)
	var show struct {
		Connected string `json:"connected"`
		State     string `json:"state"`
		Link      string `json:"link"`
	}
	if err := a.T.Show(ctx, "show/interface/"+name, &show); err == nil {
		st.Connected = show.Connected == "yes"
	}
	return st, rec, nil
}

func freeProxyName(blocks []Block, preferred string) (string, error) {
	used := map[string]bool{}
	for _, b := range blocks {
		if strings.HasPrefix(b.Header, "interface Proxy") {
			used[strings.TrimPrefix(b.Header, "interface ")] = true
		}
	}
	if preferred != "" && ValidProxyName(preferred) && !used[preferred] {
		return preferred, nil
	}
	for i := 0; i <= 255; i++ {
		n := "Proxy" + strconv.Itoa(i)
		if !used[n] {
			return n, nil
		}
	}
	return "", errors.New("KEENETIC_NO_FREE_PROXY_INTERFACE")
}

// EnsureInterface creates (or repairs) the owned SOCKS5 proxy interface
// pointing at host:port, in the exact 3.x order, and saves. Description is
// "BROray - <label>" or "BROray". An interface that exists without a receipt is
// never adopted: a fresh name is allocated instead.
func (a *Adapter) EnsureInterface(ctx context.Context, host string, port int, label, preferred string) (InterfaceReceipt, error) {
	if !ValidIPv4(host) || port < 1 || port > 65535 {
		return InterfaceReceipt{}, errors.New("KEENETIC_UPSTREAM_INVALID")
	}
	lines, err := a.T.RunningConfig(ctx)
	if err != nil {
		return InterfaceReceipt{}, err
	}
	blocks := ParseBlocks(lines)
	rec, hasRec := a.LoadInterface()
	desc := Owner
	if label = strings.TrimSpace(label); label != "" {
		desc = Owner + " - " + label
		if len([]rune(desc)) > 128 {
			desc = string([]rune(desc)[:128])
		}
	}
	desc = strings.ReplaceAll(desc, "\"", "")
	var name string
	var cmds []string
	mode := "allocated"
	if hasRec {
		name = rec.InterfaceName
		if b, ok := FindBlock(blocks, "interface "+name); ok {
			st := parseInterfaceBlock(b)
			mode = "repaired"
			if st.Protocol != "socks5" {
				cmds = append(cmds, "interface "+name+" proxy protocol socks5")
			}
			if st.Upstream != host+" "+strconv.Itoa(port) {
				cmds = append(cmds, "interface "+name+" proxy upstream "+host+" "+strconv.Itoa(port), "interface "+name+" proxy connect")
			}
			if st.Description != desc {
				cmds = append(cmds, "interface "+name+" description "+quoteDesc(desc))
			}
			if !st.Up {
				cmds = append(cmds, "interface "+name+" up")
			}
			if len(cmds) == 0 {
				return rec, nil
			}
		} else {
			cmds = nil // receipt exists but object vanished: recreate under the same name
		}
	} else {
		name, err = freeProxyName(blocks, preferred)
		if err != nil {
			return InterfaceReceipt{}, err
		}
	}
	if len(cmds) == 0 {
		cmds = []string{
			"interface " + name,
			"interface " + name + " proxy protocol socks5",
			"interface " + name + " proxy upstream " + host + " " + strconv.Itoa(port),
			"interface " + name + " proxy connect",
			"interface " + name + " description " + quoteDesc(desc),
			"interface " + name + " security-level public",
			"interface " + name + " up",
		}
	}
	if _, err := Apply(ctx, a.T, AreaInterface, cmds); err != nil {
		if !hasRec {
			_, _ = Apply(ctx, a.T, AreaInterface, []string{"no interface " + name})
		}
		return InterfaceReceipt{}, err
	}
	running, err := Save(ctx, a.T)
	if err != nil {
		return InterfaceReceipt{}, err
	}
	b, ok := FindBlock(ParseBlocks(running), "interface "+name)
	if !ok {
		return InterfaceReceipt{}, errors.New("KEENETIC_INTERFACE_NOT_CONFIRMED")
	}
	rec = InterfaceReceipt{SchemaVersion: 3, Contract: "broray-proxy-owned-interface/2", Owner: Owner, InterfaceName: name, Protocol: "socks5", Description: desc, SelectionMode: mode, RunningBlockSHA: sha(b.Text()), UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	rec.Upstream.Host, rec.Upstream.Port = host, port
	if err := platform.WriteJSONAtomic(a.interfacePath(), rec, 0o600); err != nil {
		return rec, err
	}
	return rec, nil
}

func quoteDesc(d string) string {
	if d == Owner {
		return d
	}
	return "\"" + d + "\""
}

// DeleteInterface removes the owned interface (receipt required) and saves.
func (a *Adapter) DeleteInterface(ctx context.Context) error {
	rec, ok := a.LoadInterface()
	if !ok {
		return nil
	}
	lines, err := a.T.RunningConfig(ctx)
	if err != nil {
		return err
	}
	if _, exists := FindBlock(ParseBlocks(lines), "interface "+rec.InterfaceName); exists {
		if _, err := Apply(ctx, a.T, AreaInterface, []string{"no interface " + rec.InterfaceName}); err != nil {
			return err
		}
		if _, err := Save(ctx, a.T); err != nil {
			return err
		}
	}
	return os.Remove(a.interfacePath())
}

// ---------- Static routes ----------

// Route is one IPv4 network routed to the owned interface.
type Route struct {
	Network string `json:"network"`
	Mask    string `json:"mask"`
}

func (r Route) Key() string { return r.Network + " " + r.Mask }

// InstalledRoutes lists routes present in running-config for the given
// interface with BROray's metric. Only exact shape matches count as managed.
func (a *Adapter) InstalledRoutes(ctx context.Context, iface string) (map[string]Route, []string, error) {
	lines, err := a.T.RunningConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	return routesFromLines(lines, iface, a.Metric), lines, nil
}

func routesFromLines(lines []string, iface string, metric int) map[string]Route {
	out := map[string]Route{}
	suffix := " " + iface + " " + strconv.Itoa(metric)
	for _, l := range lines {
		if !strings.HasPrefix(l, "ip route ") || !strings.HasSuffix(l, suffix) {
			continue
		}
		f := strings.Fields(l)
		if len(f) == 6 && ValidIPv4(f[2]) && validMask(f[3]) {
			r := Route{Network: f[2], Mask: f[3]}
			out[r.Key()] = r
		}
	}
	return out
}

// RouteBatch applies adds and removes in batches, re-reading running-config
// after each batch to confirm (16 per batch as in 3.x; RCI tolerates 100).
// It saves once at the end. progress is called after each confirmed batch.
func (a *Adapter) RouteBatch(ctx context.Context, iface string, add, remove []Route, batch int, progress func(done, total int)) error {
	if !ValidProxyName(iface) {
		return errors.New("KEENETIC_INTERFACE_INVALID")
	}
	if batch <= 0 {
		batch = 50
	}
	var cmds []string
	for _, r := range remove {
		cmds = append(cmds, "no ip route "+r.Network+" "+r.Mask+" "+iface)
	}
	for _, r := range add {
		cmds = append(cmds, "ip route "+r.Network+" "+r.Mask+" "+iface+" "+strconv.Itoa(a.Metric))
	}
	total := len(cmds)
	if total == 0 {
		return nil
	}
	for i := 0; i < total; i += batch {
		end := i + batch
		if end > total {
			end = total
		}
		if _, err := Apply(ctx, a.T, AreaRoutes, cmds[i:end]); err != nil {
			return err
		}
		if progress != nil {
			progress(end, total)
		}
	}
	// Confirm from running-config before saving.
	installed, _, err := a.InstalledRoutes(ctx, iface)
	if err != nil {
		return err
	}
	for _, r := range add {
		if _, ok := installed[r.Key()]; !ok {
			return fmt.Errorf("KEENETIC_ROUTE_NOT_CONFIRMED: %s", r.Key())
		}
	}
	for _, r := range remove {
		if _, ok := installed[r.Key()]; ok {
			return fmt.Errorf("KEENETIC_ROUTE_STILL_PRESENT: %s", r.Key())
		}
	}
	_, err = Save(ctx, a.T)
	return err
}

// ---------- DNS-over-TLS ----------

type DoTServer struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	SNI      string `json:"sni"`
	SPKI     string `json:"spki,omitempty"`
}

func (d DoTServer) Key() string { return d.Address + ":" + strconv.Itoa(d.Port) }

type DoTReceipt struct {
	SchemaVersion int         `json:"schemaVersion"`
	Owner         string      `json:"owner"`
	Managed       []DoTServer `json:"managed"`
	UpdatedAt     string      `json:"updatedAt"`
}

func (a *Adapter) dotPath() string { return filepath.Join(a.Receipts, "dot.json") }

func (a *Adapter) LoadDoT() DoTReceipt {
	var r DoTReceipt
	_ = platform.ReadJSON(a.dotPath(), &r)
	return r
}

// LiveDoT lists every "tls upstream" line in the dns-proxy block.
func (a *Adapter) LiveDoT(ctx context.Context) ([]DoTServer, error) {
	lines, err := a.T.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	b, ok := FindBlock(ParseBlocks(lines), "dns-proxy")
	if !ok {
		return nil, nil
	}
	var out []DoTServer
	for _, l := range b.Body {
		f := strings.Fields(l)
		if len(f) >= 3 && f[0] == "tls" && f[1] == "upstream" {
			s := DoTServer{Address: f[2], Port: 853}
			i := 3
			if i < len(f) && validPort(f[i]) {
				s.Port, _ = strconv.Atoi(f[i])
				i++
			}
			for ; i+1 < len(f); i += 2 {
				switch f[i] {
				case "sni":
					s.SNI = f[i+1]
				case "spki":
					s.SPKI = f[i+1]
				}
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// ApplyDoT makes the owned set equal to want: removes owned servers not in
// want, adds missing ones (never touching foreign upstreams), saves and writes
// the receipt. At most 8 upstreams total, like 3.x.
func (a *Adapter) ApplyDoT(ctx context.Context, want []DoTServer) (DoTReceipt, error) {
	rec := a.LoadDoT()
	live, err := a.LiveDoT(ctx)
	if err != nil {
		return rec, err
	}
	liveKeys := map[string]bool{}
	for _, s := range live {
		liveKeys[s.Key()] = true
	}
	wantKeys := map[string]DoTServer{}
	for _, s := range want {
		if !ValidIPv4(s.Address) || s.Port < 1 || s.Port > 65535 || !reSNI.MatchString(s.SNI) {
			return rec, fmt.Errorf("DOT_SERVER_INVALID: %s", s.Key())
		}
		wantKeys[s.Key()] = s
	}
	var cmds []string
	var kept []DoTServer
	for _, s := range rec.Managed {
		if _, keep := wantKeys[s.Key()]; !keep && liveKeys[s.Key()] {
			cmds = append(cmds, "no dns-proxy tls upstream "+s.Address+" "+strconv.Itoa(s.Port))
		}
	}
	foreign := 0
	for _, s := range live {
		owned := false
		for _, m := range rec.Managed {
			if m.Key() == s.Key() {
				owned = true
			}
		}
		if !owned {
			foreign++
		}
	}
	for _, s := range want {
		if !liveKeys[s.Key()] {
			c := "dns-proxy tls upstream " + s.Address + " " + strconv.Itoa(s.Port) + " sni " + s.SNI
			if s.SPKI != "" {
				c += " spki " + s.SPKI
			}
			cmds = append(cmds, c)
		}
		kept = append(kept, s)
	}
	if foreign+len(kept) > 8 {
		return rec, errors.New("DOT_LIMIT_EXCEEDED")
	}
	if len(cmds) > 0 {
		if _, err := Apply(ctx, a.T, AreaDoT, cmds); err != nil {
			return rec, err
		}
		if _, err := Save(ctx, a.T); err != nil {
			return rec, err
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	rec = DoTReceipt{SchemaVersion: 4, Owner: Owner, Managed: kept, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	return rec, platform.WriteJSONAtomic(a.dotPath(), rec, 0o600)
}

// ---------- KeenDNS web application ----------

type WebReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Upstream      struct {
		Scheme string `json:"scheme"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
	} `json:"upstream"`
	Domain    string `json:"domain"`
	UpdatedAt string `json:"updatedAt"`
}

func (a *Adapter) webPath() string { return filepath.Join(a.Receipts, "web-publish.json") }

func (a *Adapter) LoadWeb() (WebReceipt, bool) {
	var r WebReceipt
	if err := platform.ReadJSON(a.webPath(), &r); err != nil {
		return r, false
	}
	return r, r.Owner == Owner
}

// WebStatus reports whether the app block exists and its upstream line.
func (a *Adapter) WebStatus(ctx context.Context) (exists bool, upstream string, err error) {
	lines, err := a.T.RunningConfig(ctx)
	if err != nil {
		return false, "", err
	}
	b, ok := FindBlock(ParseBlocks(lines), "ip http proxy "+a.AppName)
	if !ok {
		return false, "", nil
	}
	for _, l := range b.Body {
		if strings.HasPrefix(l, "upstream ") {
			upstream = strings.TrimPrefix(l, "upstream ")
		}
	}
	return true, upstream, nil
}

func (a *Adapter) EnableWeb(ctx context.Context, lanIP string, port int) (WebReceipt, error) {
	if !ValidIPv4(lanIP) {
		return WebReceipt{}, errors.New("LAN_IP_INVALID")
	}
	exists, _, err := a.WebStatus(ctx)
	if err != nil {
		return WebReceipt{}, err
	}
	if _, owned := a.LoadWeb(); exists && !owned {
		return WebReceipt{}, errors.New("KEENETIC_WEB_APP_FOREIGN")
	}
	n := a.AppName
	cmds := []string{
		"ip http proxy " + n,
		"ip http proxy " + n + " upstream http " + lanIP + " " + strconv.Itoa(port),
		"ip http proxy " + n + " domain ndns",
		"ip http proxy " + n + " ssl redirect",
		"ip http proxy " + n + " security-level public",
	}
	if _, err := Apply(ctx, a.T, AreaWeb, cmds); err != nil {
		return WebReceipt{}, err
	}
	if _, err := Save(ctx, a.T); err != nil {
		return WebReceipt{}, err
	}
	rec := WebReceipt{SchemaVersion: 4, Owner: Owner, Name: n, Domain: "ndns", UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	rec.Upstream.Scheme, rec.Upstream.Host, rec.Upstream.Port = "http", lanIP, port
	return rec, platform.WriteJSONAtomic(a.webPath(), rec, 0o600)
}

func (a *Adapter) DisableWeb(ctx context.Context) error {
	if _, owned := a.LoadWeb(); !owned {
		return nil
	}
	exists, _, err := a.WebStatus(ctx)
	if err != nil {
		return err
	}
	if exists {
		if _, err := Apply(ctx, a.T, AreaWeb, []string{"no ip http proxy " + a.AppName}); err != nil {
			return err
		}
		if _, err := Save(ctx, a.T); err != nil {
			return err
		}
	}
	return os.Remove(a.webPath())
}

// NDNSName returns the router's KeenDNS name (e.g. "myrouter.keenetic.pro").
func (a *Adapter) NDNSName(ctx context.Context) (string, error) {
	var v struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
	}
	if err := a.T.Show(ctx, "show/ndns", &v); err != nil {
		return "", err
	}
	if v.Name == "" || v.Domain == "" {
		return "", nil
	}
	return v.Name + "." + v.Domain, nil
}

// LANAddress returns the IPv4 address of the LAN interface (Bridge0 by default).
func (a *Adapter) LANAddress(ctx context.Context, iface string) (string, error) {
	if iface == "" {
		iface = "Bridge0"
	}
	var v struct {
		Address string `json:"address"`
		Mask    string `json:"mask"`
	}
	if err := a.T.Show(ctx, "show/interface/"+iface, &v); err == nil && ValidIPv4(v.Address) {
		return v.Address, nil
	}
	lines, err := a.T.RunningConfig(ctx)
	if err != nil {
		return "", err
	}
	b, ok := FindBlock(ParseBlocks(lines), "interface "+iface)
	if !ok {
		return "", errors.New("LAN_INTERFACE_NOT_FOUND")
	}
	for _, l := range b.Body {
		f := strings.Fields(l)
		if len(f) >= 4 && f[0] == "ip" && f[1] == "address" && ValidIPv4(f[2]) {
			return f[2], nil
		}
	}
	return "", errors.New("LAN_ADDRESS_NOT_FOUND")
}

// Version returns KeeneticOS release/title/model from show version.
func (a *Adapter) Version(ctx context.Context) (map[string]any, error) {
	var v map[string]any
	if err := a.T.Show(ctx, "show/version", &v); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, k := range []string{"release", "title", "model", "hw_id", "arch", "description", "device"} {
		if x, ok := v[k]; ok {
			out[k] = x
		}
	}
	return out, nil
}
