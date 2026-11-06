package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// Scheduled updates of the built-in route sets (3.5.0, owner decision
// 2026-11-02). At the interval the owner chooses (off by default) BROray
// reads the newest commit of RockBlack-VPN/ip-address (branch main) and the
// .bat listing of every catalog directory from the GitHub API. A bundle
// whose files differ from its current listing gets the new listing; then:
//
//   - installed bundles: the files are downloaded from raw.githubusercontent.com
//     at that commit, validated exactly like a manual check (size, git blob
//     SHA, BAT parser, bogon filter, route limits), stored as the downloaded
//     version and prepared (read-only Keenetic plan), so the card shows
//     «Готово обновление»; with «Устанавливать обновления автоматически» the
//     normal export runs (same fence, preflight, receipts and rollback);
//   - bundles the owner checked or downloaded but did not install: only the
//     available version is refreshed;
//   - bundles never touched: nothing (their first check uses the new listing).
//
// An update that would remove more than maxRemovalPercent of the installed
// routes, or that fails validation, is marked «Требует проверки» and never
// applied automatically. When the API is unreachable nothing changes: the
// pinned listing (or the last accepted one) stays in use. Automation pause
// and maintenance mode are respected; the GitHub hosts are fixed.

// GitHubAPI is the GitHub REST API base (dev builds may point it at a local fake).
var GitHubAPI = "https://api.github.com"

const (
	actionCatalogUpdate = "routes:catalog-update"
	catalogTarget       = "catalog"
	historyLimit        = 20
	catalogAPIBudget    = 4 // one head request and at most three directory listings
	catalogRetry        = 30 * time.Minute
	listingMaxBytes     = 1 << 20
	defaultRemovalPct   = 50
)

var (
	// scheduleIntervals are the owner's choices in hours (0 = off).
	scheduleIntervals = []int{0, 6, 12, 24, 168}
	// ScheduleTick is how often the loop looks for a due check (dev builds may shorten it).
	ScheduleTick = time.Minute
	// busyRetry is the delay before retrying a bundle step that met another operation.
	busyRetry = 5 * time.Minute
	reGitSHA  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	reAPIDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)

	errSchedBusy = errors.New("ROUTES_OPERATION_BUSY: " + msgBusy)
)

func (e *engine) scheduleFile() string       { return e.p("catalog-schedule.json") }
func (e *engine) scheduleStatusFile() string { return e.p("catalog-schedule-status.json") }
func (e *engine) remoteCatalogFile() string  { return e.p("catalog-remote.json") }

// ---------- settings ----------

type schedSettings struct {
	IntervalHours     int
	AutoApply         bool
	MaxRemovalPercent int
}

func validInterval(h int) bool {
	for _, x := range scheduleIntervals {
		if x == h {
			return true
		}
	}
	return false
}

func validRemovalPct(p int) bool { return p >= 10 && p <= 90 }

// scheduleSettings reads routes/catalog-schedule.json; anything invalid is
// the safe default (off, no automatic installation, 50 %).
func (e *engine) scheduleSettings() schedSettings {
	s := schedSettings{MaxRemovalPercent: defaultRemovalPct}
	m, ok := readObj(e.scheduleFile())
	if !ok || intOr(m["schemaVersion"], 0) != 1 {
		return s
	}
	if h, ok := intOf(m["intervalHours"]); ok && validInterval(h) {
		s.IntervalHours = h
	}
	s.AutoApply = m["autoApply"] == true
	if p, ok := intOf(m["maxRemovalPercent"]); ok && validRemovalPct(p) {
		s.MaxRemovalPercent = p
	}
	return s
}

func (e *engine) saveScheduleSettings(s schedSettings) error {
	return writeObj(e.scheduleFile(), obj{"schemaVersion": 1, "intervalHours": s.IntervalHours, "autoApply": s.AutoApply,
		"maxRemovalPercent": s.MaxRemovalPercent, "updatedAt": stamp()})
}

// ---------- the listing in use ----------

// sourceFor returns the bundle's current listing: the one accepted by the
// last scheduled check (catalog-remote.json) or the pinned one.
func (e *engine) sourceFor(b builtin) catalogSource {
	rc, ok := readObj(e.remoteCatalogFile())
	if !ok || intOr(rc["schemaVersion"], 0) != 1 {
		return b.pinnedSource()
	}
	if src, ok := parseRemoteEntry(b, sub(sub(rc, "bundles"), b.ID)); ok {
		return src
	}
	return b.pinnedSource()
}

func parseRemoteEntry(b builtin, m obj) (catalogSource, bool) {
	if m == nil || str(m, "directory") != b.Directory {
		return catalogSource{}, false
	}
	src := catalogSource{Commit: str(m, "commit"), Date: str(m, "date")}
	if !reGitSHA.MatchString(src.Commit) || !reAPIDate.MatchString(src.Date) {
		return catalogSource{}, false
	}
	for _, x := range anyList(m["files"]) {
		f, _ := x.(obj)
		size, ok := intOf(orNull(f, "sizeBytes"))
		ref := sourceRef{Name: str(f, "name"), Size: size, GitSHA: str(f, "gitSha")}
		if !ok || !validListedFile(b, ref) {
			return catalogSource{}, false
		}
		src.Files = append(src.Files, ref)
	}
	return src, len(src.Files) > 0
}

func validListedFile(b builtin, f sourceRef) bool {
	return safeSourceName(f.Name) && strings.HasSuffix(strings.ToLower(f.Name), ".bat") &&
		f.Size > 0 && f.Size <= b.MaxSourceBytes && reGitSHA.MatchString(f.GitSHA)
}

func remoteEntry(b builtin, src catalogSource) obj {
	files := []any{}
	for _, f := range src.Files {
		files = append(files, obj{"name": f.Name, "sizeBytes": f.Size, "gitSha": f.GitSHA})
	}
	return obj{"directory": b.Directory, "commit": src.Commit, "date": src.Date, "files": files}
}

// ---------- GitHub API ----------

func (e *engine) apiGet(ctx context.Context, path string, v any) error {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, strings.TrimRight(e.apiBase, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "BROray-routes/1.1")
	resp, err := e.http.Do(req)
	if err != nil {
		return errors.New("сервер недоступен")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, listingMaxBytes+1))
	if err != nil {
		return errors.New("ответ получен не полностью")
	}
	if len(body) > listingMaxBytes {
		return errors.New("слишком большой ответ")
	}
	if resp.StatusCode != http.StatusOK {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &m)
		if m.Message != "" && len(m.Message) <= 200 {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, m.Message)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return errors.New("неожиданный ответ")
	}
	return nil
}

// fetchHead reads the newest commit of the catalog branch.
func (e *engine) fetchHead(ctx context.Context) (sha, date string, err error) {
	var br struct {
		Commit struct {
			SHA    string `json:"sha"`
			Commit struct {
				Committer struct {
					Date string `json:"date"`
				} `json:"committer"`
				Author struct {
					Date string `json:"date"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"commit"`
	}
	if err := e.apiGet(ctx, "/repos/"+catalogRepository+"/branches/"+catalogBranch, &br); err != nil {
		return "", "", err
	}
	sha, date = br.Commit.SHA, br.Commit.Commit.Committer.Date
	if date == "" {
		date = br.Commit.Commit.Author.Date
	}
	if !reGitSHA.MatchString(sha) || !reAPIDate.MatchString(date) {
		return "", "", errors.New("некорректные сведения о версии")
	}
	return sha, date, nil
}

// fetchListing is the 3.x all-bat discovery of one directory at a commit.
func (e *engine) fetchListing(ctx context.Context, b builtin, sha, date string) (catalogSource, error) {
	var items []struct {
		Type string `json:"type"`
		Name string `json:"name"`
		Size int    `json:"size"`
		SHA  string `json:"sha"`
	}
	if err := e.apiGet(ctx, "/repos/"+catalogRepository+"/contents/"+escapePath(b.Directory)+"?ref="+sha, &items); err != nil {
		return catalogSource{}, fmt.Errorf("Не удалось прочитать список файлов раздела %s на GitHub: %v.", b.Name, err)
	}
	src := catalogSource{Commit: sha, Date: date}
	for _, it := range items {
		if it.Type != "file" || !strings.HasSuffix(strings.ToLower(it.Name), ".bat") {
			continue
		}
		ref := sourceRef{Name: it.Name, Size: it.Size, GitSHA: it.SHA}
		if !validListedFile(b, ref) {
			return catalogSource{}, fmt.Errorf("GitHub вернул некорректный файл источника в разделе %s.", b.Name)
		}
		src.Files = append(src.Files, ref)
	}
	if len(src.Files) == 0 {
		return catalogSource{}, fmt.Errorf("В разделе %s на GitHub не найдено ни одного .bat-файла.", b.Name)
	}
	sort.SliceStable(src.Files, func(i, j int) bool { return strings.ToLower(src.Files[i].Name) < strings.ToLower(src.Files[j].Name) })
	return src, nil
}

// refreshListings reads the newest commit and, when it is new, the listing
// of every catalog directory; accepted listings go to catalog-remote.json.
// It returns the head commit and the per-bundle listing errors. An error
// means the API was not usable: nothing was changed.
func (e *engine) refreshListings(ctx context.Context) (head, date string, listErr map[string]string, err error) {
	head, date, err = e.fetchHead(ctx)
	if err != nil {
		return "", "", nil, err
	}
	rc, ok := readObj(e.remoteCatalogFile())
	if !ok || intOr(rc["schemaVersion"], 0) != 1 {
		rc = obj{"schemaVersion": 1, "repository": catalogRepository, "branch": catalogBranch, "bundles": obj{}}
	}
	listErr = map[string]string{}
	if str(rc, "headCommit") == head && rc["complete"] == true {
		return head, date, listErr, nil
	}
	bundles := sub(rc, "bundles")
	if bundles == nil {
		bundles = obj{}
	}
	listed, failed := obj{}, obj{}
	if str(rc, "headCommit") == head {
		if old := sub(rc, "listedBundles"); old != nil {
			listed = old
		}
		if old := sub(rc, "failedListings"); old != nil {
			failed = old
		}
	}
	// The embedded manifest already covers every directory at this commit.
	if head == catalogCommit {
		bundles = obj{}
		for _, b := range builtins {
			listed[b.ID] = true
		}
		failed = obj{}
	} else {
		// Retry a failed directory promptly, then use the remaining API budget
		// for unchecked directories. A persistent failure must not prevent
		// independent bundles from receiving their own updates.
		priority := map[string]bool{}
		for _, b := range builtins {
			st, ok := readObj(e.stateFile(b.ID))
			priority[b.ID] = ok && (st["installedVersion"] != nil || st["downloadedVersion"] != nil || st["availableVersion"] != nil)
		}
		candidates := append([]builtin(nil), builtins...)
		sort.SliceStable(candidates, func(i, j int) bool {
			return priority[candidates[i].ID] && !priority[candidates[j].ID]
		})
		var retry, fresh []builtin
		for _, b := range candidates {
			switch {
			case failed[b.ID] == true:
				retry = append(retry, b)
			case listed[b.ID] != true:
				fresh = append(fresh, b)
			}
		}
		selected := make([]builtin, 0, catalogAPIBudget-1)
		cursor, retried := intOr(rc["failedCursor"], 0), 0
		if cursor < 0 {
			cursor = 0
		}
		if len(retry) > 0 {
			cursor %= len(retry)
			selected = append(selected, retry[cursor])
			retried++
		}
		for _, b := range fresh {
			if len(selected) >= catalogAPIBudget-1 {
				break
			}
			selected = append(selected, b)
		}
		for len(selected) < catalogAPIBudget-1 && retried < len(retry) {
			selected = append(selected, retry[(cursor+retried)%len(retry)])
			retried++
		}
		if len(retry) > 0 {
			rc["failedCursor"] = (cursor + retried) % len(retry)
		} else {
			rc["failedCursor"] = 0
		}
		for _, b := range selected {
			src, lerr := e.fetchListing(ctx, b, head, date)
			if lerr != nil {
				if ctx.Err() != nil {
					return "", "", nil, ctx.Err()
				}
				listErr[b.ID] = lerr.Error()
				failed[b.ID] = true
				continue // keep the bundle's previous accepted listing
			}
			listed[b.ID] = true
			delete(failed, b.ID)
			cur := e.sourceFor(b)
			switch {
			case src.sameFiles(b.pinnedSource()):
				delete(bundles, b.ID)
			case src.sameFiles(cur):
				// The accepted file list is unchanged; keep its commit.
			default:
				bundles[b.ID] = remoteEntry(b, src)
			}
		}
	}
	complete := len(failed) == 0
	for _, b := range builtins {
		if listed[b.ID] != true {
			complete = false
		}
	}
	rc["bundles"], rc["listedBundles"], rc["failedListings"] = bundles, listed, failed
	rc["headCommit"], rc["headDate"], rc["listedAt"] = head, date, stamp()
	rc["complete"] = complete
	if werr := writeObj(e.remoteCatalogFile(), rc); werr != nil {
		return "", "", nil, errors.New("не удалось сохранить список файлов")
	}
	return head, date, listErr, nil
}

// ---------- status ----------

func (e *engine) readScheduleStatus() obj {
	m, ok := readObj(e.scheduleStatusFile())
	if !ok || intOr(m["schemaVersion"], 0) != 1 {
		return obj{"schemaVersion": 1, "history": []any{}}
	}
	return m
}

func (e *engine) writeScheduleStatus(mut func(obj)) error {
	e.schedMu.Lock()
	defer e.schedMu.Unlock()
	m := e.readScheduleStatus()
	mut(m)
	m["updatedAt"] = stamp()
	if err := writeObj(e.scheduleStatusFile(), m); err != nil {
		e.schedRetryAt = nowFn().Add(catalogRetry)
		e.log.Printf("routes: schedule status: %v", err)
		return err
	}
	e.schedRetryAt = time.Time{}
	return nil
}

// note records one automatic change in the schedule history and the daemon log.
func (e *engine) note(id, event, message string) {
	e.log.Printf("routes: catalog update %s: %s: %s", id, event, message)
	e.writeScheduleStatus(func(m obj) {
		h := append(anyList(m["history"]), obj{"at": stamp(), "bundleId": id, "bundleName": e.bundleName(id), "event": event, "message": message})
		if len(h) > historyLimit {
			h = h[len(h)-historyLimit:]
		}
		m["history"] = h
	})
}

// scheduleView is the GET /api/routes/catalog-schedule.cgi object.
func (e *engine) scheduleView() obj {
	s := e.scheduleSettings()
	st := e.readScheduleStatus()
	paused := e.queue != nil && e.queue.Paused()
	queued := false
	if e.queue != nil {
		req, live := e.queue.FindLive(actionCatalogUpdate, catalogTarget)
		queued = live && (req.State == "running" || req.Source == "USER")
	}
	choices := make([]any, len(scheduleIntervals))
	for i, h := range scheduleIntervals {
		choices[i] = h
	}
	var next any
	if s.IntervalHours > 0 && !paused {
		next = stamp()
		if at := e.nextDue(s, st); !at.IsZero() && at.After(nowFn()) {
			next = compat.Stamp(at)
		}
	}
	rc, _ := readObj(e.remoteCatalogFile())
	using := "pinned"
	if len(sub(rc, "bundles")) > 0 {
		using = "github"
	}
	history := anyList(st["history"])
	if history == nil {
		history = []any{}
	}
	return obj{"schemaVersion": 1, "intervalHours": s.IntervalHours, "intervalChoices": choices, "autoApply": s.AutoApply,
		"maxRemovalPercent": s.MaxRemovalPercent, "automationPaused": paused, "running": queued,
		"lastCheckAt": orNull(st, "lastCheckAt"), "lastResult": orNull(st, "lastResult"), "lastMessage": orNull(st, "lastMessage"),
		"headCommit": orNull(st, "headCommit"), "headDate": orNull(st, "headDate"), "nextCheckAt": next,
		"source":  obj{"repository": catalogRepository, "branch": catalogBranch, "pinnedCommit": catalogCommit, "pinnedDate": catalogCommitDate, "listing": using},
		"bundles": orNull(st, "bundles"), "history": history}
}

// nextDue is the time of the next scheduled check (zero: now).
func (e *engine) nextDue(s schedSettings, st obj) time.Time {
	var due time.Time
	if last, ok := intOf(st["lastAttemptEpoch"]); ok && last > 0 {
		due = time.Unix(int64(last), 0).Add(time.Duration(s.IntervalHours) * time.Hour)
	}
	if retry, ok := intOf(st["retryEpoch"]); ok && retry > 0 {
		if r := time.Unix(int64(retry), 0); due.IsZero() || r.Before(due) {
			due = r
		}
	}
	e.schedMu.Lock()
	retryAt := e.schedRetryAt
	e.schedMu.Unlock()
	if retryAt.After(due) {
		due = retryAt
	}
	return due
}

// ---------- loop and queue ----------

func (e *engine) defineScheduleQueue() {
	if e.queue == nil {
		return
	}
	e.queue.Define(compat.ActionSpec{Name: actionCatalogUpdate, Type: "route_operation", Priority: map[string]int{"USER": 3, "*": 7},
		Stages: []compat.Stage{{Name: "fetch", Phase: "fetching", Protected: true, Run: e.catalogUpdateTask}}})
}

func (e *engine) scheduleLoop(ctx context.Context) {
	t := time.NewTicker(ScheduleTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.enqueueDueCheck()
		}
	}
}

// enqueueDueCheck queues a SCHEDULER check when the interval (or a busy
// retry) has passed; nothing while automation is paused or the schedule is off.
func (e *engine) enqueueDueCheck() bool {
	if e.queue == nil || e.queue.Paused() {
		return false
	}
	s := e.scheduleSettings()
	if s.IntervalHours == 0 {
		return false
	}
	if due := e.nextDue(s, e.readScheduleStatus()); !due.IsZero() && nowFn().Before(due) {
		return false
	}
	if _, err := e.queue.Enqueue(actionCatalogUpdate, catalogTarget, "SCHEDULER", "scheduled"); err != nil {
		e.log.Printf("routes: catalog update enqueue: %v", err)
		return false
	}
	return true
}

func (e *engine) catalogUpdateTask(t *compat.Task) error {
	res, err := e.runCatalogUpdate(t.Ctx, t.Source, t.Op)
	msg := str(res, "message")
	if err != nil {
		t.Result(msg, "ROUTES_CATALOG_CHECK_FAILED")
		return err
	}
	t.Result(msg, "")
	return nil
}

// ---------- the update run ----------

type bundleOutcome struct {
	ID, Result, Message string
	Busy                bool
}

// runCatalogUpdate is one scheduled (or «Проверить сейчас») check. source
// is the queue source; automatic installation never runs while automation
// is paused.
func (e *engine) runCatalogUpdate(ctx context.Context, source string, op *compat.Operation) (obj, error) {
	settings := e.scheduleSettings()
	now := nowFn()
	if err := e.writeScheduleStatus(func(m obj) {
		m["lastAttemptEpoch"] = now.Unix()
		m["retryEpoch"] = nil
	}); err != nil {
		msg := "Не удалось сохранить состояние проверки каталога. Проверка не запускалась."
		return obj{"result": "failed", "message": msg}, fmt.Errorf("ROUTES_CATALOG_STATUS_PERSIST_FAILED: %w", err)
	}
	head, date, listErr, err := e.refreshListings(ctx)
	if err != nil {
		msg := "Не удалось получить сведения о версии с GitHub (" + err.Error() + "). Используется прежняя версия источника."
		statusErr := e.writeScheduleStatus(func(m obj) {
			m["lastCheckAt"], m["lastResult"], m["lastMessage"] = stamp(), "failed", msg
			m["retryEpoch"] = now.Add(catalogRetry).Unix()
		})
		e.log.Printf("routes: catalog update: %s", msg)
		fetchErr := errors.New("ROUTES_CATALOG_CHECK_FAILED: " + msg)
		if statusErr != nil {
			return obj{"result": "failed", "message": msg}, errors.Join(fetchErr, fmt.Errorf("ROUTES_CATALOG_STATUS_PERSIST_FAILED: %w", statusErr))
		}
		return obj{"result": "failed", "message": msg}, fetchErr
	}
	if op != nil {
		op.Phase("committing")
	}
	paused := e.queue != nil && e.queue.Paused()
	var outcomes []bundleOutcome
	for _, b := range builtins {
		if ctx.Err() != nil {
			break
		}
		if m, ok := listErr[b.ID]; ok {
			outcomes = append(outcomes, bundleOutcome{ID: b.ID, Result: "listing_failed", Message: m})
			continue
		}
		outcomes = append(outcomes, e.updateBundle(ctx, b, settings, paused))
	}
	counts := map[string]int{}
	busy := false
	bundles := obj{}
	for _, o := range outcomes {
		counts[o.Result]++
		busy = busy || o.Busy
		bundles[o.ID] = obj{"result": o.Result, "message": nullStr(o.Message)}
	}
	var parts []string
	add := func(n int, text string) {
		if n > 0 {
			parts = append(parts, text+": "+itoa(n))
		}
	}
	add(counts["applied"], "Установлено в Keenetic автоматически")
	add(counts["ready"], "Готово обновление")
	add(counts["available"], "Доступна новая версия")
	add(counts["review_required"], "Требует проверки")
	add(counts["failed"]+counts["listing_failed"]+counts["apply_failed"], "Ошибка")
	add(counts["busy"], "Отложено: выполнялась другая операция")
	result := "no_changes"
	msg := "Новых версий готовых маршрутов нет."
	if len(parts) > 0 {
		result, msg = "updates_found", strings.Join(parts, ". ")+"."
		if counts["applied"]+counts["ready"]+counts["available"]+counts["review_required"] == 0 {
			result = "failed"
		}
	}
	remote, _ := readObj(e.remoteCatalogFile())
	incomplete := str(remote, "headCommit") == head && remote["complete"] != true
	if incomplete {
		if result == "no_changes" {
			msg = "Проверка разделов каталога продолжается. Следующая попытка запланирована через 30 минут."
		} else {
			msg += " Проверка остальных разделов продолжится через 30 минут."
		}
	}
	if paused && settings.AutoApply && counts["ready"] > 0 {
		msg += " Автоматическая установка приостановлена: фоновые операции остановлены."
	}
	if err := e.writeScheduleStatus(func(m obj) {
		m["lastCheckAt"], m["lastResult"], m["lastMessage"] = stamp(), result, msg
		m["headCommit"], m["headDate"], m["bundles"] = head, date, bundles
		m["lastSource"] = source
		if busy {
			m["retryEpoch"] = nowFn().Add(busyRetry).Unix()
		} else if incomplete {
			m["retryEpoch"] = nowFn().Add(catalogRetry).Unix()
		}
	}); err != nil {
		msg := "Проверка каталога выполнена, но результат не удалось сохранить. Проверьте состояние маршрутов перед повтором."
		return obj{"result": "failed", "message": msg, "headCommit": head}, fmt.Errorf("ROUTES_CATALOG_STATUS_PERSIST_FAILED: %w", err)
	}
	return obj{"result": result, "message": msg, "headCommit": head}, nil
}

// updateBundle brings one built-in bundle up to its current listing.
func (e *engine) updateBundle(ctx context.Context, b builtin, settings schedSettings, paused bool) bundleOutcome {
	out := bundleOutcome{ID: b.ID, Result: "unchanged"}
	state, ok := readObj(e.stateFile(b.ID))
	if !ok {
		return out
	}
	installed := state["installedVersion"] != nil
	if state["availableVersion"] == nil && state["downloadedVersion"] == nil {
		return out // never checked: the first manual check uses the current listing
	}
	if e.rollbackBlocked() {
		out.Result, out.Message = "failed", msgRollbackBlock
		return out
	}
	src := e.sourceFor(b)
	su := sub(state, "scheduledUpdate")
	if str(su, "status") == "review_required" && str(su, "sourceCommit") == src.Commit && scheduleReviewRequired(state) {
		out.Result, out.Message = "review_required", str(su, "message")
		return out // waits for the owner
	}
	busy := func() bundleOutcome {
		out.Result, out.Busy, out.Message = "busy", true, msgBusy
		return out
	}
	// 1. check: the listing is newer than the available version
	if str(sub(state, "availableVersion"), "sourceCommit") != src.Commit {
		var ev *checkEval
		var evErr error
		guard, rejected := "", ""
		var ce *cliError
		err := e.withFence("check", b.ID, func() error {
			st, _ := readObj(e.stateFile(b.ID))
			if ev, evErr = e.evaluateCheck(ctx, b.ID, b, st, src); evErr != nil {
				// not recorded as the card's error: a network failure is
				// retried at the next check; rejected files (parser, limits,
				// empty list) leave the installed and downloaded versions as
				// they are and wait for the owner
				if !errors.As(evErr, &ce) || (!ce.network && !ce.noState) {
					rejected = "Новая версия источника " + shortCommit(src.Commit) + " не прошла проверку и не применяется. " + evErr.Error()
					e.markScheduled(b.ID, "review_required", "invalid_source", src.Commit, rejected, nil)
				}
				return nil
			}
			if _, err := e.commitCheck(b.ID, st, ev); err != nil {
				return err
			}
			if !installed {
				return nil
			}
			if removed, before := e.installedRemoval(b.ID, ev.normalized); removalTooLarge(before, removed, settings.MaxRemovalPercent) {
				guard = fmt.Sprintf("Новая версия источника %s удалит %d из %d установленных маршрутов (больше %d%%). Автоматическое обновление остановлено: проверьте изменения и обновите набор вручную.",
					shortCommit(src.Commit), removed, before, settings.MaxRemovalPercent)
				e.markScheduled(b.ID, "review_required", "mass_removal", src.Commit, guard, ev)
			}
			return nil
		})
		if errors.Is(err, errSchedBusy) {
			return busy()
		}
		switch {
		case err != nil:
			out.Result, out.Message = "failed", err.Error()
			return out
		case rejected != "":
			e.note(b.ID, "review_required", rejected)
			out.Result, out.Message = "review_required", rejected
			return out
		case evErr != nil:
			out.Result, out.Message = "failed", evErr.Error() // retried at the next check
			return out
		case guard != "":
			e.note(b.ID, "review_required", guard)
			out.Result, out.Message = "review_required", guard
			return out
		}
		if !installed {
			if ev.sourceChanged || !ev.downloadedPresent {
				out.Result, out.Message = "available", ev.message
			}
			return out
		}
	} else if !installed {
		return out
	}
	// 2. download the checked version (installed bundles only)
	state, _ = readObj(e.stateFile(b.ID))
	av, dv, iv := sub(state, "availableVersion"), sub(state, "downloadedVersion"), sub(state, "installedVersion")
	if !versionEqual(av, dv) || str(av, "sourceSetSha256") != str(dv, "sourceSetSha256") {
		var dlErr error
		err := e.withFence("download", b.ID, func() error {
			_, dlErr = e.runDownload(ctx, b.ID)
			return nil
		})
		if errors.Is(err, errSchedBusy) {
			return busy()
		}
		if dlErr != nil {
			out.Result, out.Message = "failed", dlErr.Error()
			return out
		}
		state, _ = readObj(e.stateFile(b.ID))
		dv = sub(state, "downloadedVersion")
		if str(dv, "contentSha256") != str(iv, "contentSha256") {
			e.note(b.ID, "downloaded", "Скачана новая версия источника "+shortCommit(str(dv, "sourceCommit"))+". "+routeChangeLine(state))
		}
	}
	if str(dv, "contentSha256") == str(iv, "contentSha256") {
		return out // the files changed, the route list did not
	}
	// guard against the installed routes (also for a manually checked version)
	if removed, before := e.installedRemoval(b.ID, e.downloadedRoutes(b.ID)); removalTooLarge(before, removed, settings.MaxRemovalPercent) {
		msg := fmt.Sprintf("Загруженная версия удалит %d из %d установленных маршрутов (больше %d%%). Автоматическая установка не выполняется: проверьте изменения и нажмите «Обновить в Keenetic».",
			removed, before, settings.MaxRemovalPercent)
		if str(su, "status") != "review_required" || str(su, "contentSha256") != str(dv, "contentSha256") {
			if errors.Is(e.markFenced(b.ID, "review_required", "mass_removal", str(dv, "sourceCommit"), str(dv, "contentSha256"), msg), errSchedBusy) {
				return busy()
			}
			e.note(b.ID, "review_required", msg)
		}
		out.Result, out.Message = "review_required", msg
		return out
	}
	// 3. prepare (read-only Keenetic plan) so the card shows «Готово обновление»
	if !e.verifiedFor(b.ID) {
		var vErr error
		err := e.withFence("verify", b.ID, func() error {
			_, vErr = e.verify(b.ID)
			return nil
		})
		if errors.Is(err, errSchedBusy) {
			return busy()
		}
		if vErr != nil {
			out.Result, out.Message = "failed", errText(vErr)
			return out
		}
	}
	state, _ = readObj(e.stateFile(b.ID))
	ready := "Готово обновление: " + routeChangeLine(state)
	cur := sub(state, "scheduledUpdate")
	if str(cur, "status") == "apply_failed" && str(cur, "contentSha256") == str(dv, "contentSha256") {
		// a failed (rolled back) automatic installation is not repeated: the owner decides
		out.Result, out.Message = "apply_failed", str(cur, "message")
		return out
	}
	if str(cur, "contentSha256") != str(dv, "contentSha256") {
		if errors.Is(e.markFenced(b.ID, "ready", "", str(dv, "sourceCommit"), str(dv, "contentSha256"), ready), errSchedBusy) {
			return busy()
		}
		e.note(b.ID, "ready", ready)
	}
	out.Result, out.Message = "ready", ready
	if !settings.AutoApply || paused {
		return out
	}
	// 4. automatic installation through the normal export
	vr := sub(state, "verifyResult")
	if str(sub(vr, "keenetic"), "status") != "update_pending" || sub(vr, "keenetic")["canApply"] != true {
		return out // conflicts or Keenetic unavailable: left to the owner
	}
	var exErr error
	pausedBeforeExport := false
	msg := "Обновление установлено в Keenetic автоматически (версия " + shortCommit(str(dv, "sourceCommit")) + "). " + routeChangeLine(state)
	err := e.withFence("export", b.ID, func() error {
		if e.queue != nil && e.queue.Paused() {
			pausedBeforeExport = true
			return nil
		}
		exErr = e.autoExport(b.ID)
		// the verdict is written before the fence is released
		if exErr != nil {
			msg = "Автоматическая установка обновления не выполнена: " + errText(exErr)
			e.markScheduledFor(b.ID, "apply_failed", "", str(dv, "sourceCommit"), str(dv, "contentSha256"), msg, nil)
		} else {
			e.markScheduledFor(b.ID, "applied", "", str(dv, "sourceCommit"), str(dv, "contentSha256"), msg, nil)
		}
		return nil
	})
	if errors.Is(err, errSchedBusy) {
		return busy()
	}
	if pausedBeforeExport {
		return out
	}
	if exErr != nil {
		e.note(b.ID, "apply_failed", msg)
		out.Result, out.Message = "apply_failed", msg
		return out
	}
	e.note(b.ID, "applied", msg)
	out.Result, out.Message = "applied", msg
	return out
}

// autoExport is preflight + export as one SCHEDULER operation (the caller
// holds the fence). A resumable stop keeps the fence pending, like export.cgi.
func (e *engine) autoExport(id string) error {
	var op *compat.Operation
	if e.queue != nil {
		op = e.queue.StartOperation("", "route_operation", "export", "SCHEDULER", "protected", "")
		op.Phase("committing")
	}
	finish := func(err error) {
		if op != nil {
			op.Finish(errOf(err))
		}
	}
	pf, err := e.preflight(id, "export")
	if err != nil {
		finish(err)
		return err
	}
	p, _ := pf.(obj)
	if p["ready"] != true {
		err := errors.New("ROUTES_PREFLIGHT_NOT_READY: " + str(p, "message"))
		finish(err)
		return err
	}
	opID := ""
	if op != nil {
		opID = op.ID
	}
	_, err = e.mutation(id, "export", str(p, "token"), opID)
	if e.progressResumable(id) {
		if err == nil {
			err = errors.New("ROUTES_EXPORT_PAUSED: установка остановлена после ошибки и ждёт продолжения (кнопка «Продолжить» в карточке набора)")
		}
		err = errors.Join(err, e.lock.MarkPending(true))
	}
	finish(err)
	return err
}

// withFence runs one bundle step under the global fence, like a user
// request; another operation (or a resumable one) means "try later".
func (e *engine) withFence(action, id string, fn func() error) error {
	if e.operationBlocking(action, id) {
		return errSchedBusy
	}
	rel, ok := e.lock.TryAcquire("routes", action, id)
	if !ok {
		return errSchedBusy
	}
	defer rel()
	return fn()
}

// errText drops a leading ERROR_CODE: prefix.
func errText(err error) string {
	s := err.Error()
	if i := strings.Index(s, ": "); i > 0 && strings.ToUpper(s[:i]) == s[:i] && !strings.Contains(s[:i], " ") {
		s = s[i+2:]
	}
	return s
}

func shortCommit(c string) string {
	if len(c) >= 7 {
		return c[:7]
	}
	return c
}

func routeChangeLine(state obj) string {
	rc := sub(sub(state, "checkResult"), "routeChanges")
	if rc == nil {
		return ""
	}
	return fmt.Sprintf("Маршруты: +%d / −%d (было %d, станет %d).", intOr(rc["added"], 0), intOr(rc["removed"], 0),
		intOr(rc["before"], 0), intOr(rc["after"], 0))
}

func removalTooLarge(before, removed, pct int) bool {
	return before > 0 && removed*100 > before*pct
}

// installedRemoval counts the installed routes of the bundle (its registry
// keys) that the list next would remove. next nil → the downloaded list.
func (e *engine) installedRemoval(id string, next []string) (removed, before int) {
	reg, ok := readObj(e.registryFile(id))
	if !ok || reg["installedVersion"] == nil {
		return 0, 0
	}
	if next == nil {
		next = e.downloadedRoutes(id)
	}
	keep := map[string]bool{}
	for _, r := range next {
		keep[r] = true
	}
	for _, k := range strList(reg["routeKeys"]) {
		p := strings.Split(k, "|")
		if len(p) < 2 {
			continue
		}
		before++
		if !keep[p[1]] {
			removed++
		}
	}
	return removed, before
}

func (e *engine) downloadedRoutes(id string) []string {
	l, _ := readLines(e.catalogDir(id) + "/normalized.txt")
	if l == nil {
		l = []string{}
	}
	return l
}

// verifiedFor reports a current successful verify of the downloaded version.
func (e *engine) verifiedFor(id string) bool {
	s, ok := readObj(e.stateFile(id))
	if !ok {
		return false
	}
	vr := sub(s, "verifyResult")
	return vr != nil && vr["success"] == true && str(vr, "contentSha256") == str(sub(s, "downloadedVersion"), "contentSha256") &&
		sub(vr, "keenetic")["available"] == true
}

func (e *engine) markScheduled(id, status, reason, commit, message string, ev *checkEval) {
	sha := ""
	if ev != nil {
		sha = ev.contentSHA
	}
	e.markScheduledFor(id, status, reason, commit, sha, message, ev)
}

// markFenced is markScheduledFor as its own fenced step (errSchedBusy when
// another operation holds the fence).
func (e *engine) markFenced(id, status, reason, commit, contentSHA, message string) error {
	return e.withFence("check", id, func() error {
		e.markScheduledFor(id, status, reason, commit, contentSHA, message, nil)
		return nil
	})
}

// markScheduledFor records the scheduler's verdict on a bundle in its state
// (state.scheduledUpdate, additive to the 3.x state shape). It is a
// read-modify-write of the state a user check/download/export rewrites, so
// the caller holds the global fence (withFence / markFenced).
func (e *engine) markScheduledFor(id, status, reason, commit, contentSHA, message string, ev *checkEval) {
	s, ok := readObj(e.stateFile(id))
	if !ok {
		return
	}
	su := obj{"status": status, "reason": nullStr(reason), "sourceCommit": commit, "contentSha256": nullStr(contentSHA),
		"message": message, "at": stamp()}
	if ev != nil {
		su["routeChanges"] = obj{"before": ev.baselineCount, "after": len(ev.normalized), "added": ev.routeAdded, "removed": ev.routeRemoved}
	} else if rc := sub(sub(s, "checkResult"), "routeChanges"); rc != nil {
		su["routeChanges"] = rc
	}
	s["scheduledUpdate"] = su
	if err := writeObj(e.stateFile(id), s); err != nil {
		e.log.Printf("routes: state %s: %v", id, err)
	}
}
