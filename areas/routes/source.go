package routes

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// cliError is a failed CLI step: message is what 3.x printed to stderr.
type cliError struct {
	msg     string
	network bool // the source host could not be reached (no verdict on the files)
	noState bool // a local read failure that 3.x did not record in the state
}

func (c *cliError) Error() string { return c.msg }

func cliErr(format string, a ...any) error { return &cliError{msg: fmt.Sprintf(format, a...)} }

func netErr(format string, a ...any) error {
	return &cliError{msg: fmt.Sprintf(format, a...), network: true}
}

type sourceFile struct {
	Name    string
	SHA256  string
	Size    int
	Routes  int
	HTMLURL string
	Content []byte
	Parsed  []string // sorted unique net/prefix (bogons removed)
	Bogons  []string // dropped private/special networks
}

func (f sourceFile) object() obj {
	return obj{"name": f.Name, "parser": "windows-route-bat", "sha256": f.SHA256, "sizeBytes": f.Size, "routeCount": f.Routes, "htmlUrl": f.HTMLURL,
		"bogonRoutesDropped": toAny(nonNil(f.Bogons))}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// droppedBogons lists every dropped network of the files (sorted, unique).
func droppedBogons(files []sourceFile) []string {
	var all []string
	for _, f := range files {
		all = append(all, f.Bogons...)
	}
	return nonNil(sortedUnique(all))
}

func bogonLine(files []sourceFile) string {
	b := droppedBogons(files)
	if len(b) == 0 {
		return ""
	}
	return "Отброшено частных и служебных сетей: " + itoa(len(b)) + " (" + strings.Join(b, ", ") + ")"
}

func sourceFilesJSON(files []sourceFile) []any {
	out := make([]any, len(files))
	for i, f := range files {
		out[i] = f.object()
	}
	return out
}

// sourceSetSHA is sha256 of "name\tsha256\tsizeBytes" lines sorted by lower-case name.
func sourceSetSHA(files []any) string {
	type row struct{ key, line string }
	var rows []row
	for _, x := range files {
		m, _ := x.(obj)
		size, _ := numOf(m["sizeBytes"])
		rows = append(rows, row{strings.ToLower(str(m, "name")), str(m, "name") + "\t" + str(m, "sha256") + "\t" + strconv.FormatFloat(size, 'f', -1, 64)})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r.line
	}
	return sha256Hex(listBytes(lines))
}

// fetchSources downloads and parses the files of a built-in bundle listing
// (broray_routes_parse_windows_bat per file, union with LC_ALL=C sort -u).
// Every file must match its listing entry (size + SHA-256 or git blob SHA).
func (e *engine) fetchSources(ctx context.Context, b builtin, src catalogSource, names []string, verb string) ([]sourceFile, []string, error) {
	var files []sourceFile
	total := 0
	var union []string
	for _, name := range names {
		ref, ok := src.ref(name)
		if !ok || !safeSourceName(name) {
			return nil, nil, cliErr("Манифест содержит некорректное имя файла источника.")
		}
		body, err := e.httpGet(ctx, rawURL(e.rawBase, src.Commit, b.Directory, name), b.MaxSourceBytes)
		if err != nil {
			if verb == "download" {
				return nil, nil, netErr("Не удалось скачать файл %s зафиксированной версии.", name)
			}
			return nil, nil, netErr("Не удалось получить файл %s для найденной версии.", name)
		}
		if len(body) == 0 {
			return nil, nil, cliErr("Файл %s пуст.", name)
		}
		total += len(body)
		if total > b.MaxSourceBytes {
			return nil, nil, cliErr("Общий размер файлов набора превышает разрешённый предел.")
		}
		sum := sha256Hex(body)
		match := len(body) == ref.Size
		if ref.SHA256 != "" {
			match = match && sum == ref.SHA256
		} else {
			match = match && ref.GitSHA != "" && gitBlobSHA(body) == ref.GitSHA
		}
		if !match {
			if src.Pinned {
				return nil, nil, cliErr("Файл %s не совпал с закреплённой версией источника %s.", name, catalogCommit[:7])
			}
			return nil, nil, cliErr("Файл %s не совпал с версией источника %s.", name, src.Commit[:7])
		}
		res := parseBat(body, true, 0)
		if res.ErrorCount > 0 {
			details := res.Errors
			if len(details) > 8 {
				details = details[:8]
			}
			return nil, nil, cliErr("Файл %s не прошёл проверку. %s", name, strings.Join(details, "\n"))
		}
		parsed, bogons := filterBogons(sortedUnique(res.Routes))
		if len(parsed) == 0 {
			return nil, nil, cliErr("Файл %s не прошёл проверку. В источнике не найдено маршрутов.", name)
		}
		if len(parsed) > b.MaxRoutes {
			return nil, nil, cliErr("Файл %s не прошёл проверку. Количество маршрутов превышает лимит: %d > %d", name, len(parsed), b.MaxRoutes)
		}
		files = append(files, sourceFile{Name: name, SHA256: sum, Size: len(body), Routes: len(parsed),
			HTMLURL: htmlURL(src.Commit, b.Directory, name), Content: body, Parsed: parsed, Bogons: bogons})
		union = append(union, parsed...)
	}
	normalized := sortedUnique(union)
	if len(normalized) == 0 {
		return nil, nil, cliErr("В наборе не найдено маршрутов.")
	}
	if len(normalized) > b.MaxRoutes {
		return nil, nil, cliErr("Количество маршрутов превышает лимит: %d > %d", len(normalized), b.MaxRoutes)
	}
	return files, normalized, nil
}

// gitBlobSHA is the git object id of a blob ("blob <size>\x00" + content),
// the "sha" the GitHub contents API reports for a file.
func gitBlobSHA(body []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(body))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// safeSourceName is broray_routes_safe_filename.
func safeSourceName(s string) bool {
	if s == "" || strings.Contains(s, "..") {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("_.()-", c)) {
			return false
		}
	}
	return true
}

func (e *engine) httpGet(ctx context.Context, url string, limit int) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BROray-routes/1.1")
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
}

func (e *engine) stateError(id, status, message string, checked bool) {
	s, ok := readObj(e.stateFile(id))
	if !ok {
		return
	}
	now := stamp()
	s["status"] = status
	if checked {
		s["lastCheckedAt"] = now
	}
	s["lastError"] = obj{"message": message, "at": now}
	s["updatedAt"] = now
	_ = writeObj(e.stateFile(id), s)
}

func (e *engine) ensureState(id string) (obj, error) {
	if s, ok := readObj(e.stateFile(id)); ok {
		return s, nil
	}
	s := seedState(id)
	if err := writeObj(e.stateFile(id), s); err != nil {
		return nil, cliErr("Не удалось создать локальное состояние набора.")
	}
	return s, nil
}

func (e *engine) rollbackBlocked() bool {
	return e.rollbackUncertain.Load() || fileExists(e.rollbackMarker()) || e.unfinishedTransaction()
}

func (e *engine) beginLiveTransaction(path string) {
	e.txMu.Lock()
	defer e.txMu.Unlock()
	e.liveTransactions[path] = true
}

func (e *engine) endLiveTransaction(path string) {
	e.txMu.Lock()
	defer e.txMu.Unlock()
	delete(e.liveTransactions, path)
}

func (e *engine) isLiveTransaction(path string) bool {
	e.txMu.Lock()
	defer e.txMu.Unlock()
	return e.liveTransactions[path]
}

// An unfinished durable transaction remains a route-write fence even when
// the separate rollback marker could not be published before a restart.
func (e *engine) unfinishedTransaction() bool {
	dir := e.p("transactions")
	directory, err := platform.OpenDirNoFollow(dir)
	if err != nil {
		return true
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		name := entry.Name()
		var path string
		var syncDir bool
		switch {
		case strings.HasPrefix(name, "sync-"):
			if !entry.IsDir() {
				return true
			}
			syncDir = true
			path = filepath.Join(dir, name, "transaction.json")
		case strings.HasPrefix(name, "delete-") && strings.HasSuffix(name, ".json"):
			if !entry.Type().IsRegular() {
				return true
			}
			path = filepath.Join(dir, name)
		default:
			continue
		}
		if e.isLiveTransaction(path) {
			continue
		}
		var data []byte
		if syncDir {
			child, err := platform.OpenDirAtNoFollow(directory, name)
			if err != nil {
				return true
			}
			data, err = platform.ReadRegularAtNoFollow(child, "transaction.json")
			if errors.Is(err, os.ErrNotExist) {
				// An empty new sync directory is safe before its first record.
				// A saved backup with a missing record is ambiguous.
				children, readErr := child.ReadDir(-1)
				closeErr := child.Close()
				if readErr != nil || closeErr != nil || len(children) != 0 {
					return true
				}
				continue
			}
			closeErr := child.Close()
			if err != nil || closeErr != nil {
				return true
			}
		} else {
			data, err = platform.ReadRegularAtNoFollow(directory, name)
			if err != nil {
				return true
			}
		}
		m, ok := decodeObjBytes(data)
		if !ok || (str(m, "operation") != "sync" && str(m, "operation") != "delete") {
			return true
		}
		switch str(m, "phase") {
		case "committed", "paused", "rolled_back", "resume_attempt_rolled_back":
			continue
		default:
			return true
		}
	}
	return false
}

// checkEval is the result of comparing a listing with the downloaded
// version of a bundle; it touches no file.
type checkEval struct {
	src                                 catalogSource
	files                               []sourceFile
	normalized                          []string
	contentSHA, setSHA                  string
	filesJSON                           []any
	baselineCount                       int
	baselineContent, baselineSet        string
	added, changed, unchanged, removed  []any
	routeAdded, routeRemoved, routeSame int
	sourceChanged, routesChanged        bool
	result, status, message             string
	downloadRequired, downloadedPresent bool
}

// evaluateCheck downloads the listing's files (in memory), validates them
// and diffs them against the downloaded version. Errors marked noState
// must not be recorded as the bundle's check error.
func (e *engine) evaluateCheck(ctx context.Context, id string, b builtin, state obj, src catalogSource) (*checkEval, error) {
	ev := &checkEval{src: src}
	files, normalized, err := e.fetchSources(ctx, b, src, src.names(), "check")
	if err != nil {
		return nil, err
	}
	ev.files, ev.normalized = files, normalized
	ev.contentSHA = sha256Hex(listBytes(normalized))
	ev.filesJSON = sourceFilesJSON(files)
	ev.setSHA = sourceSetSHA(ev.filesJSON)

	var baseline []string
	baselineFiles := []any{}
	ev.downloadedPresent = sub(state, "downloadedVersion") != nil
	if ev.downloadedPresent {
		cat := e.catalogDir(id)
		if !regularFile(filepath.Join(cat, "normalized.txt")) || !regularFile(filepath.Join(cat, "source-files.json")) || !regularFile(filepath.Join(cat, "version.json")) {
			return nil, cliErr("Локальный каталог скачанной версии отсутствует или повреждён.")
		}
		raw, err := os.ReadFile(filepath.Join(cat, "normalized.txt"))
		if err != nil {
			return nil, &cliError{msg: "Не удалось прочитать локальный список маршрутов.", noState: true}
		}
		ev.baselineContent = sha256Hex(raw)
		if ev.baselineContent != str(sub(state, "downloadedVersion"), "contentSha256") {
			return nil, cliErr("Контрольная сумма локального каталога не совпадает с состоянием.")
		}
		baseline, _ = readLines(filepath.Join(cat, "normalized.txt"))
		var sf any
		if err := readJSONAny(filepath.Join(cat, "source-files.json"), &sf); err != nil {
			return nil, &cliError{msg: "Не удалось прочитать локальный список файлов.", noState: true}
		}
		baselineFiles = anyList(sf)
		ev.baselineSet = sourceSetSHA(baselineFiles)
	}
	ev.baselineCount = len(baseline)
	oldByName := map[string]obj{}
	for _, x := range baselineFiles {
		if m, ok := x.(obj); ok {
			oldByName[str(m, "name")] = m
		}
	}
	newByName := map[string]bool{}
	ev.added, ev.changed, ev.unchanged, ev.removed = []any{}, []any{}, []any{}, []any{}
	for _, f := range files {
		newByName[f.Name] = true
		old, ok := oldByName[f.Name]
		if !ok {
			ev.added = append(ev.added, f.Name)
			continue
		}
		size, _ := numOf(old["sizeBytes"])
		if str(old, "sha256") != f.SHA256 || int(size) != f.Size {
			ev.changed = append(ev.changed, f.Name)
		} else {
			ev.unchanged = append(ev.unchanged, f.Name)
		}
	}
	for _, x := range baselineFiles {
		if m, ok := x.(obj); ok && !newByName[str(m, "name")] {
			ev.removed = append(ev.removed, str(m, "name"))
		}
	}
	oldSet := map[string]bool{}
	for _, r := range baseline {
		oldSet[r] = true
	}
	newSet := map[string]bool{}
	for _, r := range normalized {
		newSet[r] = true
		if !oldSet[r] {
			ev.routeAdded++
		}
	}
	for _, r := range baseline {
		if !newSet[r] {
			ev.routeRemoved++
		}
	}
	ev.routeSame = len(normalized) - ev.routeAdded
	if ev.routeSame < 0 {
		ev.routeSame = 0
	}
	ev.sourceChanged = len(ev.added) > 0 || len(ev.changed) > 0 || len(ev.removed) > 0
	ev.routesChanged = ev.routeAdded > 0 || ev.routeRemoved > 0
	ev.downloadRequired = true
	n := len(files)
	switch {
	case !ev.downloadedPresent:
		ev.result, ev.status = "initial_available", "available"
		ev.message = fmt.Sprintf("Найдено файлов: %d. Маршрутов: %d.", n, len(normalized))
	case !ev.sourceChanged && !ev.routesChanged:
		ev.result, ev.status, ev.downloadRequired = "no_changes", "no_changes", false
		ev.message = fmt.Sprintf("Проверено файлов: %d. Новых или изменённых маршрутов нет.", n)
	case !ev.routesChanged:
		ev.result, ev.status = "source_changed_routes_unchanged", "update_available"
		ev.message = "Источник изменился, но итоговый список маршрутов не изменился. Обновление Keenetic не требуется."
	default:
		ev.result, ev.status = "changed", "update_available"
		ev.message = fmt.Sprintf("На GitHub добавлено %d, изменено %d, удалено %d файлов. Маршруты: добавлено %d, удалено %d, без изменений %d.",
			len(ev.added), len(ev.changed), len(ev.removed), ev.routeAdded, ev.routeRemoved, ev.routeSame)
	}
	return ev, nil
}

// commitCheck records a check result in the bundle state and returns the
// CLI output of `broray-routes check`.
func (e *engine) commitCheck(id string, state obj, ev *checkEval) (string, error) {
	iface := e.managedInterface()
	n := len(ev.files)
	installedSHA := str(sub(state, "installedVersion"), "contentSha256")
	keeneticUpdate := installedSHA != "" && installedSHA != ev.contentSHA
	now := stamp()
	state["bundleId"] = id
	state["status"] = ev.status
	state["availableVersion"] = obj{"sourceCommit": ev.src.Commit, "sourceDate": ev.src.Date, "contentSha256": ev.contentSHA,
		"sourceSetSha256": ev.setSHA, "sourceFileCount": n, "sourceFiles": ev.filesJSON}
	state["routeCount"] = len(ev.normalized)
	state["lastCheckedAt"] = now
	state["lastError"] = nil
	state["checkResult"] = obj{"result": ev.result, "baselineContentSha256": nullStr(ev.baselineContent), "currentContentSha256": ev.contentSHA,
		"baselineSourceSetSha256": nullStr(ev.baselineSet), "currentSourceSetSha256": ev.setSHA, "sourceFileCount": n,
		"sourceFiles": ev.filesJSON, "fileChanges": obj{"addedFiles": ev.added, "removedFiles": ev.removed, "changedFiles": ev.changed, "unchangedFiles": ev.unchanged},
		"routeChanges":  obj{"before": ev.baselineCount, "after": len(ev.normalized), "added": ev.routeAdded, "removed": ev.routeRemoved, "unchanged": ev.routeSame},
		"sourceChanged": ev.sourceChanged, "routesChanged": ev.routesChanged, "downloadRequired": ev.downloadRequired,
		"keeneticUpdateRequired": keeneticUpdate, "discoveryMode": "all-bat", "managedInterface": iface, "message": ev.message,
		"bogonRoutesDropped": toAny(droppedBogons(ev.files)), "bogonRouteCount": len(droppedBogons(ev.files))}
	state["updatedAt"] = now
	if err := writeObj(e.stateFile(id), state); err != nil {
		return "", cliErr("Не удалось установить новое состояние.")
	}
	out := []string{ev.message, "Набор: " + id, "Управляемый интерфейс: " + iface, "Версия источника: " + ev.src.Commit[:7],
		"Дата источника: " + ev.src.Date, "Файлов источника: " + itoa(n), "Маршрутов: " + itoa(len(ev.normalized)),
		"SHA-256 списка маршрутов: " + ev.contentSHA, "SHA-256 состава файлов: " + ev.setSHA}
	if l := bogonLine(ev.files); l != "" {
		out = append(out, l)
	}
	return strings.Join(out, "\n") + "\n", nil
}

// runCheck is `broray-routes check <id>` (lib/routes-source-check.sh). It
// checks the bundle's current listing (pinned, or the newest commit found
// by the scheduled check) and returns the stdout text.
func (e *engine) runCheck(ctx context.Context, id string) (string, error) {
	b, ok := findBuiltin(id)
	if !ok {
		return "", cliErr("Набор маршрутов не найден.")
	}
	state, err := e.ensureState(id)
	if err != nil {
		return "", err
	}
	if e.rollbackBlocked() {
		return "", cliErr(msgRollbackBlock)
	}
	ev, err := e.evaluateCheck(ctx, id, b, state, e.sourceFor(b))
	if err != nil {
		var ce *cliError
		if errors.As(err, &ce) && ce.noState {
			return "", err
		}
		e.stateError(id, "check_error", err.Error(), true)
		return "", cliErr("%s", err.Error())
	}
	// The owner looked at the bundle: a scheduled "needs review" mark is done.
	delete(state, "scheduledUpdate")
	return e.commitCheck(id, state, ev)
}

func readJSONAny(path string, v *any) error {
	m, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	o, err := decodeAny(m)
	if err != nil {
		return err
	}
	*v = o
	return nil
}

// runDownload is `broray-routes download <id>` (lib/routes-download.sh).
func (e *engine) runDownload(ctx context.Context, id string) (string, error) {
	b, ok := findBuiltin(id)
	if !ok {
		return "", cliErr("Набор маршрутов не найден.")
	}
	state, err := e.ensureState(id)
	if err != nil {
		return "", err
	}
	av := sub(state, "availableVersion")
	commit := str(av, "sourceCommit")
	if len(commit) != 40 || !isHexN(commit) {
		return "", cliErr("Сначала выполните проверку новых маршрутов.")
	}
	if str(av, "sourceDate") == "" {
		return "", cliErr("В локальном состоянии отсутствует дата версии.")
	}
	expectedSHA := str(av, "contentSha256")
	if !isHex64(expectedSHA) {
		return "", cliErr("В локальном состоянии отсутствует корректный хеш версии.")
	}
	expectedSet := str(av, "sourceSetSha256")
	if expectedSet != "" && !isHex64(expectedSet) {
		return "", cliErr("В локальном состоянии указан некорректный отпечаток файлов источника.")
	}
	expectedCount, ok := intOf(state["routeCount"])
	if !ok {
		return "", cliErr("В локальном состоянии отсутствует количество маршрутов.")
	}
	if e.rollbackBlocked() {
		return "", cliErr(msgRollbackBlock)
	}
	fail := func(msg string) (string, error) {
		e.stateError(id, "download_error", msg, false)
		return "", cliErr("%s", msg)
	}
	// Only the pinned commit or the bundle's current listing are accepted.
	if commit != catalogCommit && commit != e.sourceFor(b).Commit {
		return fail("Сначала выполните проверку источника, чтобы получить реальный список всех .bat-файлов.")
	}
	sf := anyList(av["sourceFiles"])
	if len(sf) == 0 {
		return fail("Сначала выполните проверку источника, чтобы получить реальный список всех .bat-файлов.")
	}
	// The checked version's files (SHA-256 + size recorded by the check).
	src := catalogSource{Commit: commit, Date: str(av, "sourceDate"), Pinned: commit == catalogCommit}
	var names []string
	for _, x := range sf {
		m, _ := x.(obj)
		size, _ := intOf(orNull(m, "sizeBytes"))
		if !isHex64(str(m, "sha256")) {
			return fail("Сначала выполните проверку источника, чтобы получить реальный список всех .bat-файлов.")
		}
		names = append(names, str(m, "name"))
		src.Files = append(src.Files, sourceRef{Name: str(m, "name"), Size: size, SHA256: str(m, "sha256")})
	}
	files, normalized, err := e.fetchSources(ctx, b, src, names, "download")
	if err != nil {
		return fail(err.Error())
	}
	if len(normalized) != expectedCount {
		return fail("Количество маршрутов скачанной версии не совпало с результатом проверки.")
	}
	contentSHA := sha256Hex(listBytes(normalized))
	if contentSHA != expectedSHA {
		return fail("Содержимое источника не совпало с проверенной версией. Экспорт запрещён.")
	}
	filesJSON := sourceFilesJSON(files)
	setSHA := sourceSetSHA(filesJSON)
	if expectedSet != "" && setSHA != expectedSet {
		return fail("Состав файлов источника изменился после проверки. Выполните поиск обновлений повторно.")
	}
	iface := e.managedInterface()
	now := stamp()
	// stage the catalog, then swap it atomically; the previous one is kept in backup/
	parent := e.p("catalog")
	work := filepath.Join(parent, "."+id+".new."+itoa(os.Getpid()))
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(filepath.Join(work, "source"), 0o700); err != nil {
		return "", cliErr("Не удалось создать временный каталог загрузки.")
	}
	defer os.RemoveAll(work)
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(work, "source", f.Name), f.Content, 0o600); err != nil {
			return "", cliErr("Не удалось сохранить исходный файл %s.", f.Name)
		}
	}
	routes := make([]any, len(normalized))
	for i, r := range normalized {
		net, p, _ := splitCIDR(r)
		routes[i] = obj{"family": "ipv4", "network": net, "prefix": p}
	}
	mf, _ := readObj(e.manifestFile(id))
	writes := []struct {
		name string
		v    any
	}{
		{"manifest.json", mf},
		{"source-files.json", filesJSON},
		{"routes.json", obj{"schemaVersion": 1, "bundleId": id, "targetInterface": iface, "routeComment": "BROray", "contentSha256": contentSHA, "routeCount": len(normalized), "routes": routes}},
		{"version.json", obj{"schemaVersion": 1, "bundleId": id, "sourceCommit": commit, "sourceDate": str(av, "sourceDate"), "contentSha256": contentSHA,
			"sourceSetSha256": setSHA, "routeCount": len(normalized), "sourceFileCount": len(files), "sourceFiles": filesJSON, "downloadedAt": now, "targetInterface": iface}},
	}
	if err := os.WriteFile(filepath.Join(work, "normalized.txt"), listBytes(normalized), 0o600); err != nil {
		return "", cliErr("Не удалось сохранить нормализованный список.")
	}
	for _, w := range writes {
		if err := writeObj(filepath.Join(work, w.name), w.v); err != nil {
			return "", cliErr("Не удалось сформировать локальный каталог маршрутов.")
		}
	}
	target := e.catalogDir(id)
	backup := e.p("backup", "catalog-"+id+"-"+nowFn().Format("20060102-150405")+"-"+itoa(os.Getpid()))
	had := fileExists(target)
	if had {
		if err := os.Rename(target, backup); err != nil {
			return "", cliErr("Не удалось сохранить предыдущий локальный каталог.")
		}
	}
	if err := os.Rename(work, target); err != nil {
		if had {
			_ = os.Rename(backup, target)
		}
		return "", cliErr("Не удалось установить новый локальный каталог.")
	}
	restore := func() {
		_ = os.RemoveAll(target)
		if had {
			_ = os.Rename(backup, target)
		}
	}
	dv := sub(state, "downloadedVersion")
	installedSHA := str(sub(state, "installedVersion"), "contentSha256")
	statusV, kur := "downloaded", true
	if installedSHA != "" && installedSHA == contentSHA {
		statusV, kur = "installed", false
	}
	var result, message string
	switch {
	case str(dv, "contentSha256") == contentSHA && str(dv, "sourceSetSha256") == setSHA:
		result, message = "already_downloaded", "Проверенная версия файлов уже загружена."
	case installedSHA != "" && installedSHA == contentSHA:
		result, message = "files_updated_routes_unchanged", "Файлы маршрутов обновлены. Итоговый список маршрутов не изменился; обновление Keenetic не требуется."
	case installedSHA != "":
		result, message = "updated", "Файлы маршрутов обновлены. Набор готов к обновлению в Keenetic."
	default:
		result, message = "downloaded", "Файлы маршрутов загружены. Набор готов к установке в Keenetic."
	}
	state["status"] = statusV
	delete(state, "scheduledUpdate") // a review mark is resolved by downloading
	state["downloadedVersion"] = obj{"sourceCommit": commit, "sourceDate": str(av, "sourceDate"), "contentSha256": contentSHA,
		"sourceSetSha256": setSHA, "sourceFileCount": len(files), "sourceFiles": filesJSON}
	state["routeCount"] = len(normalized)
	state["lastDownloadedAt"] = now
	state["lastVerifiedAt"] = nil
	state["verifyResult"] = nil
	state["lastError"] = nil
	state["downloadResult"] = obj{"result": result, "message": message, "catalogPath": target, "managedInterface": iface,
		"routeCount": len(normalized), "sourceFileCount": len(files), "sourceSetSha256": setSHA, "keeneticUpdateRequired": kur,
		"bogonRoutesDropped": toAny(droppedBogons(files)), "bogonRouteCount": len(droppedBogons(files))}
	state["updatedAt"] = now
	if err := writeObj(e.stateFile(id), state); err != nil {
		restore()
		return "", cliErr("Не удалось установить состояние загрузки.")
	}
	out := []string{message, "Набор: " + id, "Версия источника: " + commit[:7], "Маршрутов: " + itoa(len(normalized)),
		"Управляемый интерфейс: " + iface, "Локальный каталог: " + target}
	if l := bogonLine(files); l != "" {
		out = append(out, l)
	}
	return strings.Join(out, "\n") + "\n", nil
}

func isHexN(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != ""
}
