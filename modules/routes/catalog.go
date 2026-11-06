// Package routes manages route bundles: built-in catalogs downloaded from the
// pinned RockBlack-VPN/ip-address commit, user .bat imports, the bogon filter,
// and applying bundles to the owned Keenetic interface with receipts.
package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
)

// Manifest describes one built-in bundle (3.x share/routes/manifests shape).
type Manifest struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Directory string   `json:"directory"` // under Global/ in the repository
	Files     []string `json:"files"`     // .bat files; pinned at the commit
	MaxRoutes int      `json:"maxRoutes"`
}

// BuiltIn is the dev1 catalog in alphabetical order (no "featured" subset), verified against commit
// 65a895d1e8cf30653e94a00f2e374991a6838e36 on 2026-11-01. The former
// "Global/ AI_Claude" directory (leading space) was merged upstream into
// Global/claude on 2026-10-31.
var BuiltIn = []Manifest{
	{ID: "chatgpt", Name: "ChatGPT", Directory: "Global/ChatGPT(OpenAI)", Files: []string{"ChatGPT(OpenAI)_0.0.6.bat"}, MaxRoutes: 10000},
	{ID: "claude", Name: "Claude", Directory: "Global/claude", Files: []string{"claude.bat"}, MaxRoutes: 10000},
	{ID: "facebook", Name: "Facebook", Directory: "Global/Facebook", Files: []string{"Facebook.bat"}, MaxRoutes: 10000},
	{ID: "instagram", Name: "Instagram", Directory: "Global/Instagram", Files: []string{"Instagram.bat"}, MaxRoutes: 10000},
	{ID: "meta", Name: "Meta", Directory: "Global/Meta", Files: []string{"Meta.bat"}, MaxRoutes: 10000},
	{ID: "netflix", Name: "Netflix", Directory: "Global/Netflix", Files: []string{"Netflix.bat"}, MaxRoutes: 10000},
	{ID: "speedtest", Name: "Speedtest", Directory: "Global/speedtest", Files: []string{"speedtest.bat"}, MaxRoutes: 10000},
	{ID: "telegram", Name: "Telegram", Directory: "Global/Telegram", Files: []string{"telegram.bat"}, MaxRoutes: 2048},
	{ID: "tiktok", Name: "TikTok", Directory: "Global/TikTok", Files: []string{"TikTok_0.0.3.bat"}, MaxRoutes: 10000},
	{ID: "whatsapp", Name: "WhatsApp", Directory: "Global/Whatsapp", Files: []string{"Whatsapp.bat"}, MaxRoutes: 10000},
	{ID: "wikipedia", Name: "Wikipedia", Directory: "Global/wikipedia", Files: []string{"wikipedia.bat"}, MaxRoutes: 256},
	{ID: "youtube", Name: "YouTube", Directory: "Global/Youtube", Files: []string{"Youtube.bat"}, MaxRoutes: 10000},
}

func FindManifest(id string) (Manifest, bool) {
	for _, m := range BuiltIn {
		if m.ID == id {
			return m, true
		}
	}
	return Manifest{}, false
}

// Route is an IPv4 network in Keenetic's dotted mask form.
type Route = keenetic.Route

var bogons = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// IsBogon reports whether the prefix overlaps a private/special range.
func IsBogon(p netip.Prefix) bool {
	for _, b := range bogons {
		if b.Overlaps(p) {
			return true
		}
	}
	return false
}

var reRoute = regexp.MustCompile(`(?i)^\s*route\s+(?:-p\s+)?add\s+(\d+\.\d+\.\d+\.\d+)\s+mask\s+(\d+\.\d+\.\d+\.\d+)\s+(\d+\.\d+\.\d+\.\d+)`)

// ParseReport is the result of parsing one or more .bat sources.
type ParseReport struct {
	Routes     []Route  `json:"routes"`
	Lines      int      `json:"lines"`
	Parsed     int      `json:"parsed"`
	Duplicates int      `json:"duplicates"`
	Bogons     []string `json:"bogons"`
	Invalid    []string `json:"invalid"`
	Truncated  bool     `json:"truncated"`
}

func maskBits(mask string) (int, bool) {
	a, err := netip.ParseAddr(mask)
	if err != nil || !a.Is4() {
		return 0, false
	}
	b := a.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	inv := ^v
	if inv&(inv+1) != 0 {
		return 0, false
	}
	bits := 0
	for v != 0 {
		bits++
		v <<= 1
	}
	return bits, true
}

func maskFromBits(bits int) string {
	v := uint32(0)
	if bits > 0 {
		v = ^uint32(0) << (32 - bits)
	}
	return fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// ParseBat parses Windows "route add" lines. Networks are normalised to the
// network address, duplicates and nested exact copies dropped, bogons and
// 0.0.0.0/0 refused. maxRoutes caps the result (Truncated=true).
func ParseBat(r io.Reader, maxRoutes int) (ParseReport, error) {
	rep := ParseReport{Bogons: []string{}, Invalid: []string{}}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		rep.Lines++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "::") || strings.HasPrefix(strings.ToLower(line), "rem ") || strings.HasPrefix(line, "@") {
			continue
		}
		m := reRoute.FindStringSubmatch(line)
		if m == nil {
			if strings.HasPrefix(strings.ToLower(line), "route ") {
				rep.Invalid = append(rep.Invalid, line)
			}
			continue
		}
		rep.Parsed++
		addr, err := netip.ParseAddr(m[1])
		bits, ok := maskBits(m[2])
		if err != nil || !addr.Is4() || !ok {
			rep.Invalid = append(rep.Invalid, line)
			continue
		}
		p := netip.PrefixFrom(addr, bits).Masked()
		if bits == 0 {
			rep.Invalid = append(rep.Invalid, line+" (0.0.0.0/0 is refused)")
			continue
		}
		if IsBogon(p) {
			rep.Bogons = append(rep.Bogons, p.String())
			continue
		}
		rt := Route{Network: p.Addr().String(), Mask: maskFromBits(bits)}
		if seen[rt.Key()] {
			rep.Duplicates++
			continue
		}
		if maxRoutes > 0 && len(rep.Routes) >= maxRoutes {
			rep.Truncated = true
			continue
		}
		seen[rt.Key()] = true
		rep.Routes = append(rep.Routes, rt)
	}
	if err := sc.Err(); err != nil {
		return rep, err
	}
	sort.Slice(rep.Routes, func(i, j int) bool { return rep.Routes[i].Key() < rep.Routes[j].Key() })
	return rep, nil
}

// Catalog is a downloaded bundle stored at data/routes/catalog/<id>/catalog.json.
type Catalog struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Source       string   `json:"source"` // repo@commit
	Files        []string `json:"files"`
	DownloadedAt string   `json:"downloadedAt"`
	Routes       []Route  `json:"routes"`
	Bogons       []string `json:"bogonsDropped"`
	Duplicates   int      `json:"duplicatesDropped"`
	Invalid      int      `json:"invalidLines"`
	Truncated    bool     `json:"truncated"`
	Kind         string   `json:"kind"` // builtin | user
}

type Store struct {
	Root   platform.Root
	Repo   string
	Commit string
	Client *http.Client
}

func NewStore(root platform.Root, repo, commit string) *Store {
	tr := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 60 * time.Second}
	return &Store{Root: root, Repo: repo, Commit: commit, Client: &http.Client{Transport: tr, Timeout: 5 * time.Minute}}
}

func (s *Store) catalogPath(id string) string {
	return filepath.Join(s.Root.RouteCatalog(), id, "catalog.json")
}

var reID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

func ValidBundleID(id string) bool { return reID.MatchString(id) }

func (s *Store) Load(id string) (Catalog, bool) {
	var c Catalog
	if !ValidBundleID(id) {
		return c, false
	}
	if err := platform.ReadJSON(s.catalogPath(id), &c); err != nil {
		return c, false
	}
	return c, true
}

func (s *Store) Save(c Catalog) error {
	if !ValidBundleID(c.ID) {
		return errors.New("ROUTES_BUNDLE_ID_INVALID")
	}
	dir := filepath.Dir(s.catalogPath(c.ID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return platform.WriteJSONAtomic(s.catalogPath(c.ID), c, 0o600)
}

func (s *Store) Remove(id string) error {
	if !ValidBundleID(id) {
		return errors.New("ROUTES_BUNDLE_ID_INVALID")
	}
	return os.RemoveAll(filepath.Dir(s.catalogPath(id)))
}

// List returns all stored catalogs (built-in and user), sorted by id.
func (s *Store) List() []Catalog {
	entries, _ := os.ReadDir(s.Root.RouteCatalog())
	var out []Catalog
	for _, e := range entries {
		if c, ok := s.Load(e.Name()); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) rawURL(dir, file string) string {
	var segs []string
	for _, seg := range strings.Split(dir, "/") {
		segs = append(segs, url.PathEscape(seg))
	}
	return "https://raw.githubusercontent.com/" + s.Repo + "/" + s.Commit + "/" + strings.Join(segs, "/") + "/" + url.PathEscape(file)
}

// Download fetches a built-in bundle's files at the pinned commit, parses
// them and stores the catalog. The previous catalog stays until the new one
// is fully parsed.
func (s *Store) Download(ctx context.Context, m Manifest) (Catalog, error) {
	var all strings.Builder
	for _, f := range m.Files {
		u := s.rawURL(m.Directory, f)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return Catalog{}, err
		}
		req.Header.Set("User-Agent", "BROray/3.5.0")
		resp, err := s.Client.Do(req)
		if err != nil {
			return Catalog{}, fmt.Errorf("ROUTES_DOWNLOAD_FAILED: %s: %v", f, err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
		resp.Body.Close()
		if err != nil {
			return Catalog{}, fmt.Errorf("ROUTES_DOWNLOAD_FAILED: %s: %v", f, err)
		}
		if resp.StatusCode != http.StatusOK {
			return Catalog{}, fmt.Errorf("ROUTES_DOWNLOAD_FAILED: %s: HTTP %d", f, resp.StatusCode)
		}
		if len(body) > 4<<20 {
			return Catalog{}, fmt.Errorf("ROUTES_SOURCE_TOO_LARGE: %s", f)
		}
		all.Write(body)
		all.WriteString("\n")
	}
	rep, err := ParseBat(strings.NewReader(all.String()), m.MaxRoutes)
	if err != nil {
		return Catalog{}, err
	}
	if len(rep.Routes) == 0 {
		return Catalog{}, errors.New("ROUTES_SOURCE_EMPTY")
	}
	c := Catalog{ID: m.ID, Name: m.Name, Source: s.Repo + "@" + s.Commit, Files: m.Files, DownloadedAt: time.Now().UTC().Format(time.RFC3339),
		Routes: rep.Routes, Bogons: rep.Bogons, Duplicates: rep.Duplicates, Invalid: len(rep.Invalid), Truncated: rep.Truncated, Kind: "builtin"}
	return c, s.Save(c)
}

// ExportBat renders a bundle as a Windows .bat like 3.x routes-export-build.sh.
func ExportBat(c Catalog, metric int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "@echo off\r\n:: BROray 3.5.0 — %s (%s), %d routes, exported %s\r\n", c.Name, c.ID, len(c.Routes), time.Now().UTC().Format(time.RFC3339))
	for _, r := range c.Routes {
		fmt.Fprintf(&b, "route add %s mask %s 0.0.0.0 metric %d :: rem BROray\r\n", r.Network, r.Mask, metric)
	}
	return b.String()
}

// ---------- installed receipt ----------

// Installed is state/keenetic/routes.json: which bundles are applied to which
// interface and the exact routes each one contributed.
type Installed struct {
	SchemaVersion int                `json:"schemaVersion"`
	Owner         string             `json:"owner"`
	Interface     string             `json:"interface"`
	Bundles       map[string][]Route `json:"bundles"`
	UpdatedAt     string             `json:"updatedAt"`
}

func (s *Store) installedPath() string { return filepath.Join(s.Root.Receipts(), "routes.json") }

func (s *Store) LoadInstalled() Installed {
	var in Installed
	_ = platform.ReadJSON(s.installedPath(), &in)
	if in.Bundles == nil {
		in.Bundles = map[string][]Route{}
	}
	return in
}

func (s *Store) SaveInstalled(in Installed) error {
	in.SchemaVersion, in.Owner, in.UpdatedAt = 1, keenetic.Owner, time.Now().UTC().Format(time.RFC3339)
	return platform.WriteJSONAtomic(s.installedPath(), in, 0o600)
}

// Plan computes adds/removes to make bundle id present (apply=true) or absent
// on the interface, honouring routes shared with other installed bundles.
func Plan(in Installed, id string, routes []Route, apply bool) (add, remove []Route) {
	refs := map[string]int{}
	for bid, rs := range in.Bundles {
		if bid == id {
			continue
		}
		for _, r := range rs {
			refs[r.Key()]++
		}
	}
	current := map[string]bool{}
	for _, r := range in.Bundles[id] {
		current[r.Key()] = true
	}
	if apply {
		want := map[string]bool{}
		for _, r := range routes {
			want[r.Key()] = true
			if !current[r.Key()] && refs[r.Key()] == 0 {
				add = append(add, r)
			}
		}
		for _, r := range in.Bundles[id] {
			if !want[r.Key()] && refs[r.Key()] == 0 {
				remove = append(remove, r)
			}
		}
		return add, remove
	}
	for _, r := range in.Bundles[id] {
		if refs[r.Key()] == 0 {
			remove = append(remove, r)
		}
	}
	return nil, remove
}

// Summary is the public view of one bundle for the WebUI.
type Summary struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Downloaded   bool   `json:"downloaded"`
	DownloadedAt string `json:"downloadedAt,omitempty"`
	RouteCount   int    `json:"routeCount"`
	Installed    bool   `json:"installed"`
	InstalledCnt int    `json:"installedCount"`
	LiveCount    int    `json:"liveCount"`
	Bogons       int    `json:"bogonsDropped"`
	Consistent   bool   `json:"consistent"`
}

// Summaries merges manifests, downloaded catalogs, the receipt and the live
// routes seen on the router into one list.
func (s *Store) Summaries(live map[string]Route) []Summary {
	in := s.LoadInstalled()
	cats := map[string]Catalog{}
	for _, c := range s.List() {
		cats[c.ID] = c
	}
	var out []Summary
	seen := map[string]bool{}
	build := func(id, name, kind string) Summary {
		sm := Summary{ID: id, Name: name, Kind: kind}
		if c, ok := cats[id]; ok {
			sm.Downloaded, sm.DownloadedAt, sm.RouteCount, sm.Bogons = true, c.DownloadedAt, len(c.Routes), len(c.Bogons)
			if sm.Name == "" {
				sm.Name = c.Name
			}
		}
		if rs, ok := in.Bundles[id]; ok {
			sm.Installed, sm.InstalledCnt = true, len(rs)
			liveN := 0
			for _, r := range rs {
				if _, ok := live[r.Key()]; ok {
					liveN++
				}
			}
			sm.LiveCount = liveN
			sm.Consistent = live == nil || liveN == len(rs)
		} else {
			sm.Consistent = true
		}
		return sm
	}
	for _, m := range BuiltIn {
		out = append(out, build(m.ID, m.Name, "builtin"))
		seen[m.ID] = true
	}
	for id, c := range cats {
		if !seen[id] {
			out = append(out, build(id, c.Name, c.Kind))
			seen[id] = true
		}
	}
	for id := range in.Bundles {
		if !seen[id] {
			out = append(out, build(id, id, "unknown"))
		}
	}
	return out
}

// JSONRoutes is a helper for API responses.
func JSONRoutes(rs []Route) json.RawMessage {
	b, _ := json.Marshal(rs)
	return b
}
