package routes

import (
	"net/netip"
	"sort"
	"strings"

	modroutes "github.com/BROadmin/broray/modules/routes"
)

// Built-in route catalogs. 3.x discovered "all .bat files of the directory at
// the newest commit" through api.github.com; 3.5.0 downloads the files from
// raw.githubusercontent.com at the pinned commit of RockBlack-VPN/ip-address
// and verifies each file against its pinned SHA-256 and size (product
// decision: pinned catalog). The optional scheduled check (schedule.go) may
// replace the pinned listing of a bundle with the listing of the newest
// commit read from the GitHub API; the pinned commit stays the baseline.
const (
	catalogRepository = "RockBlack-VPN/ip-address"
	catalogBranch     = "main"
	catalogCommit     = "65a895d1e8cf30653e94a00f2e374991a6838e36"
	// committer date of catalogCommit as the GitHub API reports it.
	catalogCommitDate = "2026-10-31T11:34:17Z"
)

type pinnedFile struct {
	Name   string
	SHA256 string
	Size   int
	GitSHA string // git blob SHA-1 (what the GitHub contents API lists)
}

type builtin struct {
	ID             string
	Name           string
	Directory      string
	MaxSourceBytes int
	MaxRoutes      int
	Files          []pinnedFile // every .bat of the directory at catalogCommit
	ExtraFiles     []string     // non-BAT files of the 3.x manifest (domain lists, disabled)
}

// builtins is the ONE list of built-in bundles (3.x had 10; 3.5.0 adds
// Claude and Netflix), sorted by display name
// (3.x had a fixed order with Telegram first and marked it bundleType
// "extended"; that distinction is dropped).
var builtins = sortBuiltins([]builtin{
	{ID: "telegram", Name: "Telegram", Directory: "Global/Telegram", MaxSourceBytes: 1048576, MaxRoutes: 2048,
		Files: []pinnedFile{{"telegram.bat", "6c59ca39cd62acf90c1bdc8f6264bedd07807366e0d54aa9533fc730d4cda226", 1860, "de89496ec0224e2e02b88dc6d1be1020867dd7e4"}}, ExtraFiles: []string{"telegram_domain"}},
	{ID: "whatsapp", Name: "WhatsApp", Directory: "Global/Whatsapp", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"Whatsapp.bat", "1e4d6286808702909380f4fe94037014b93fe496d38406aca314963bb085030a", 2436, "11e5370e541cd0d326f8bf5f42a9b6ba19fe221c"}}},
	{ID: "youtube", Name: "YouTube", Directory: "Global/Youtube", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"Youtube.bat", "54a8d3bd93678d5db9e6cda28ae6dfa60b16eb7917f941b9d7ea65ff2f3715a4", 6599, "2833efaf334557d113355674575448d30d84f822"}}, ExtraFiles: []string{"Youtube_domain"}},
	{ID: "chatgpt", Name: "ChatGPT", Directory: "Global/ChatGPT(OpenAI)", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"ChatGPT(OpenAI)_0.0.6.bat", "d8db5002b1128c161bc82cd7604aace91d86b4e6ccedf92a9b466dfadff9e9ce", 3986, "5b223f36fa11945a90ceeaad22c5b183b637d281"}}, ExtraFiles: []string{"ChatGPT_domain"}},
	{ID: "facebook", Name: "Facebook", Directory: "Global/Facebook", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"Facebook.bat", "4c7dc3e8c97b6771cb14a0a682f34ee0885e0eb7a7f71679995be91cbbd2a346", 6203, "171ed579463734a1b769c48c877d9b3bca21ccf3"}}},
	{ID: "instagram", Name: "Instagram", Directory: "Global/Instagram", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"Instagram.bat", "b0eabab8142a4974f5a837c9324ca512f3b5271e7ba7677f37d5b780eebdcf86", 3194, "d95cc1a4e4644df5738c07a906a072b49aa9b11f"}}},
	{ID: "meta", Name: "Meta", Directory: "Global/Meta", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"Meta.bat", "9cc3d9089c0c572dcf6c163cc2a8daf1b0f76598cb4d664589cc81ace3b4de58", 1385, "c70d26f92b3e37a60c54e6388a472a9c49ebbb84"}}},
	{ID: "tiktok", Name: "TikTok", Directory: "Global/TikTok", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"TikTok_0.0.3.bat", "7ccc5b496224f4c822e9b60c1c228eef6cb929cb64edaf086c0a1b99a6b8f26e", 10095, "522fe7dd3eee07d7f326cef5a04becce2e800cb6"}}, ExtraFiles: []string{"TikTok_domain"}},
	{ID: "speedtest", Name: "Speedtest", Directory: "Global/speedtest", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"speedtest.bat", "5fdaf2b5ca48e68b86358203a18a5c107a61e951888b7002e0ade33c747c0a1f", 11363, "d43d048427f92ed44dd42334fe555612de2def7d"}}},
	{ID: "claude", Name: "Claude", Directory: "Global/claude", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"claude.bat", "8cbd1ffbd602242e6d2e44a422ebd202e228ab1a7fee7245fc50f6e61317dde5", 2339, "96c668d0c8db1a20c4333c022ce018ad12863ad0"}}, ExtraFiles: []string{"claude_domain"}},
	{ID: "netflix", Name: "Netflix", Directory: "Global/Netflix", MaxSourceBytes: 4194304, MaxRoutes: 10000,
		Files: []pinnedFile{{"Netflix.bat", "104b65f64f27f82ab858df1e24a6c337418734a84d3760730b81859b68638113", 13052, "60bc052e77cad6b7595e3f569cf7d536cf8664cc"}}, ExtraFiles: []string{"Netflix_domain"}},
	{ID: "wikipedia", Name: "Wikipedia", Directory: "Global/wikipedia", MaxSourceBytes: 65536, MaxRoutes: 256,
		Files: []pinnedFile{{"wikipedia.bat", "a704788b81fd068e1fe45f20a1491864a9916e65f65062f0d5812c5711021700", 593, "32f09bea1fd6e380d0b285cb769ae1098b24b7a6"}}},
})

func sortBuiltins(list []builtin) []builtin {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := strings.ToLower(list[i].Name), strings.ToLower(list[j].Name)
		if a != b {
			return a < b
		}
		return list[i].ID < list[j].ID
	})
	for i := range list {
		sort.SliceStable(list[i].Files, func(a, b int) bool {
			return strings.ToLower(list[i].Files[a].Name) < strings.ToLower(list[i].Files[b].Name)
		})
	}
	return list
}

func findBuiltin(id string) (builtin, bool) {
	for _, b := range builtins {
		if b.ID == id {
			return b, true
		}
	}
	return builtin{}, false
}

func builtinIDs() []string {
	out := make([]string, len(builtins))
	for i, b := range builtins {
		out[i] = b.ID
	}
	return out
}

func isBuiltinID(id string) bool { _, ok := findBuiltin(id); return ok }

// manifestFor is the 3.x manifest shape with targetInterface replaced by
// the managed interface (lib/package-setup.sh:384-400).
func (b builtin) manifestFor(iface string) obj {
	files := []any{}
	for _, f := range b.Files {
		files = append(files, obj{"name": f.Name, "type": "windows-route-bat", "required": true, "enabled": true})
	}
	for _, f := range b.ExtraFiles {
		files = append(files, obj{"name": f, "type": "domain-list", "required": false, "enabled": false})
	}
	return obj{
		"schemaVersion": 1, "id": b.ID, "name": b.Name,
		"source": obj{"provider": "github", "repository": catalogRepository, "branch": catalogBranch, "commit": catalogCommit,
			"directory": b.Directory, "files": files, "discovery": obj{"mode": "all-bat", "recursive": false}},
		"targetInterface": iface, "exportComment": "BROray",
		"limits": obj{"maxSourceBytes": b.MaxSourceBytes, "maxRoutes": b.MaxRoutes},
	}
}

func rawURL(base, commit, dir, file string) string {
	return strings.TrimRight(base, "/") + "/" + catalogRepository + "/" + commit + "/" + escapePath(dir) + "/" + escapePath(file)
}

func htmlURL(commit, dir, file string) string {
	return "https://github.com/" + catalogRepository + "/blob/" + commit + "/" + dir + "/" + file
}

// catalogSource is the listing a bundle is checked and downloaded from: the
// pinned one or the one of a newer commit found by the scheduled check.
type catalogSource struct {
	Commit string
	Date   string
	Pinned bool
	Files  []sourceRef
}

// sourceRef is one .bat file of a listing. A file is accepted when its size
// matches and its SHA-256 (pinned listing, a checked version) or its git
// blob SHA-1 (GitHub contents API listing) matches.
type sourceRef struct {
	Name   string
	Size   int
	SHA256 string
	GitSHA string
}

func (b builtin) pinnedSource() catalogSource {
	src := catalogSource{Commit: catalogCommit, Date: catalogCommitDate, Pinned: true}
	for _, f := range b.Files {
		src.Files = append(src.Files, sourceRef{Name: f.Name, Size: f.Size, SHA256: f.SHA256, GitSHA: f.GitSHA})
	}
	return src
}

func (s catalogSource) names() []string {
	out := make([]string, len(s.Files))
	for i, f := range s.Files {
		out[i] = f.Name
	}
	return out
}

func (s catalogSource) ref(name string) (sourceRef, bool) {
	for _, f := range s.Files {
		if f.Name == name {
			return f, true
		}
	}
	return sourceRef{}, false
}

// sameFiles compares two listings by name, size and git blob SHA.
func (s catalogSource) sameFiles(o catalogSource) bool {
	if len(s.Files) != len(o.Files) {
		return false
	}
	for _, f := range s.Files {
		g, ok := o.ref(f.Name)
		if !ok || g.Size != f.Size || g.GitSHA == "" || g.GitSHA != f.GitSHA {
			return false
		}
	}
	return true
}

// escapePath escapes characters that are not safe in a URL path segment
// (spaces, '#', '?', '%'); parentheses are kept like GitHub links.
func escapePath(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteString("%20")
		case c == '#':
			b.WriteString("%23")
		case c == '?':
			b.WriteString("%3F")
		case c == '%':
			b.WriteString("%25")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// filterBogons drops networks overlapping private/special ranges
// (modules/routes IsBogon) from a built-in catalog list and returns them.
func filterBogons(list []string) (kept, dropped []string) {
	for _, r := range list {
		p, err := netip.ParsePrefix(r)
		if err == nil && modroutes.IsBogon(p) {
			dropped = append(dropped, r)
			continue
		}
		kept = append(kept, r)
	}
	return kept, dropped
}
