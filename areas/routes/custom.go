package routes

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

// Custom routes (user BAT sets): contract routes-custom-dns.md Part A
// (lib/routes-user-import.sh, web-new/api/routes/custom-*.cgi).

const (
	userMaxBytes   = 2097152
	userMaxFiles   = 16
	userMaxRoutes  = 100000
	userPreviewTTL = 1800
	customBodyMax  = 4194304
	userDesc       = "Пользовательский набор из загруженных BAT-файлов."
)

// customError is the CLI "BRORAY_ERROR:<CODE>:<message>" + details lines.
func customError(code, message, details string) *cgi.APIError {
	status := 400
	switch code {
	case "ROUTES_BUSY":
		status = 409
	case "BUNDLE_NOT_FOUND":
		status = 404
	case "DEPENDENCY_MISSING", "MODULE_UNAVAILABLE", "INDEX_INVALID", "RUNTIME_PREPARE_FAILED":
		status = 500
	}
	return cgi.Ed(status, code, message, details)
}

func customGeneric(details string) *cgi.APIError {
	return cgi.Ed(400, "CUSTOM_ROUTES_OPERATION_FAILED", "Операция с пользовательскими маршрутами завершилась ошибкой.", details)
}

func registerCustomHandlers() {
	post := cgi.POST()
	cgi.Register("/api/routes/custom-summary.cgi", wrap(func(e *engine, c *cgi.Req) (any, error) {
		return e.pageSummary(c.R.Context(), "custom"), nil
	}))
	cgi.Register("/api/routes/custom-preview.cgi", wrap(hCustomPreview), post, cgi.Limit(customBodyMax))
	cgi.Register("/api/routes/custom-commit.cgi", wrap(hCustomCommit), post, cgi.Limit(customBodyMax))
	cgi.Register("/api/routes/custom-validate.cgi", wrap(hCustomValidate), post, cgi.Limit(customBodyMax))
	cgi.Register("/api/routes/custom-remove.cgi", wrap(hCustomRemove), post, cgi.Limit(customBodyMax))
	cgi.Register("/api/routes/custom-list.cgi", wrap(hCustomList))
	cgi.Register("/api/routes/custom-status.cgi", wrap(hCustomStatus))
}

// customBundleFromQuery is broray_custom_routes_bundle_from_query.
func customBundleFromQuery(raw string) (string, *cgi.APIError) {
	if !strings.HasPrefix(raw, "bundleId=") {
		return "", cgi.E(400, "ROUTES_BUNDLE_REQUIRED", "Не указан идентификатор пользовательского набора.")
	}
	id := strings.TrimPrefix(raw, "bundleId=")
	if !validUserID(id) {
		return "", cgi.E(400, "ROUTES_BUNDLE_INVALID", "Некорректный идентификатор пользовательского набора.")
	}
	return id, nil
}

// customBody reads the JSON object body (web-request-body.sh + jq type check).
func customBody(c *cgi.Req) (obj, *cgi.APIError) {
	if c.R.ContentLength < 0 && !c.HasBody() {
		return nil, cgi.E(400, "CONTENT_LENGTH_INVALID", "Некорректный размер запроса.")
	}
	if !c.HasBody() {
		return nil, cgi.E(400, "REQUEST_BODY_REQUIRED", "Тело запроса отсутствует.")
	}
	m, err := decodeAny(c.Body())
	o, ok := m.(obj)
	if err != nil || !ok {
		return nil, cgi.E(400, "REQUEST_JSON_INVALID", "Запрос не является корректным JSON.")
	}
	return o, nil
}

// customJob admits a custom:<action> job (custom-common.sh).
func (e *engine) customJob(action, bundle string) (*job, *cgi.APIError) {
	j, aerr := e.admit("custom:"+action, bundle, msgBusyCustom)
	if aerr != nil {
		return nil, aerr
	}
	return j, nil
}

func endJob(j *job, err *cgi.APIError) {
	if err != nil {
		j.end(errOf(err))
	} else {
		j.end(nil)
	}
}

// ---------- runtime / index ----------

func (e *engine) userPrepareRuntime() *cgi.APIError {
	for _, d := range []string{"catalog", "manifests", "state", "installed/bundles", "backup", "tmp", "tmp/user-previews"} {
		if err := platform.MkdirAllDurable(e.p(d), 0o700); err != nil {
			return customError("RUNTIME_PREPARE_FAILED", "Не удалось подготовить каталоги пользовательских маршрутов.", "")
		}
	}
	if !regularFile(e.customFile()) {
		if err := writeObj(e.customFile(), obj{"schemaVersion": 1, "bundles": []any{}}); err != nil {
			return customError("INDEX_CREATE_FAILED", "Не удалось создать реестр пользовательских наборов.", "")
		}
	}
	idx, ok := readObj(e.customFile())
	bundles, isArr := idx["bundles"].([]any)
	valid := ok && intOr(idx["schemaVersion"], 0) == 1 && isArr
	seen := map[string]bool{}
	for _, x := range bundles {
		m, isObj := x.(obj)
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		if !isObj || !validUserID(id) || name == "" || seen[id] {
			valid = false
			break
		}
		seen[id] = true
	}
	if !valid {
		return customError("INDEX_INVALID", "Реестр пользовательских маршрутов повреждён.", "")
	}
	return nil
}

func (e *engine) cleanupPreviews() {
	root, err := platform.OpenDirNoFollow(e.previewsDir())
	if err != nil {
		return
	}
	defer root.Close()
	entries, err := root.ReadDir(-1)
	if err != nil {
		return
	}
	now := nowFn().Unix()
	for _, en := range entries {
		name := en.Name()
		if !en.IsDir() || !isHex64(name) {
			continue
		}
		func() {
			child, err := platform.OpenDirAtNoFollow(root, name)
			if err != nil {
				return
			}
			defer child.Close()
			b, err := platform.ReadRegularAtNoFollow(child, "report.json")
			if err != nil {
				return
			}
			r, ok := decodeObjBytes(b)
			if !ok || intOr(r["schemaVersion"], 0) != 1 || str(r, "token") != name || r["ready"] != true {
				return
			}
			created, ok := intOf(orNull(r, "createdEpoch"))
			if !ok || created <= 0 || int64(created) > now || now-int64(created) <= userPreviewTTL {
				return
			}
			_ = platform.RemoveOwnedDirTreeAtNoFollow(root, name, child)
		}()
	}
}

// userName is broray_user_routes_name_from_json: one line, trimmed.
// ok=false for a multi-line name (generic CLI failure in 3.x).
func userName(v any) (string, bool) {
	s, _ := v.(string)
	if strings.Contains(s, "\n") {
		return "", false
	}
	return strings.Trim(s, " \t\v\f\r"), true
}

func validUserName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] < 0x20 || name[i] == 0x7f {
			return false
		}
	}
	return true
}

func decodeBase64(s string) ([]byte, bool) {
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if b, err := base64.StdEncoding.DecodeString(clean); err == nil {
		return b, true
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "=")); err == nil {
		return b, true
	}
	return nil, false
}

// ---------- A2 preview ----------

func hCustomPreview(e *engine, c *cgi.Req) (any, error) {
	req, aerr := customBody(c)
	if aerr != nil {
		return nil, aerr
	}
	j, aerr := e.customJob("preview", "")
	if aerr != nil {
		return nil, aerr
	}
	data, perr := e.userPreview(req)
	endJob(j, perr)
	if perr != nil {
		return nil, perr
	}
	return data, nil
}

func (e *engine) userPreview(req obj) (obj, *cgi.APIError) {
	return e.userPreviewWithHook(req, nil)
}

func (e *engine) userPreviewWithHook(req obj, beforeStore func(string)) (obj, *cgi.APIError) {
	if err := e.userPrepareRuntime(); err != nil {
		return nil, err
	}
	e.cleanupPreviews()
	files, isArr := req["files"].([]any)
	_, nameStr := req["name"].(string)
	shapeOK := nameStr && isArr && len(files) > 0
	for _, f := range files {
		m, isObj := f.(obj)
		_, n := m["name"].(string)
		_, b := m["contentBase64"].(string)
		if !isObj || !n || !b {
			shapeOK = false
		}
	}
	if !shapeOK {
		return nil, customError("REQUEST_INVALID", "Некорректный запрос загрузки BAT-файлов.", "")
	}
	name, ok := userName(req["name"])
	if !ok {
		return nil, customGeneric("")
	}
	if !validUserName(name) {
		return nil, customError("NAME_INVALID", "Название набора должно содержать от 1 до 80 печатных символов.", "")
	}
	if len(files) > userMaxFiles {
		return nil, customError("TOO_MANY_FILES", fmt.Sprintf("Разрешено не более %d BAT-файлов в одном наборе.", userMaxFiles), "")
	}
	type srcFile struct {
		stored, original, sha string
		size, routeLines      int
		content               []byte
	}
	var srcs []srcFile
	var combined []string
	total, totalLines, normalizedLines := 0, 0, 0
	for i, f := range files {
		m := f.(obj)
		orig := str(m, "name")
		if orig == "" || utf8.RuneCountInString(orig) > 160 {
			return nil, customError("FILENAME_INVALID", "Некорректное имя загружаемого файла.", "")
		}
		if strings.ContainsAny(orig, "/\\") {
			return nil, customError("FILENAME_INVALID", "Имя файла не должно содержать путь.", "")
		}
		if !strings.HasSuffix(asciiLower(orig), ".bat") {
			return nil, customError("FILE_EXTENSION_INVALID", "Разрешены только файлы с расширением .bat.", "")
		}
		raw, ok := decodeBase64(str(m, "contentBase64"))
		if !ok {
			return nil, customError("FILE_DECODE_FAILED", "Не удалось декодировать файл "+orig+".", "")
		}
		if len(raw) == 0 {
			return nil, customError("FILE_EMPTY", "Файл "+orig+" пуст.", "")
		}
		total += len(raw)
		if total > userMaxBytes {
			return nil, customError("SOURCE_TOO_LARGE", "Общий размер файлов превышает 2 МБ.", "")
		}
		if strings.IndexByte(string(raw), 0) >= 0 {
			return nil, customError("FILE_NOT_TEXT", "Файл "+orig+" содержит нулевые байты и не является текстовым BAT-файлом.", "")
		}
		res := parseBat(raw, false, 20)
		if res.ErrorCount > 0 {
			lines := res.Errors
			if len(lines) > 12 {
				lines = lines[:12]
			}
			return nil, customError("FILE_REJECTED", "Файл "+orig+" не прошёл безопасную проверку. "+lines[0], strings.Join(lines[1:], "\n"))
		}
		if res.RouteLines == 0 {
			return nil, customError("FILE_REJECTED", "Файл "+orig+" не прошёл безопасную проверку. В файле не найдено ни одной допустимой команды route add.", "")
		}
		totalLines += res.RouteLines
		normalizedLines += res.NormalizedLines
		combined = append(combined, res.Routes...)
		srcs = append(srcs, srcFile{stored: fmt.Sprintf("%02d.bat", i+1), original: orig, sha: sha256Hex(raw), size: len(raw), routeLines: res.RouteLines, content: raw})
	}
	if totalLines > userMaxRoutes {
		return nil, customError("TOO_MANY_ROUTES", fmt.Sprintf("Количество строк маршрутов превышает лимит %d.", userMaxRoutes), "")
	}
	canonical := sortedUnique(combined)
	if len(canonical) == 0 {
		return nil, customError("NO_ROUTES", "В загруженных файлах не найдено маршрутов.", "")
	}
	if len(canonical) > userMaxRoutes {
		return nil, customError("TOO_MANY_ROUTES", fmt.Sprintf("Количество уникальных маршрутов превышает лимит %d.", userMaxRoutes), "")
	}
	optimized := collapseCIDR(canonical)
	if len(optimized) == 0 {
		return nil, customError("CIDR_OPTIMIZE_FAILED", "Не удалось безопасно оптимизировать CIDR для экспорта.", "")
	}
	broad := []any{}
	for _, r := range canonical {
		if _, p, ok := splitCIDR(r); ok && p <= 8 {
			broad = append(broad, r)
		}
	}
	normBytes, optBytes := listBytes(canonical), listBytes(optimized)
	normSHA, optSHA := sha256Hex(normBytes), sha256Hex(optBytes)
	now := nowFn()
	var meta strings.Builder
	sourceFiles := make([]any, len(srcs))
	for i, s := range srcs {
		fmt.Fprintf(&meta, "%d\t%s\t%s\t%d\t%d\n", i+1, base64.StdEncoding.EncodeToString([]byte(s.original)), s.sha, s.size, s.routeLines)
		sourceFiles[i] = obj{"storedName": s.stored, "originalName": s.original, "parser": "windows-route-bat", "sha256": s.sha,
			"sizeBytes": s.size, "routeLineCount": s.routeLines}
	}
	token := sha256Hex([]byte(fmt.Sprintf("%d\n%d\n%s\n%s\n%s\n%s%s", now.Unix(), os.Getpid(), name, normSHA, optSHA, meta.String(), compat.RandHex(16))))
	var warning any
	if len(broad) > 0 {
		warning = "Набор содержит очень широкие сети /7–/8. Через Xray может направляться значительная часть интернет-трафика."
	}
	report := obj{"schemaVersion": 1, "token": token, "name": name, "createdAt": compat.Stamp(now), "createdEpoch": now.Unix(),
		"expiresInSeconds": userPreviewTTL, "fileCount": len(files), "totalBytes": total, "sourceRouteLineCount": totalLines,
		"canonicalRouteCount": len(canonical), "exportRouteCount": len(optimized), "normalizedNetworkCount": normalizedLines,
		"duplicateCount": totalLines - len(canonical), "rejectedLineCount": 0, "broadRouteCount": len(broad), "broadRoutes": broad,
		"normalizedSha256": normSHA, "exportSha256": optSHA, "sourceFiles": sourceFiles, "ready": true, "warning": warning}
	root, err := platform.OpenDirNoFollow(e.previewsDir())
	if err != nil {
		return nil, customGeneric(err.Error())
	}
	defer root.Close()
	sameRoot := func() error {
		current, err := platform.OpenDirNoFollow(e.previewsDir())
		if err != nil {
			return err
		}
		defer current.Close()
		heldInfo, err := root.Stat()
		if err != nil {
			return err
		}
		currentInfo, err := current.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(heldInfo, currentInfo) {
			return fmt.Errorf("каталог предварительных наборов подменён")
		}
		return nil
	}
	if beforeStore != nil {
		beforeStore(token)
	}
	if err := sameRoot(); err != nil {
		return nil, customGeneric(err.Error())
	}
	if err := unix.Mkdirat(int(root.Fd()), token, 0o700); err != nil {
		return nil, customGeneric(err.Error())
	}
	owned, err := platform.OpenDirAtNoFollow(root, token)
	if err != nil {
		// The entry cannot be proved to be ours, so leave it for inspection.
		return nil, customGeneric("Созданный каталог не удалось подтвердить: " + err.Error())
	}
	defer owned.Close()
	fail := func(cause error) (obj, *cgi.APIError) {
		details := cause.Error()
		if cleanupErr := platform.RemoveOwnedDirTreeAtNoFollow(root, token, owned); cleanupErr != nil {
			details += "; очистка не подтверждена: " + cleanupErr.Error()
		}
		return nil, customGeneric(details)
	}
	if err := root.Sync(); err != nil {
		return fail(err)
	}
	if err := unix.Mkdirat(int(owned.Fd()), "source", 0o700); err != nil {
		return fail(err)
	}
	source, err := platform.OpenDirAtNoFollow(owned, "source")
	if err != nil {
		return fail(err)
	}
	defer source.Close()
	if err := owned.Sync(); err != nil {
		return fail(err)
	}
	for _, s := range srcs {
		if err := platform.WriteNewRegularAtNoFollow(source, s.stored, s.content, 0o600); err != nil {
			return fail(err)
		}
	}
	for _, file := range []struct {
		name string
		data []byte
	}{{"normalized.txt", normBytes}, {"optimized.txt", optBytes}} {
		if err := platform.WriteNewRegularAtNoFollow(owned, file.name, file.data, 0o600); err != nil {
			return fail(err)
		}
	}
	for _, file := range []struct {
		name  string
		value any
	}{{"source-files.json", sourceFiles}, {"report.json", report}} {
		data, err := compat.Marshal(file.value)
		if err != nil {
			return fail(err)
		}
		if err := platform.WriteNewRegularAtNoFollow(owned, file.name, data, 0o600); err != nil {
			return fail(err)
		}
	}
	if err := sameRoot(); err != nil {
		return fail(err)
	}
	current, err := platform.OpenDirAtNoFollow(root, token)
	if err != nil {
		return fail(err)
	}
	currentInfo, currentErr := current.Stat()
	closeErr := current.Close()
	ownedInfo, ownedErr := owned.Stat()
	if currentErr != nil || closeErr != nil || ownedErr != nil || !os.SameFile(currentInfo, ownedInfo) {
		return fail(fmt.Errorf("каталог предварительного набора подменён"))
	}
	return report, nil
}

// verifySourceFiles is broray_user_routes_verify_source_files.
func verifySourceFiles(dir string, meta any) bool {
	source, err := platform.OpenDirNoFollow(dir)
	if err != nil {
		return false
	}
	defer source.Close()
	_, ok := readVerifiedSourceFilesAt(source, meta)
	return ok
}

type previewSourceFile struct {
	name string
	data []byte
}

func readVerifiedSourceFilesAt(source *os.File, meta any) ([]previewSourceFile, bool) {
	list, ok := meta.([]any)
	if !ok || len(list) == 0 {
		return nil, false
	}
	files := make([]previewSourceFile, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, x := range list {
		m, _ := x.(obj)
		stored, sha := str(m, "storedName"), str(m, "sha256")
		size, ok := numOf(orNull(m, "sizeBytes"))
		if stored == "" || !ok || size <= 0 || !isHex64(sha) || !strings.HasSuffix(stored, ".bat") || strings.HasPrefix(stored, ".") ||
			strings.Contains(stored, "..") || strings.ContainsAny(stored, "/\\") || seen[stored] {
			return nil, false
		}
		for _, c := range stored {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '_' || c == '-') {
				return nil, false
			}
		}
		b, err := platform.ReadRegularAtNoFollow(source, stored)
		if err != nil || float64(len(b)) != size || sha256Hex(b) != sha {
			return nil, false
		}
		seen[stored] = true
		files = append(files, previewSourceFile{name: stored, data: b})
	}
	return files, true
}

func (e *engine) previewValid(dir string) bool {
	return e.previewValidWithHook(dir, nil)
}

func (e *engine) previewValidWithHook(dir string, afterPreflight func()) bool {
	_, ok := e.loadPreviewWithHook(dir, afterPreflight)
	return ok
}

type previewSnapshot struct {
	report      obj
	sourceFiles any
	top         map[string][]byte
	sources     []previewSourceFile
}

func (e *engine) loadPreviewWithHook(dir string, afterPreflight func()) (previewSnapshot, bool) {
	for _, f := range []string{"report.json", "normalized.txt", "optimized.txt", "source-files.json"} {
		if !regularFile(filepath.Join(dir, f)) {
			return previewSnapshot{}, false
		}
	}
	root, err := platform.OpenDirNoFollow(dir)
	if err != nil {
		return previewSnapshot{}, false
	}
	defer root.Close()
	if afterPreflight != nil {
		afterPreflight()
	}
	readTop := func(name string) ([]byte, bool) {
		b, err := platform.ReadRegularAtNoFollow(root, name)
		return b, err == nil
	}
	metaBytes, ok := readTop("source-files.json")
	if !ok {
		return previewSnapshot{}, false
	}
	meta, err := decodeAny(metaBytes)
	if err != nil {
		return previewSnapshot{}, false
	}
	source, err := platform.OpenDirAtNoFollow(root, "source")
	if err != nil {
		return previewSnapshot{}, false
	}
	defer source.Close()
	sources, ok := readVerifiedSourceFilesAt(source, meta)
	if !ok {
		return previewSnapshot{}, false
	}
	reportBytes, ok := readTop("report.json")
	if !ok {
		return previewSnapshot{}, false
	}
	r, ok := decodeObjBytes(reportBytes)
	_, n1 := numOf(orNull(r, "canonicalRouteCount"))
	_, n2 := numOf(orNull(r, "exportRouteCount"))
	_, arr := r["sourceFiles"].([]any)
	if !ok || intOr(r["schemaVersion"], 0) != 1 || r["ready"] != true || str(r, "token") == "" || !n1 || !n2 || !arr {
		return previewSnapshot{}, false
	}
	nb, normalOK := readTop("normalized.txt")
	ob, optimizedOK := readTop("optimized.txt")
	if !normalOK || !optimizedOK || sha256Hex(nb) != str(r, "normalizedSha256") || sha256Hex(ob) != str(r, "exportSha256") {
		return previewSnapshot{}, false
	}
	current, err := platform.OpenDirNoFollow(dir)
	if err != nil {
		return previewSnapshot{}, false
	}
	defer current.Close()
	heldInfo, heldErr := root.Stat()
	currentInfo, currentErr := current.Stat()
	if heldErr != nil || currentErr != nil || !os.SameFile(heldInfo, currentInfo) {
		return previewSnapshot{}, false
	}
	return previewSnapshot{report: r, sourceFiles: meta,
		top:     map[string][]byte{"report.json": reportBytes, "source-files.json": metaBytes, "normalized.txt": nb, "optimized.txt": ob},
		sources: sources}, true
}

// ---------- A3 commit ----------

func hCustomCommit(e *engine, c *cgi.Req) (any, error) {
	req, aerr := customBody(c)
	if aerr != nil {
		return nil, aerr
	}
	j, aerr := e.customJob("commit", "")
	if aerr != nil {
		return nil, aerr
	}
	data, perr := e.userCommit(req)
	endJob(j, perr)
	if perr != nil {
		return nil, perr
	}
	return data, nil
}

func (e *engine) userBundleExists(id string) bool { _, ok := e.customEntry(id); return ok }

func (e *engine) userCommit(req obj) (obj, *cgi.APIError) { return e.userCommitAs(req, "") }

// userCommitAs is userCommit; newID, when set, is the id of a new bundle
// instead of the token-derived one (settings backup restores keep their ids).
func (e *engine) userCommitAs(req obj, newID string) (obj, *cgi.APIError) {
	return e.userCommitAsWithWriter(req, newID, writeObj)
}

func (e *engine) userCommitAsWithWriter(req obj, newID string, writer func(string, any) error, stageHook ...func(string)) (obj, *cgi.APIError) {
	if writer == nil {
		return nil, customError("COMMIT_FAILED", "Запись пользовательского набора недоступна.", "")
	}
	if err := e.userPrepareRuntime(); err != nil {
		return nil, err
	}
	_, t := req["token"].(string)
	_, n := req["name"].(string)
	_, bs := req["bundleId"].(string)
	if !t || !n || !(req["bundleId"] == nil || bs) {
		return nil, customError("REQUEST_INVALID", "Некорректный запрос сохранения пользовательского набора.", "")
	}
	token := str(req, "token")
	name, nameOK := userName(req["name"])
	bundleID := str(req, "bundleId")
	if !isHex64(token) {
		return nil, customError("TOKEN_INVALID", "Некорректный токен предварительной проверки.", "")
	}
	if !nameOK || !validUserName(name) {
		return nil, customError("NAME_INVALID", "Некорректное название пользовательского набора.", "")
	}
	pdir := filepath.Join(e.previewsDir(), token)
	ownedPreview, err := platform.OpenDirNoFollow(pdir)
	if err != nil {
		return nil, customError("PREVIEW_INVALID", "Предварительная проверка отсутствует, истекла или повреждена.", "")
	}
	defer ownedPreview.Close()
	preview, ok := e.loadPreviewWithHook(pdir, nil)
	if !ok {
		return nil, customError("PREVIEW_INVALID", "Предварительная проверка отсутствует, истекла или повреждена.", "")
	}
	if len(stageHook) > 1 && stageHook[1] != nil {
		stageHook[1](token)
	}
	report := preview.report
	created, _ := intOf(report["createdEpoch"])
	if nowFn().Unix()-int64(created) > userPreviewTTL {
		return nil, customError("PREVIEW_EXPIRED", "Срок предварительной проверки истёк. Проверьте файлы повторно.", "")
	}
	replacing := false
	if bundleID != "" {
		if !validUserID(bundleID) {
			return nil, customError("BUNDLE_INVALID", "Некорректный идентификатор набора.", "")
		}
		if !e.userBundleExists(bundleID) {
			return nil, customError("BUNDLE_NOT_FOUND", "Пользовательский набор не найден.", "")
		}
		replacing = true
	} else if newID != "" {
		if !validUserID(newID) || e.userBundleExists(newID) {
			return nil, customError("BUNDLE_INVALID", "Некорректный идентификатор набора.", "")
		}
		bundleID = newID
	} else {
		bundleID = "user-" + token[:16]
		for n := 1; e.userBundleExists(bundleID); n++ {
			bundleID = "user-" + token[:12] + "-" + itoa(n)
		}
	}
	if !e.importMu.TryLock() {
		return nil, customError("ROUTES_BUSY", "Другая операция с маршрутами уже выполняется.", "")
	}
	defer e.importMu.Unlock()
	now := stamp()
	iface := e.managedInterface()
	if len(iface) < 6 || !strings.HasPrefix(iface, "Proxy") || iface[5] < '0' || iface[5] > '9' { // case Proxy[0-9]*
		return nil, customError("ROUTES_CONFIG_INVALID", "Некорректный управляемый интерфейс Keenetic.", "")
	}
	canonicalCount := intOr(report["canonicalRouteCount"], 0)
	exportCount := intOr(report["exportRouteCount"], 0)
	canonicalSHA, exportSHA := str(report, "normalizedSha256"), str(report, "exportSha256")
	fileCount := intOr(report["fileCount"], 0)
	sourceFiles := preview.sourceFiles
	optimizedText := strings.TrimRight(string(preview.top["optimized.txt"]), "\n")
	var optimized []string
	if optimizedText != "" {
		optimized = strings.Split(optimizedText, "\n")
	}
	routes := make([]any, 0, len(optimized))
	for _, r := range optimized {
		net, p, _ := splitCIDR(r)
		routes = append(routes, obj{"family": "ipv4", "network": net, "prefix": p})
	}
	manifest := obj{"schemaVersion": 1, "id": bundleID, "name": name, "description": userDesc,
		"source": obj{"provider": "local-upload", "files": sourceFiles}, "targetInterface": iface, "exportComment": "BROray",
		"limits": obj{"maxSourceBytes": userMaxBytes, "maxRoutes": userMaxRoutes}, "createdAt": now, "updatedAt": now}
	version := obj{"schemaVersion": 1, "bundleId": bundleID, "sourceCommit": canonicalSHA, "sourceDate": now, "contentSha256": exportSHA,
		"routeCount": exportCount, "canonicalRouteCount": canonicalCount, "sourceFileCount": fileCount, "sourceFiles": sourceFiles,
		"downloadedAt": now, "targetInterface": iface, "sourceProvider": "local-upload"}
	checkResult := obj{"result": "local_validated", "message": "Пользовательские BAT-файлы проверены.", "sourceFiles": sourceFiles,
		"canonicalRouteCount": canonicalCount, "exportRouteCount": exportCount}
	downloadResult := obj{"result": "local_saved", "message": "Пользовательский набор сохранён и готов к экспорту.",
		"routeCount": exportCount, "canonicalRouteCount": canonicalCount, "sourceFileCount": fileCount}
	available := obj{"sourceCommit": canonicalSHA, "sourceDate": now, "contentSha256": exportSHA, "sourceFiles": sourceFiles}
	downloaded := obj{"sourceCommit": canonicalSHA, "sourceDate": now, "contentSha256": exportSHA}
	var state obj
	if old, ok := readObj(e.stateFile(bundleID)); replacing && ok {
		state = old
		state["schemaVersion"], state["bundleId"], state["status"] = 1, bundleID, "downloaded"
		state["availableVersion"], state["downloadedVersion"], state["routeCount"] = available, downloaded, exportCount
		state["lastCheckedAt"], state["lastDownloadedAt"], state["lastError"] = now, now, nil
		state["checkResult"], state["downloadResult"], state["customImport"], state["updatedAt"] = checkResult, downloadResult, report, now
	} else {
		state = obj{"schemaVersion": 1, "bundleId": bundleID, "status": "downloaded", "availableVersion": available,
			"downloadedVersion": downloaded, "installedVersion": nil, "routeCount": exportCount, "lastCheckedAt": now,
			"lastDownloadedAt": now, "lastExportedAt": nil, "lastDeletedAt": nil, "lastError": nil, "checkResult": checkResult,
			"downloadResult": downloadResult, "exportBuild": nil, "preflight": nil, "exportResult": nil, "deleteResult": nil,
			"customImport": report, "updatedAt": now}
	}
	idx, _ := readObj(e.customFile())
	var entries []any
	createdAt := any(now)
	for _, x := range anyList(idx["bundles"]) {
		m, _ := x.(obj)
		if str(m, "id") == bundleID {
			if m["createdAt"] != nil {
				createdAt = m["createdAt"]
			}
			continue
		}
		entries = append(entries, m)
	}
	entries = append(entries, obj{"id": bundleID, "name": name, "description": userDesc, "sourceProvider": "local-upload",
		"canonicalRouteCount": canonicalCount, "exportRouteCount": exportCount, "sourceFileCount": fileCount,
		"createdAt": createdAt, "updatedAt": now})
	idx["schemaVersion"], idx["bundles"] = 1, entries
	bl, ok := readObj(e.bundlesFile())
	if !ok {
		bl = obj{}
	}
	ids := strList(bl["bundles"])
	if !e.bundleInList(bundleID) {
		ids = append(ids, bundleID)
	}
	bl["schemaVersion"], bl["bundles"] = 1, toAny(ids)
	registry, regOK := readObj(e.registryFile(bundleID))
	if !regOK {
		registry = seedRegistry(bundleID, iface)
	}
	// stage the catalog directory
	target := e.catalogDir(bundleID)
	catalogParent := filepath.Dir(target)
	catalog, err := platform.OpenDirNoFollow(catalogParent)
	if err != nil {
		return nil, customError("COMMIT_FAILED", "Каталог пользовательских наборов не подтверждён.", err.Error())
	}
	defer catalog.Close()
	stageName, err := platform.MkdirTempAtNoFollow(catalog, filepath.Base(target)+".new.", 0o700)
	if err != nil {
		details := err.Error()
		if stageName != "" {
			details += "; запись временного каталога сохранена для проверки: " + stageName
		}
		return nil, customError("COMMIT_FAILED", "Не удалось создать временный каталог пользовательского набора.", details)
	}
	if len(stageHook) > 8 && stageHook[8] != nil {
		stageHook[8](stageName)
	}
	ownedStage, err := platform.OpenDirAtNoFollow(catalog, stageName)
	if err != nil {
		if len(stageHook) > 9 && stageHook[9] != nil {
			stageHook[9](stageName)
		}
		details := err.Error() + "; запись временного каталога сохранена для проверки: " + stageName
		return nil, customError("COMMIT_FAILED", "Временный каталог пользовательского набора изменился.", details)
	}
	defer ownedStage.Close()
	if len(stageHook) > 0 && stageHook[0] != nil {
		stageHook[0](stageName)
	}
	commitFailDetails := func(details string) (obj, *cgi.APIError) {
		if err := platform.RemoveOwnedDirTreeAtNoFollow(catalog, stageName, ownedStage); err != nil && !errors.Is(err, unix.ENOENT) {
			return nil, customError("COMMIT_FAILED", "Не удалось атомарно сохранить пользовательский набор; временные данные сохранены для проверки.", details+"; очистка: "+err.Error())
		}
		return nil, customError("COMMIT_FAILED", "Не удалось атомарно сохранить пользовательский набор; прежнее состояние восстановлено.", details)
	}
	commitFail := func() (obj, *cgi.APIError) { return commitFailDetails("") }
	sameCatalogEntry := func(name string, held *os.File) error {
		current, err := platform.OpenDirAtNoFollow(catalog, name)
		if err != nil {
			return err
		}
		defer current.Close()
		expected, err := held.Stat()
		if err != nil {
			return err
		}
		actual, err := current.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(expected, actual) {
			return fmt.Errorf("каталог %s подменён", name)
		}
		return nil
	}
	sameCatalogParent := func() error {
		current, err := platform.OpenDirNoFollow(catalogParent)
		if err != nil {
			return err
		}
		defer current.Close()
		expected, err := catalog.Stat()
		if err != nil {
			return err
		}
		actual, err := current.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(expected, actual) {
			return fmt.Errorf("каталог пользовательских наборов подменён")
		}
		return nil
	}
	if err := unix.Mkdirat(int(ownedStage.Fd()), "source", 0o700); err != nil {
		return commitFailDetails(err.Error())
	}
	stageSource, err := platform.OpenDirAtNoFollow(ownedStage, "source")
	if err != nil {
		return commitFailDetails(err.Error())
	}
	defer stageSource.Close()
	for _, sourceFile := range preview.sources {
		if err := platform.WriteNewRegularAtNoFollow(stageSource, sourceFile.name, sourceFile.data, 0o600); err != nil {
			return commitFailDetails(err.Error())
		}
	}
	for _, cp := range [][2]string{{"normalized.txt", "canonical.txt"}, {"optimized.txt", "normalized.txt"}, {"source-files.json", "source-files.json"}, {"report.json", "import-report.json"}} {
		if err := platform.WriteNewRegularAtNoFollow(ownedStage, cp[1], preview.top[cp[0]], 0o600); err != nil {
			return commitFailDetails(err.Error())
		}
	}
	for _, item := range []struct {
		name  string
		value any
	}{{"manifest.json", manifest}, {"routes.json", obj{"schemaVersion": 1, "bundleId": bundleID, "targetInterface": iface, "routeComment": "BROray",
		"contentSha256": exportSHA, "routeCount": len(routes), "routes": routes}}, {"version.json", version}} {
		b, err := compat.Marshal(item.value)
		if err != nil {
			return commitFailDetails(err.Error())
		}
		if err := platform.WriteNewRegularAtNoFollow(ownedStage, item.name, b, 0o600); err != nil {
			return commitFailDetails(err.Error())
		}
	}
	if err := sameCatalogParent(); err != nil {
		details := err.Error()
		if cleanupErr := platform.RemoveOwnedDirTreeAtNoFollow(catalog, stageName, ownedStage); cleanupErr != nil {
			details += "; очистка не подтверждена: " + cleanupErr.Error()
		}
		return nil, customError("COMMIT_FAILED", "Каталог пользовательских наборов изменился; состояние не подтверждено.", details)
	}
	// back up every target, then apply; on failure restore all
	bak := backupFiles(e.manifestFile(bundleID), e.stateFile(bundleID), e.registryFile(bundleID), e.bundlesFile(), e.customFile())
	if bak == nil {
		return commitFail()
	}
	targetName := filepath.Base(target)
	legacyBackupName := targetName + ".bak." + itoa(os.Getpid())
	var occupied unix.Stat_t
	if err := unix.Fstatat(int(catalog.Fd()), legacyBackupName, &occupied, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return commitFailDetails("Резервный каталог с предсказуемым именем уже занят; владение не подтверждено.")
	} else if !errors.Is(err, unix.ENOENT) {
		return commitFailDetails("Не удалось проверить резервный каталог: " + err.Error())
	}
	ownedTarget, targetErr := platform.OpenDirAtNoFollow(catalog, targetName)
	hadCat := targetErr == nil
	if hadCat {
		defer ownedTarget.Close()
	} else if !errors.Is(targetErr, unix.ENOENT) {
		return commitFailDetails("Каталог набора не подтверждён: " + targetErr.Error())
	}
	if hadCat != replacing {
		return commitFailDetails("Каталог набора изменился после проверки; владение не подтверждено.")
	}
	backupName := ""
	var backupPlaceholder *os.File
	if hadCat {
		backupName, err = platform.MkdirTempAtNoFollow(catalog, targetName+".bak.", 0o700)
		if err != nil {
			details := "Не удалось подготовить резервный каталог: " + err.Error()
			if backupName != "" {
				details += "; запись резервного каталога сохранена для проверки: " + backupName
			}
			return commitFailDetails(details)
		}
		if len(stageHook) > 10 && stageHook[10] != nil {
			stageHook[10](backupName)
		}
		backupPlaceholder, err = platform.OpenDirAtNoFollow(catalog, backupName)
		if err != nil {
			if len(stageHook) > 11 && stageHook[11] != nil {
				stageHook[11](backupName)
			}
			details := "Резервный каталог изменился: " + err.Error() + "; запись сохранена для проверки: " + backupName
			return commitFailDetails(details)
		}
		defer backupPlaceholder.Close()
	}
	if hadCat && len(stageHook) > 6 && stageHook[6] != nil {
		stageHook[6](backupName)
	}
	var applyErr error
	ownershipUncertain := false
	movedOld, movedStage := false, false
	if err := sameCatalogParent(); err != nil {
		applyErr, ownershipUncertain = err, true
	}
	if hadCat && applyErr == nil {
		if err := sameCatalogEntry(targetName, ownedTarget); err != nil {
			applyErr, ownershipUncertain = err, true
		} else if err := sameCatalogEntry(backupName, backupPlaceholder); err != nil {
			applyErr, ownershipUncertain = err, true
		} else {
			if len(stageHook) > 5 && stageHook[5] != nil {
				stageHook[5](backupName)
			}
			if err := platform.RemoveOwnedDirTreeAtNoFollow(catalog, backupName, backupPlaceholder); err != nil {
				applyErr, ownershipUncertain = err, true
			} else {
				applyErr = unix.Renameat2(int(catalog.Fd()), targetName, int(catalog.Fd()), backupName, unix.RENAME_NOREPLACE)
				movedOld = applyErr == nil
			}
			if movedOld {
				if err := sameCatalogEntry(backupName, ownedTarget); err != nil {
					applyErr, ownershipUncertain = err, true
				}
			}
		}
	}
	if applyErr == nil {
		if err := sameCatalogParent(); err != nil {
			applyErr, ownershipUncertain = err, true
		} else if err := sameCatalogEntry(stageName, ownedStage); err != nil {
			applyErr, ownershipUncertain = err, true
		} else {
			if len(stageHook) > 2 && stageHook[2] != nil {
				stageHook[2](targetName)
			}
			applyErr = unix.Renameat2(int(catalog.Fd()), stageName, int(catalog.Fd()), targetName, unix.RENAME_NOREPLACE)
			movedStage = applyErr == nil
			if movedStage {
				if err := sameCatalogEntry(targetName, ownedStage); err != nil {
					applyErr, ownershipUncertain = err, true
				}
			}
		}
	}
	if applyErr == nil {
		for _, w := range []struct {
			p string
			v any
		}{{e.manifestFile(bundleID), manifest}, {e.stateFile(bundleID), state}, {e.registryFile(bundleID), registry},
			{e.bundlesFile(), bl}, {e.customFile(), idx}} {
			if err := writer(w.p, w.v); err != nil {
				applyErr = err
				break
			}
		}
	}
	if applyErr == nil {
		if err := sameCatalogParent(); err != nil {
			applyErr, ownershipUncertain = err, true
		} else if err := sameCatalogEntry(targetName, ownedStage); err != nil {
			applyErr, ownershipUncertain = err, true
		}
	}
	if applyErr != nil {
		problems := []string{applyErr.Error()}
		if ownershipUncertain {
			problems = append(problems, "владение каталогом набора не подтверждено")
		}
		targetCleared := !movedStage
		if movedStage {
			if err := sameCatalogEntry(targetName, ownedStage); err != nil {
				problems = append(problems, "новый каталог подменён: "+err.Error())
			} else {
				if len(stageHook) > 4 && stageHook[4] != nil {
					stageHook[4](targetName)
				}
				if err := platform.RemoveOwnedDirTreeAtNoFollow(catalog, targetName, ownedStage); err != nil {
					problems = append(problems, "новый каталог не удалён: "+err.Error())
				} else {
					targetCleared = true
				}
			}
		}
		if movedOld {
			if !targetCleared {
				problems = append(problems, "прежний каталог сохранён в резерве до проверки нового")
			} else if err := sameCatalogEntry(backupName, ownedTarget); err != nil {
				problems = append(problems, "резервный каталог подменён: "+err.Error())
			} else if err := unix.Renameat2(int(catalog.Fd()), backupName, int(catalog.Fd()), targetName, unix.RENAME_NOREPLACE); err != nil {
				problems = append(problems, "прежний каталог не восстановлен: "+err.Error())
			}
		} else if backupName != "" {
			if err := sameCatalogEntry(backupName, backupPlaceholder); err != nil {
				problems = append(problems, "пустой резервный каталог подменён: "+err.Error())
			} else {
				if len(stageHook) > 7 && stageHook[7] != nil {
					stageHook[7](backupName)
				}
				if err := platform.RemoveOwnedDirTreeAtNoFollow(catalog, backupName, backupPlaceholder); err != nil {
					problems = append(problems, "пустой резервный каталог не удалён: "+err.Error())
				}
			}
		}
		if !bak.restore() {
			problems = append(problems, "локальные данные не восстановлены")
		}
		if err := platform.RemoveOwnedDirTreeAtNoFollow(catalog, stageName, ownedStage); err != nil && !errors.Is(err, unix.ENOENT) {
			problems = append(problems, "временный каталог не удалён: "+err.Error())
		}
		if len(problems) > 1 {
			return nil, customError("COMMIT_FAILED", "Не удалось сохранить пользовательский набор; восстановление не подтверждено.", strings.Join(problems, "\n"))
		}
		return nil, customError("COMMIT_FAILED", "Не удалось атомарно сохранить пользовательский набор; прежнее состояние восстановлено.", applyErr.Error())
	}
	var warnings []string
	if backupName != "" {
		if err := sameCatalogEntry(backupName, ownedTarget); err != nil {
			warnings = append(warnings, "резервный каталог изменён и сохранён: "+err.Error())
		} else {
			if len(stageHook) > 3 && stageHook[3] != nil {
				stageHook[3](backupName)
			}
			if err := platform.RemoveOwnedDirTreeAtNoFollow(catalog, backupName, ownedTarget); err != nil {
				warnings = append(warnings, "резервный каталог сохранён: "+err.Error())
			}
		}
	}
	previewParent, err := platform.OpenDirNoFollow(filepath.Dir(pdir))
	if err != nil {
		warnings = append(warnings, "временные файлы проверки сохранены: "+err.Error())
	} else {
		if cleanupErr := platform.RemoveOwnedDirTreeAtNoFollow(previewParent, filepath.Base(pdir), ownedPreview); cleanupErr != nil {
			warnings = append(warnings, "временные файлы проверки сохранены: "+cleanupErr.Error())
		}
		if closeErr := previewParent.Close(); closeErr != nil {
			warnings = append(warnings, "закрытие каталога проверки не подтверждено: "+closeErr.Error())
		}
	}
	result := obj{"bundleId": bundleID, "name": name, "replaced": replacing, "canonicalRouteCount": canonicalCount,
		"exportRouteCount": exportCount, "sourceFileCount": fileCount, "status": "downloaded", "readyForExport": true, "updatedAt": now}
	if len(warnings) > 0 {
		result["cleanupWarning"] = strings.Join(warnings, "; ")
	}
	return result, nil
}

func copyRegularFilesBetweenHeld(source, destination *os.File) error {
	entries, err := source.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		b, err := platform.ReadRegularAtNoFollow(source, entry.Name())
		if err != nil {
			return err
		}
		if err := platform.WriteNewRegularAtNoFollow(destination, entry.Name(), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return copyDirWithHook(src, dst, nil)
}

func copyDirWithHook(src, dst string, beforeCopy func()) error {
	if err := platform.MkdirAllDurable(dst, 0o700); err != nil {
		return err
	}
	source, err := platform.OpenDirNoFollow(src)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := platform.OpenDirNoFollow(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	sourceInfo, err := source.Stat()
	if err != nil {
		return err
	}
	destinationInfo, err := destination.Stat()
	if err != nil {
		return err
	}
	verify := func(path string, held os.FileInfo) error {
		current, err := platform.OpenDirNoFollow(path)
		if err != nil {
			return err
		}
		defer current.Close()
		info, err := current.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(held, info) {
			return fmt.Errorf("каталог изменился во время копирования: %s", path)
		}
		return nil
	}
	if beforeCopy != nil {
		beforeCopy()
	}
	if err := verify(src, sourceInfo); err != nil {
		return err
	}
	if err := verify(dst, destinationInfo); err != nil {
		return err
	}
	entries, err := source.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, en := range entries {
		if en.IsDir() {
			continue
		}
		if err := verify(src, sourceInfo); err != nil {
			return err
		}
		if err := verify(dst, destinationInfo); err != nil {
			return err
		}
		b, err := platform.ReadRegularAtNoFollow(source, en.Name())
		if err != nil {
			return err
		}
		fd, err := unix.Openat(int(destination.Fd()), en.Name(), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), filepath.Join(dst, en.Name()))
		n, writeErr := file.Write(b)
		if writeErr == nil && n != len(b) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := verify(dst, destinationInfo); err != nil {
			return err
		}
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	if err := verify(dst, destinationInfo); err != nil {
		return err
	}
	return nil
}

// ---------- A4 validate ----------

func hCustomValidate(e *engine, c *cgi.Req) (any, error) {
	id, aerr := customBundleFromQuery(c.RawQuery)
	if aerr != nil {
		return nil, aerr
	}
	j, aerr := e.customJob("validate", id)
	if aerr != nil {
		return nil, aerr
	}
	data, perr := e.userValidate(id)
	endJob(j, perr)
	if perr != nil {
		return nil, perr
	}
	return data, nil
}

func (e *engine) userValidate(id string) (obj, *cgi.APIError) {
	if err := e.userPrepareRuntime(); err != nil {
		return nil, err
	}
	if !validUserID(id) {
		return nil, customError("BUNDLE_INVALID", "Некорректный идентификатор набора.", "")
	}
	if !e.userBundleExists(id) {
		return nil, customError("BUNDLE_NOT_FOUND", "Пользовательский набор не найден.", "")
	}
	cat := e.catalogDir(id)
	damaged := func(msg string) (obj, *cgi.APIError) { return nil, customError("BUNDLE_DAMAGED", msg, "") }
	for _, f := range []string{"import-report.json", "canonical.txt", "normalized.txt", "routes.json", "version.json", "source-files.json"} {
		if !fileExists(filepath.Join(cat, f)) {
			return damaged("Файлы пользовательского набора отсутствуют.")
		}
	}
	if st, err := os.Stat(filepath.Join(cat, "source")); err != nil || !st.IsDir() || !fileExists(e.stateFile(id)) {
		return damaged("Файлы пользовательского набора отсутствуют.")
	}
	var meta any
	if readJSONAny(filepath.Join(cat, "source-files.json"), &meta) != nil || !verifySourceFiles(filepath.Join(cat, "source"), meta) {
		return damaged("Исходные BAT-файлы пользовательского набора изменены или отсутствуют.")
	}
	report, ok := readObj(filepath.Join(cat, "import-report.json"))
	sf, isArr := report["sourceFiles"].([]any)
	_, n1 := numOf(orNull(report, "canonicalRouteCount"))
	_, n2 := numOf(orNull(report, "exportRouteCount"))
	_, s1 := report["normalizedSha256"].(string)
	_, s2 := report["exportSha256"].(string)
	if !ok || intOr(report["schemaVersion"], 0) != 1 || report["ready"] != true || !isArr || intOr(report["fileCount"], -1) != len(sf) || !n1 || !n2 || !s1 || !s2 {
		return damaged("Отчёт импорта пользовательского набора повреждён.")
	}
	state, ok := readObj(e.stateFile(id))
	_, dvObj := state["downloadedVersion"].(obj)
	_, ivObj := state["installedVersion"].(obj)
	if !ok || intOr(state["schemaVersion"], 0) != 1 || str(state, "bundleId") != id || !dvObj || !(state["installedVersion"] == nil || ivObj) {
		return damaged("Состояние пользовательского набора повреждено.")
	}
	canon, err1 := os.ReadFile(filepath.Join(cat, "canonical.txt"))
	opt, err2 := os.ReadFile(filepath.Join(cat, "normalized.txt"))
	countLines := func(b []byte) int { return strings.Count(string(b), "\n") }
	if err1 != nil || err2 != nil || sha256Hex(canon) != str(report, "normalizedSha256") || sha256Hex(opt) != str(report, "exportSha256") ||
		countLines(canon) != intOr(report["canonicalRouteCount"], -1) || countLines(opt) != intOr(report["exportRouteCount"], -1) {
		return damaged("Контрольная сумма или количество маршрутов пользовательского набора не совпало.")
	}
	exportCount := intOr(report["exportRouteCount"], -1)
	r, ok := readObj(filepath.Join(cat, "routes.json"))
	if !ok || intOr(r["schemaVersion"], 0) != 1 || str(r, "bundleId") != id || str(r, "contentSha256") != str(report, "exportSha256") ||
		intOr(r["routeCount"], -1) != exportCount || len(anyList(r["routes"])) != exportCount {
		return damaged("Каталог экспорта пользовательского набора повреждён.")
	}
	if _, isArr := r["routes"].([]any); !isArr {
		return damaged("Каталог экспорта пользовательского набора повреждён.")
	}
	v, ok := readObj(filepath.Join(cat, "version.json"))
	if !ok || intOr(v["schemaVersion"], 0) != 1 || str(v, "bundleId") != id || str(v, "contentSha256") != str(report, "exportSha256") ||
		intOr(v["routeCount"], -1) != exportCount || str(v, "sourceProvider") != "local-upload" {
		return damaged("Сведения о версии пользовательского набора повреждены.")
	}
	now := stamp()
	state["lastCheckedAt"] = now
	state["lastError"] = nil
	state["checkResult"] = obj{"result": "local_validated", "message": "Исходные и нормализованные файлы пользовательского набора проверены.",
		"sourceFiles": report["sourceFiles"], "canonicalRouteCount": report["canonicalRouteCount"], "exportRouteCount": report["exportRouteCount"]}
	state["customImport"] = report
	state["updatedAt"] = now
	if writeObj(e.stateFile(id), state) != nil {
		return nil, customGeneric("")
	}
	report["validatedAt"] = now
	return report, nil
}

// ---------- A5 remove ----------

func hCustomRemove(e *engine, c *cgi.Req) (any, error) {
	id, aerr := customBundleFromQuery(c.RawQuery)
	if aerr != nil {
		return nil, aerr
	}
	j, aerr := e.admit("custom:remove-finalize", id, msgBusyRemove)
	if aerr != nil {
		if aerr.Code == "ROUTES_OPERATION_BUSY" {
			return nil, aerr
		}
		return nil, cgi.E(500, "ROUTES_API_LOCK_FAILED", "Не удалось установить блокировку удаления.")
	}
	data, perr := e.userRemove(c, id)
	endJob(j, perr)
	if perr != nil {
		return nil, perr
	}
	return data, nil
}

func (e *engine) userRemove(c *cgi.Req, id string) (obj, *cgi.APIError) {
	if p, ok := readObj(e.progressFile(id)); ok && (p["running"] == true || p["resumable"] == true) {
		return nil, cgi.E(409, "ROUTES_CUSTOM_REMOVE_PROGRESS_PENDING", "Сначала завершите или продолжите текущую операцию с этим набором.")
	}
	requires := false
	if s, ok := readObj(e.stateFile(id)); ok && s["installedVersion"] != nil {
		requires = true
	}
	if r, ok := readObj(e.registryFile(id)); ok && (r["installedVersion"] != nil || len(anyList(r["managedRouteKeys"])) > 0) {
		requires = true
	}
	if p, ok := e.presence(c.R.Context(), id, 5*time.Second); ok && p["registered"] == true &&
		(p["actualInstalled"] == true || intOr(p["presentRouteCount"], 0) > 0) {
		requires = true
	}
	if requires {
		return nil, cgi.E(409, "ROUTES_CUSTOM_REMOVE_REQUIRES_DELETE", "Сначала безопасно удалите маршруты набора из Keenetic, затем удалите локальную карточку.")
	}
	if err := e.userPrepareRuntime(); err != nil {
		return nil, err
	}
	if !e.userBundleExists(id) {
		return nil, customError("BUNDLE_NOT_FOUND", "Пользовательский набор не найден.", "")
	}
	state, ok := readObj(e.stateFile(id))
	_, ivObj := state["installedVersion"].(obj)
	if !ok || intOr(state["schemaVersion"], 0) != 1 || str(state, "bundleId") != id || !(state["installedVersion"] == nil || ivObj) {
		return nil, customError("BUNDLE_DAMAGED", "Состояние пользовательского набора отсутствует или повреждено.", "")
	}
	reg, ok := readObj(e.registryFile(id))
	_, a1 := reg["routeKeys"].([]any)
	_, a2 := reg["managedRouteKeys"].([]any)
	_, a3 := reg["externalRouteKeys"].([]any)
	_, rivObj := reg["installedVersion"].(obj)
	if !ok || intOr(reg["schemaVersion"], 0) != 1 || str(reg, "bundleId") != id || !(reg["installedVersion"] == nil || rivObj) || !a1 || !a2 || !a3 {
		return nil, customError("BUNDLE_DAMAGED", "Реестр установки пользовательского набора отсутствует или повреждён.", "")
	}
	if !e.importMu.TryLock() {
		return nil, customError("ROUTES_BUSY", "Другая операция с маршрутами уже выполняется.", "")
	}
	defer e.importMu.Unlock()
	backup := e.p("backup", "user-removed-"+id+"-"+nowFn().Format("20060102-150405")+"-"+itoa(os.Getpid()))
	if os.MkdirAll(filepath.Join(backup, "files"), 0o700) != nil || copyFile(e.bundlesFile(), filepath.Join(backup, "bundles.json")) != nil ||
		copyFile(e.customFile(), filepath.Join(backup, "custom.json")) != nil {
		return nil, customGeneric("")
	}
	bl, _ := readObj(e.bundlesFile())
	var ids []any
	for _, x := range strList(bl["bundles"]) {
		if x != id {
			ids = append(ids, x)
		}
	}
	if ids == nil {
		ids = []any{}
	}
	bl["bundles"] = ids
	idx, _ := readObj(e.customFile())
	entries := []any{}
	for _, x := range anyList(idx["bundles"]) {
		if m, _ := x.(obj); str(m, "id") != id {
			entries = append(entries, x)
		}
	}
	idx["bundles"] = entries
	type moved struct{ key, target string }
	var movedList []moved
	for _, t := range []moved{{"catalog", e.catalogDir(id)}, {"manifest", e.manifestFile(id)}, {"state", e.stateFile(id)}, {"registry", e.registryFile(id)}} {
		if !fileExists(t.target) {
			continue
		}
		if os.Rename(t.target, filepath.Join(backup, "files", t.key)) != nil {
			for _, m := range movedList {
				_ = os.Rename(filepath.Join(backup, "files", m.key), m.target)
			}
			return nil, customGeneric("")
		}
		movedList = append(movedList, t)
	}
	if writeObj(e.bundlesFile(), bl) != nil || writeObj(e.customFile(), idx) != nil {
		_ = copyFile(filepath.Join(backup, "bundles.json"), e.bundlesFile())
		_ = copyFile(filepath.Join(backup, "custom.json"), e.customFile())
		for _, m := range movedList {
			_ = os.Rename(filepath.Join(backup, "files", m.key), m.target)
		}
		return nil, customError("REMOVE_FAILED", "Не удалось удалить пользовательский набор; локальное состояние восстановлено.", "")
	}
	_ = os.Remove(e.progressFile(id))
	_ = os.Remove(e.counterFile(id))
	_ = os.Remove(e.preflightFile(id))
	return obj{"bundleId": id, "removed": true, "backupPath": backup, "removedAt": stamp()}, nil
}

// ---------- A6 list / A7 status ----------

func hCustomList(e *engine, c *cgi.Req) (any, error) {
	if !fileExists(e.customFile()) {
		return obj{"schemaVersion": 1, "bundles": []any{}}, nil
	}
	idx, ok := readObj(e.customFile())
	_, isArr := idx["bundles"].([]any)
	if !regularFile(e.customFile()) || !ok || intOr(idx["schemaVersion"], 0) != 1 || !isArr {
		return nil, customError("INDEX_INVALID", "Реестр пользовательских маршрутов повреждён.", "")
	}
	return obj{"schemaVersion": 1, "bundles": idx["bundles"]}, nil
}

func hCustomStatus(e *engine, c *cgi.Req) (any, error) {
	id, aerr := customBundleFromQuery(c.RawQuery)
	if aerr != nil {
		return nil, aerr
	}
	idx, ok := readObj(e.customFile())
	if !ok || intOr(idx["schemaVersion"], 0) != 1 || !e.userBundleExists(id) {
		return nil, cgi.E(404, "ROUTES_BUNDLE_NOT_FOUND", "Пользовательский набор маршрутов не найден.")
	}
	s, ok := readObj(e.stateFile(id))
	if !ok || intOr(s["schemaVersion"], 0) != 1 || str(s, "bundleId") != id {
		return nil, cgi.E(500, "ROUTES_STATE_INVALID", "Состояние пользовательского набора повреждено.")
	}
	presence, ok := e.presence(c.R.Context(), id, 5*time.Second)
	if !ok {
		presence = obj{"available": false, "registered": false, "expectedRouteCount": nil, "presentRouteCount": nil, "missingRouteCount": nil,
			"actualInstalled": nil, "drift": nil, "status": "unavailable", "missingRoutes": []any{}}
	}
	s["routerPresence"] = presence
	s["operationProgress"] = e.progressOrIdle(id)
	s["globalOperation"] = e.globalOperation()
	return s, nil
}
