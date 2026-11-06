package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

// Safe cleanup of BROray service files (lib/broray-cleanup.sh) in the 3.5.0
// layout. 3.x paths below /opt/broray are mapped like every area does:
// tmp/ and logs/ stay, run/<X> stays, other <X> live below data/. Candidate
// paths are relative to the installation root. A plan (token, TTL 120 s)
// fixes the exact list; execution re-builds it and refuses on any change.

const cleanupTTL = 120

type cleanupPattern struct {
	category string
	globs    []string // relative to the root
}

// Groups of the "keep newest 3, older than a day" rule (broray-cleanup.sh:228-263).
var backupGroups = [][]string{
	{"data/backup/system-before-*.tar.gz", "data/backup/system-before-reinstall-*.tar.gz"},
	{"data/backup/user-before-*.tar.gz", "data/backup/user-before-reinstall-*.tar.gz"},
	{"data/backup/web-new-before-*.tar.gz", "data/backup/web-before-*.tar.gz"},
	{"data/backup/xray-before-*.tar.gz", "data/backup/xray-binaries/*"},
	{"data/backup/lighttpd.conf.before-*", "data/backup/package-before-*.tar.gz", "data/backup/before-update-*.tar.gz",
		"data/backup/before-restore-*.tar.gz", "data/backup/before-uninstall-*.tar.gz"},
}

var routeBackupGroup = []string{"data/routes/backup/catalog-*", "data/routes/backup/user-removed-*",
	"data/routes/backup/rollback-*", "data/routes/backup/transaction-*"}

var tempGlobs = []string{"tmp/broray-*", "tmp/routes-*", "tmp/.broray-*", "tmp/*.part", "tmp/*.download", "tmp/*.tmp.*",
	"data/routes/tmp/*", "data/routes/transactions/*", "run/broray/*.tmp.*"}

var logGlobs = []string{"logs/*.gz", "logs/*.old", "logs/*.log.[0-9]*", "logs/*.log-*", "run/broray/operation.log.*", "run/broray/*.old"}

// allowed is broray_cleanup_path_allowed: category:kind:relative must match
// the allow-list at execution time.
func cleanupAllowed(category, kind, rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") || strings.ContainsAny(rel, "\t\n\r") {
		return false
	}
	match := func(globs []string, kinds ...string) bool {
		okKind := false
		for _, k := range kinds {
			if k == kind {
				okKind = true
			}
		}
		if !okKind {
			return false
		}
		for _, g := range globs {
			if ok, _ := filepath.Match(g, rel); ok {
				return true
			}
		}
		return false
	}
	switch category {
	case "temp":
		return match([]string{"tmp/broray-*", "tmp/routes-*", "tmp/.broray-*", "tmp/*.part", "tmp/*.download", "tmp/*.tmp.*", "run/broray/*.tmp.*"}, "file") ||
			match([]string{"data/routes/tmp/*", "data/routes/transactions/*"}, "file", "directory")
	case "backups":
		for _, g := range backupGroups {
			if match(g, "file") {
				return true
			}
		}
	case "routeBackups":
		return match(routeBackupGroup, "file", "directory")
	case "logs":
		return match(logGlobs, "file")
	}
	return false
}

type cleanupLine struct {
	category, kind string
	size           int64
	rel            string
	identity       string
}

func (a *area) cleanupPlanDir() string { return a.d.RunPath("cleanup", "plans") }

func safeName(base string) bool {
	return base != "" && base != "." && base != ".." && !strings.ContainsAny(base, "/\t\n\r")
}

// isOld: floor((now - mtime) / 86400) > days (broray-cleanup.sh:103-115).
func (a *area) isOld(st os.FileInfo, days int64) bool {
	now := a.now().Unix()
	m := st.ModTime().Unix()
	if now < m {
		return false
	}
	return (now-m)/86400 > days
}

func (a *area) candidate(out *[]cleanupLine, category, abs string) {
	rel, err := cleanupRelative(a.d.Root.Dir, abs)
	if err != nil {
		return
	}
	h, err := a.openCleanup(rel)
	if err != nil {
		return
	}
	defer h.close()
	st, size, identity, err := h.describe()
	if err != nil {
		return
	}
	kind := "file"
	if st.IsDir() {
		kind = "directory"
	}
	*out = append(*out, cleanupLine{category: category, kind: kind, size: size, rel: rel, identity: identity})
}

func (a *area) globs(patterns []string) []string {
	var out []string
	for _, g := range patterns {
		// The fixed parent is opened without following any ancestor symlinks.
		parent := filepath.Join(a.d.Root.Dir, filepath.Dir(g))
		dir, err := platform.OpenDirNoFollow(parent)
		if err != nil {
			continue
		}
		entries, err := dir.ReadDir(-1)
		_ = dir.Close()
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if match, _ := filepath.Match(filepath.Base(g), entry.Name()); match {
				out = append(out, filepath.Join(parent, entry.Name()))
			}
		}
	}
	return out
}

func (a *area) addAged(out *[]cleanupLine, category string, days int64, patterns []string) {
	for _, p := range a.globs(patterns) {
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&os.ModeSymlink != 0 || !a.isOld(st, days) {
			continue
		}
		a.candidate(out, category, p)
	}
}

func (a *area) addPreserved(out *[]cleanupLine, category string, keep int, days int64, patterns []string) {
	var list []string
	for _, p := range a.globs(patterns) {
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&os.ModeSymlink != 0 || !safeName(filepath.Base(p)) {
			continue
		}
		list = append(list, p)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(list)))
	for i, p := range list {
		if i < keep {
			continue
		}
		st, err := os.Lstat(p)
		if err != nil || !a.isOld(st, days) {
			continue
		}
		a.candidate(out, category, p)
	}
}

type cleanupOptions struct{ Temp, Backups, RouteBackups, Logs bool }

// buildList is broray_cleanup_build_list: the sorted unique list text.
func (a *area) buildList(o cleanupOptions) []byte {
	var lines []cleanupLine
	if o.Temp {
		a.addAged(&lines, "temp", 0, tempGlobs)
	}
	if o.Backups {
		for _, g := range backupGroups {
			a.addPreserved(&lines, "backups", 3, 0, g)
		}
	}
	if o.RouteBackups {
		a.addPreserved(&lines, "routeBackups", 3, 0, routeBackupGroup)
	}
	if o.Logs {
		a.addAged(&lines, "logs", 6, logGlobs)
	}
	set := map[string]bool{}
	var text []string
	for _, l := range lines {
		s := l.category + "\t" + l.kind + "\t" + strconv.FormatInt(l.size, 10) + "\t" + l.rel + "\t" + l.identity
		if !set[s] {
			set[s] = true
			text = append(text, s)
		}
	}
	sort.Strings(text)
	var buf bytes.Buffer
	for _, s := range text {
		buf.WriteString(s)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func parseList(b []byte) []cleanupLine {
	var out []cleanupLine
	for _, s := range strings.Split(string(b), "\n") {
		f := strings.Split(s, "\t")
		if len(f) != 5 || f[3] == "" {
			continue
		}
		n, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, cleanupLine{category: f[0], kind: f[1], size: n, rel: f[3], identity: f[4]})
	}
	return out
}

// cleanupBusy: the global fence (any holder, including a pending resumable
// routes operation) or a running BROray page operation.
func (a *area) cleanupBusy() bool { return a.d.Lock.Busy() || a.page.running() }

func (a *area) cleanupPlanHandler(c *cgi.Req) {
	body, ok := brorayBody(c)
	if !ok {
		return
	}
	m, ok := jsonObject(body)
	if !ok {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_JSON", "Тело запроса содержит некорректный JSON."))
		return
	}
	var opts cleanupOptions
	vals := []*bool{&opts.Temp, &opts.Backups, &opts.RouteBackups, &opts.Logs}
	for i, k := range []string{"temp", "backups", "routeBackups", "logs"} {
		switch jqRawKeep(m, k) {
		case "true":
			*vals[i] = true
		case "false":
		default:
			cgi.WriteErr(c.W, cgi.E(400, "CLEANUP_OPTIONS_INVALID", "Параметры очистки некорректны."))
			return
		}
	}
	if !opts.Temp && !opts.Backups && !opts.RouteBackups && !opts.Logs {
		cgi.WriteErr(c.W, cgi.E(400, "CLEANUP_NOTHING_SELECTED", "Выберите хотя бы одну категорию очистки."))
		return
	}
	a.cleanMu.Lock()
	defer a.cleanMu.Unlock()
	planDir, err := a.openCleanupPlans(true)
	if err != nil {
		cgi.WriteErr(c.W, cgi.E(500, "CLEANUP_RUNTIME_FAILED", "Не удалось создать служебный каталог очистки."))
		return
	}
	defer planDir.Close()
	a.removeExpiredPlans(planDir)
	if a.cleanupBusy() {
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_OPERATION_BUSY", "Другая конфликтующая операция уже выполняется."))
		return
	}
	token := compat.RandHex(16)
	list := a.buildList(opts)
	lines := parseList(list)
	now := a.now()
	var total int64
	categories := map[string]any{}
	candidates := []any{}
	counts := map[string][2]int64{}
	for _, l := range lines {
		total += l.size
		v := counts[l.category]
		counts[l.category] = [2]int64{v[0] + 1, v[1] + l.size}
		candidates = append(candidates, map[string]any{"category": l.category, "kind": l.kind, "path": l.rel, "sizeBytes": l.size})
	}
	for k, v := range counts {
		categories[k] = map[string]any{"count": v[0], "estimatedBytes": v[1]}
	}
	plan := map[string]any{"schemaVersion": 2, "token": token, "createdAt": compat.StampUTC(now), "createdEpoch": now.Unix(),
		"expiresEpoch": now.Unix() + cleanupTTL, "ttlSeconds": cleanupTTL, "digest": compat.SHA256Hex(list),
		"options":        map[string]any{"temp": opts.Temp, "backups": opts.Backups, "routeBackups": opts.RouteBackups, "logs": opts.Logs},
		"candidateCount": len(lines), "estimatedBytes": total, "categories": categories, "candidates": candidates}
	if err := platform.WriteNewRegularAtNoFollow(planDir, token+".list", list, 0600); err != nil {
		cgi.WriteErr(c.W, cgi.E(500, "CLEANUP_PLAN_FAILED", "Не удалось сохранить план очистки."))
		return
	}
	planJSON, marshalErr := json.MarshalIndent(plan, "", "  ")
	if marshalErr != nil {
		cgi.WriteErr(c.W, cgi.E(500, "CLEANUP_PLAN_FAILED", "Не удалось сохранить план очистки."))
		cleanupRemovePlanFile(planDir, token+".list")
		return
	}
	if err := platform.WriteNewRegularAtNoFollow(planDir, token+".json", append(planJSON, '\n'), 0600); err != nil {
		cleanupRemovePlanFile(planDir, token+".list")
		cgi.WriteErr(c.W, cgi.E(500, "CLEANUP_PLAN_FAILED", "Не удалось сохранить план очистки."))
		return
	}
	cgi.WriteOK(c.W, plan)
}

var reHex32Token = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (a *area) cleanupHandler(c *cgi.Req) {
	body, ok := brorayBody(c)
	if !ok {
		return
	}
	m, ok := jsonObject(body)
	if !ok {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_JSON", "Тело запроса содержит некорректный JSON."))
		return
	}
	raw, has := m["token"]
	token := jqRaw(raw, has)
	if token == "" {
		cgi.WriteErr(c.W, cgi.E(400, "CLEANUP_TOKEN_REQUIRED", "Не передано подтверждение очистки."))
		return
	}
	a.cleanMu.Lock()
	defer a.cleanMu.Unlock()
	if !reHex32Token.MatchString(token) {
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_TOKEN_INVALID", "Подтверждение очистки некорректно."))
		return
	}
	planDir, openErr := a.openCleanupPlans(false)
	if openErr != nil {
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_PLAN_NOT_FOUND", "План очистки не найден. Выполните проверку заново."))
		return
	}
	defer planDir.Close()
	removePlan := func() { cleanupRemovePlanFile(planDir, token+".json"); cleanupRemovePlanFile(planDir, token+".list") }
	var plan struct {
		ExpiresEpoch int64           `json:"expiresEpoch"`
		Digest       string          `json:"digest"`
		Options      json.RawMessage `json:"options"`
	}
	list, lerr := platform.ReadRegularAtNoFollow(planDir, token+".list")
	planJSON, perr := platform.ReadRegularAtNoFollow(planDir, token+".json")
	if perr != nil || lerr != nil || len(planJSON) == 0 || json.Unmarshal(planJSON, &plan) != nil {
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_PLAN_NOT_FOUND", "План очистки не найден. Выполните проверку заново."))
		return
	}
	if plan.ExpiresEpoch < a.now().Unix() {
		removePlan()
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_PLAN_EXPIRED", "Срок подтверждения истёк. Выполните проверку заново."))
		return
	}
	var o struct{ Temp, Backups, RouteBackups, Logs bool }
	_ = json.Unmarshal(plan.Options, &o)
	current := a.buildList(cleanupOptions{o.Temp, o.Backups, o.RouteBackups, o.Logs})
	if compat.SHA256Hex(current) != plan.Digest || !bytes.Equal(current, list) {
		removePlan()
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_PLAN_CHANGED", "Состав файлов изменился. Выполните проверку заново."))
		return
	}
	if a.page.running() {
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_OPERATION_BUSY", "Другая конфликтующая операция уже выполняется."))
		return
	}
	release, ok := a.d.Lock.TryAcquire("system", "system:cleanup", "")
	if !ok {
		cgi.WriteErr(c.W, cgi.E(409, "CLEANUP_OPERATION_BUSY", "Другая конфликтующая операция уже выполняется."))
		return
	}
	op := a.d.Queue.StartOperation("", "unknown", "system:cleanup", "USER", "protected", "")
	op.Phase("working")
	var deleted, freed int64
	failed := ""
	for _, l := range parseList(list) {
		if !cleanupAllowed(l.category, l.kind, l.rel) {
			failed = l.rel
			break
		}
		if err := a.removeCleanup(l); err != nil {
			failed = l.rel
			break
		}
		deleted++
		freed += l.size
	}
	if failed != "" {
		op.Finish(errors.New("OPERATION_FAILED: cleanup stopped"))
	} else {
		op.Finish(nil)
	}
	release()
	removePlan()
	if failed != "" {
		cgi.WriteErr(c.W, cgi.E(500, "CLEANUP_DELETE_FAILED", "Очистка остановлена на объекте: "+failed))
		return
	}
	cgi.WriteOK(c.W, map[string]any{"ok": true, "deletedCount": deleted, "freedBytes": freed, "completedAt": compat.StampUTC(a.now())})
}
