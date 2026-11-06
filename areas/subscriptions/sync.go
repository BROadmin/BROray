package subscriptions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

// Subscription server catalog (lib/server-subscription-service.sh).

// srvFile is one server file of the live dir or of a disabled dir.
type srvFile struct {
	id, path string
	rec      servers.Record
	raw      []byte
}

func sourceOf(rec servers.Record) map[string]any {
	m, _ := rec["source"].(map[string]any)
	return m
}

// belongsTo is broray_server_subscription_file_matches.
func belongsTo(rec servers.Record, subID string) bool {
	src := sourceOf(rec)
	return src != nil && src["type"] == "subscription" && src["subscriptionId"] == subID
}

func isSubscriptionServer(rec servers.Record) bool {
	src := sourceOf(rec)
	return src != nil && src["type"] == "subscription"
}

// readDirServers reads <dir>/*.json in glob order (regular files only).
func readDirServers(dir string) []srvFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []srvFile
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".json") {
			continue
		}
		p := filepath.Join(dir, n)
		if !regular(p) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		m, ok := decodeObject(b)
		if !ok {
			continue
		}
		out = append(out, srvFile{id: strings.TrimSuffix(n, ".json"), path: p, rec: m, raw: b})
	}
	return out
}

func (s *svc) liveServers() []srvFile { return readDirServers(servers.ServersDir(s.d)) }

func (s *svc) disabledServers(subID string) []srvFile { return readDirServers(s.disabledDir(subID)) }

func filterBelongs(files []srvFile, subID string) []srvFile {
	var out []srvFile
	for _, f := range files {
		if belongsTo(f.rec, subID) {
			out = append(out, f)
		}
	}
	return out
}

// serverCount is broray_server_subscription_count (live + disabled dir).
func (s *svc) serverCount(live []srvFile, subID string) int {
	return len(filterBelongs(live, subID)) + len(filterBelongs(s.disabledServers(subID), subID))
}

// serverCountAll is broray_server_subscription_count_all.
func (s *svc) serverCountAll() int {
	n := 0
	for _, f := range s.liveServers() {
		if isSubscriptionServer(f.rec) {
			n++
		}
	}
	dirs, _ := os.ReadDir(s.disabledRoot())
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		for _, f := range readDirServers(filepath.Join(s.disabledRoot(), d.Name())) {
			if isSubscriptionServer(f.rec) {
				n++
			}
		}
	}
	return n
}

// serverList is broray_server_subscription_list.
func (s *svc) serverList(subID string) map[string]any {
	active := servers.ActiveID(s.d)
	items := []any{}
	add := func(files []srvFile, enabled bool) {
		for _, f := range filterBelongs(files, subID) {
			m := servers.MaskRecord(f.rec)
			network, security := alt(m["network"], "unknown"), alt(m["security"], "none")
			items = append(items, map[string]any{"id": m["id"], "name": alt(m["name"], m["id"]), "protocol": m["protocol"],
				"address": m["address"], "port": m["port"], "network": network, "security": security,
				"source": m["source"], "enabled": enabled, "active": m["id"] == active})
		}
	}
	add(s.liveServers(), true)
	add(s.disabledServers(subID), false)
	return map[string]any{"items": items, "total": len(items)}
}

// syncMarkers is broray_server_subscription_acquire_lock (the global fence is held by the caller).
func (s *svc) syncMarkers() *subErr {
	if exists(s.d.RunPath("server-subscription.lock")) {
		return fail("SERVER_SYNC_BUSY", "Прежняя синхронизация серверов требует восстановления.")
	}
	if m, _ := filepath.Glob(filepath.Join(s.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required")); len(m) > 0 {
		return fail("SERVER_SYNC_RECOVERY_REQUIRED", "Прежнее восстановление серверов не завершено. Резервная копия сохранена.")
	}
	return nil
}

// activeBelongsTo is broray_server_subscription_active_belongs_to.
func (s *svc) activeBelongsTo(subID string) bool {
	id := servers.ActiveID(s.d)
	if id == "" || !servers.ValidID(id) {
		return false
	}
	for _, f := range readDirServers(servers.ServersDir(s.d)) {
		if f.id == id {
			return belongsTo(f.rec, subID)
		}
	}
	return false
}

// setEnabled is broray_server_subscription_set_enabled: move every server
// of the subscription between servers/ and the disabled dir.
func (s *svc) setEnabled(subID string, enabled bool) error {
	if err := s.syncMarkers(); err != nil {
		return err
	}
	dis := s.disabledDir(subID)
	for _, d := range []string{servers.ServersDir(s.d), dis, s.d.RunPath("server-quality")} {
		_ = os.MkdirAll(d, 0o700)
	}
	if !enabled {
		if s.activeBelongsTo(subID) {
			return fail("ACTIVE_SERVER_CONFLICT", "Нельзя выключить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер.")
		}
		for _, f := range filterBelongs(s.liveServers(), subID) {
			if err := os.Rename(f.path, filepath.Join(dis, filepath.Base(f.path))); err != nil {
				return fail("SERVER_SOURCE_STATE_FAILED", "Не удалось выключить серверы подписки.")
			}
		}
		return nil
	}
	for _, f := range s.disabledServers(subID) {
		target := filepath.Join(servers.ServersDir(s.d), filepath.Base(f.path))
		if exists(target) {
			return fail("SERVER_ID_CONFLICT", "Не удалось включить подписку: идентификатор сервера уже занят.")
		}
		if err := os.Rename(f.path, target); err != nil {
			return fail("SERVER_SOURCE_STATE_FAILED", "Не удалось включить серверы подписки.")
		}
	}
	_ = os.Remove(dis)
	return nil
}

// removeServers is broray_server_subscription_remove.
func (s *svc) removeServers(subID string) (int, error) {
	if err := s.syncMarkers(); err != nil {
		return 0, err
	}
	if s.activeBelongsTo(subID) {
		return 0, fail("ACTIVE_SERVER_CONFLICT", "Нельзя удалить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер.")
	}
	count := 0
	for _, f := range filterBelongs(s.liveServers(), subID) {
		if err := s.removeFile(f); err != nil {
			return count, fail("SERVER_SOURCE_REMOVE_FAILED", "Не удалось удалить серверы подписки.")
		}
		count++
	}
	for _, f := range s.disabledServers(subID) {
		if err := s.removeFile(f); err != nil {
			return count, fail("SERVER_SOURCE_REMOVE_FAILED", "Не удалось удалить выключенные серверы подписки.")
		}
		count++
	}
	_ = os.Remove(s.disabledDir(subID))
	return count, nil
}

// removeFile is `rm -f <file> run/server-quality/<id>.json`.
func (s *svc) removeFile(f srvFile) error {
	if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if id, ok := f.rec["id"].(string); ok && servers.ValidID(id) {
		if err := os.Remove(servers.QualityPath(s.d, id)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ---------- catalog sync (broray_server_subscription_sync) ----------

type syncResult struct {
	Accepted, Added, Updated, Unchanged, Removed, Retained, Total int
	Policy, Impact                                                string
	Warnings                                                      []string
}

// test hooks
var (
	testActiveConfig = func(ctx context.Context, d *compat.Deps, rec servers.Record) error {
		cfg, err := servers.GenerateConfig(ctx, d, rec)
		if err != nil {
			return err
		}
		return servers.TestConfig(ctx, d, cfg)
	}
	interfaceTimeout = 60 * time.Second
)

func deepCopy(rec servers.Record) servers.Record {
	b, err := compat.Marshal(rec)
	if err != nil {
		return rec
	}
	m, ok := decodeObject(b)
	if !ok {
		return rec
	}
	return m
}

func canonicalHash(v any) string {
	h, _ := compat.CanonicalSHA256(v)
	return h
}

// nodeHash is sha256 of `jq -cS 'del(.source.nodeIndex,.source.updatedAt)'`.
func nodeHash(rec servers.Record) string {
	c := deepCopy(rec)
	if src, ok := c["source"].(map[string]any); ok {
		delete(src, "nodeIndex")
		delete(src, "updatedAt")
	}
	return canonicalHash(c)
}

func sameIgnoringUpdatedAt(a, b servers.Record) bool {
	strip := func(r servers.Record) string {
		c := deepCopy(r)
		if src, ok := c["source"].(map[string]any); ok {
			delete(src, "updatedAt")
		}
		return canonicalHash(c)
	}
	return strip(a) == strip(b)
}

// configHash is broray_server_subscription_config_hash.
func configHash(rec servers.Record) string {
	c := deepCopy(rec)
	for _, k := range []string{"id", "name", "uri", "source"} {
		delete(c, k)
	}
	return canonicalHash(c)
}

func nameOrID(rec servers.Record) string { return jqRaw(alt(rec["name"], rec["id"])) }

func safeFileID(id string) bool { return servers.ValidID(id) && id != "." && id != ".." }

type oldRow struct {
	key, cont, hash, side string
	file                  srvFile
}

type newRow struct {
	key, cont, id, matchKey string
	rec                     servers.Record
	hash                    string
}

var errMap = "Не удалось однозначно сопоставить серверы подписки. Прежний каталог сохранён."

func (s *svc) writeServer(dir string, rec servers.Record) error {
	if dir == servers.ServersDir(s.d) {
		return servers.WriteRecord(s.d, rec)
	}
	id, _ := rec["id"].(string)
	if !safeFileID(id) {
		return fmt.Errorf("invalid server id")
	}
	if err := servers.Validate(rec, filepath.Join(dir, id+".json")); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return compat.WriteJSON(filepath.Join(dir, id+".json"), rec)
}

func (s *svc) writeActive(id string) error {
	if err := os.MkdirAll(filepath.Dir(servers.ActiveFile(s.d)), 0o700); err != nil {
		return err
	}
	return platform.WriteFileAtomic(servers.ActiveFile(s.d), []byte(id+"\n"), 0o600)
}

// restoreBackup is broray_server_subscription_restore_backup.
func (s *svc) restoreBackup(subID string, live, disabled []srvFile, activeBefore string) error {
	sides := []struct {
		dir   string
		files []srvFile
	}{{servers.ServersDir(s.d), live}, {s.disabledDir(subID), disabled}}
	for _, side := range sides {
		if err := os.MkdirAll(side.dir, 0o700); err != nil {
			return err
		}
		keep := map[string]bool{}
		for _, f := range side.files {
			name := filepath.Base(f.path)
			keep[name] = true
			if err := platform.WriteFileAtomic(filepath.Join(side.dir, name), f.raw, 0o600); err != nil {
				return err
			}
		}
		for _, f := range filterBelongs(readDirServers(side.dir), subID) {
			if !keep[filepath.Base(f.path)] {
				if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
	}
	_ = os.Remove(s.disabledDir(subID))
	if activeBefore != "" {
		return s.writeActive(activeBefore)
	}
	if err := os.Remove(servers.ActiveFile(s.d)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// syncCatalog replaces the subscription's servers by the staged set.
func (s *svc) syncCatalog(ctx context.Context, subID string, stagedRecs []servers.Record, enabled bool, updateID, policy string) (*syncResult, error) {
	stage := make([]servers.Record, 0, len(stagedRecs))
	for _, r := range stagedRecs {
		id, _ := r["id"].(string)
		if servers.Validate(r, id+".json") != nil {
			return nil, fail("SERVER_SYNC_ERROR", "Подготовленный сервер не прошёл проверку серверного модуля.")
		}
		if !belongsTo(r, subID) {
			return nil, fail("SERVER_SYNC_ERROR", "Подготовленный сервер принадлежит другой подписке.")
		}
		stage = append(stage, r)
	}
	if len(stage) == 0 {
		return nil, fail("NO_VALID_NODES", "Серверный модуль не получил ни одного узла.")
	}
	if err := s.syncMarkers(); err != nil {
		return nil, err
	}
	liveDir, disDir := servers.ServersDir(s.d), s.disabledDir(subID)
	work := filepath.Join(s.d.Root.Tmp(), fmt.Sprintf("server-subscription-sync.%d.%s", os.Getpid(), updateID))
	cleanup := func() { _ = os.RemoveAll(work) }
	for _, d := range []string{filepath.Join(work, "backup", "live"), filepath.Join(work, "backup", "disabled"), liveDir, disDir, s.d.RunPath("server-quality")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			cleanup()
			return nil, fail("SERVER_SYNC_ERROR", "Не удалось создать резервную копию серверов.")
		}
	}
	activeBefore := servers.ActiveID(s.d)
	var oldLive, oldDisabled []srvFile
	var olds []oldRow
	var activeOld *srvFile
	for _, side := range []string{"live", "disabled"} {
		dir := liveDir
		if side == "disabled" {
			dir = disDir
		}
		for _, f := range filterBelongs(readDirServers(dir), subID) {
			if err := platform.WriteFileAtomic(filepath.Join(work, "backup", side, filepath.Base(f.path)), f.raw, 0o600); err != nil {
				cleanup()
				if side == "live" {
					return nil, fail("SERVER_SYNC_ERROR", "Не удалось создать резервную копию серверов.")
				}
				return nil, fail("SERVER_SYNC_ERROR", "Не удалось создать резервную копию выключенных серверов.")
			}
			key, _ := servers.CanonicalKey(f.rec)
			olds = append(olds, oldRow{key: key, hash: nodeHash(f.rec), side: side, file: f})
			if side == "live" {
				oldLive = append(oldLive, f)
				if activeBefore != "" && jqRaw(f.rec["id"]) == activeBefore {
					fc := f
					activeOld = &fc
				}
			} else {
				oldDisabled = append(oldDisabled, f)
			}
		}
	}

	// ---- remap (identity continuity) ----
	news, mapCode := s.remap(olds, stage)
	if mapCode != "" {
		cleanup()
		return nil, fail(mapCode, errMap)
	}

	res := &syncResult{Accepted: len(stage), Policy: policy, Impact: "none", Warnings: []string{}}
	oldByKey := map[string]*oldRow{}
	for i := range olds {
		if olds[i].key != "" {
			if _, dup := oldByKey[olds[i].key]; !dup {
				oldByKey[olds[i].key] = &olds[i]
			}
		}
	}
	for i := range news {
		n := &news[i]
		n.hash = nodeHash(n.rec)
		if o, ok := oldByKey[n.matchKey]; !ok {
			res.Added++
		} else if o.hash == n.hash {
			res.Unchanged++
		} else {
			res.Updated++
		}
	}
	type finalRow struct {
		key string
		rec servers.Record
	}
	final := make([]finalRow, 0, len(news))
	for _, n := range news {
		final = append(final, finalRow{n.matchKey, n.rec})
	}
	if policy == "retain-unmatched" {
		mergeFail := func(code string) (*syncResult, error) {
			cleanup()
			return nil, fail(code, "Не удалось безопасно объединить частичный ответ с прежними серверами. Прежний каталог сохранён.")
		}
		seenKey := map[string]bool{}
		for _, o := range olds {
			if seenKey[o.key] {
				return mergeFail("SERVER_SYNC_CONFLICT")
			}
			seenKey[o.key] = true
		}
		newKeys, ids := map[string]bool{}, map[string]bool{}
		for _, f := range final {
			newKeys[f.key] = true
			ids[jqRaw(f.rec["id"])] = true
		}
		for _, o := range olds {
			if o.key == "" || newKeys[o.key] {
				continue
			}
			id := jqRaw(o.file.rec["id"])
			if servers.Validate(o.file.rec, o.file.path) != nil || !safeFileID(id) || o.file.id != id || ids[id] {
				return mergeFail("SERVER_SYNC_CONFLICT")
			}
			ids[id] = true
			res.Retained++
			if len(stage)+res.Retained > maxNodes {
				return mergeFail("PARTIAL_UPDATE_LIMIT")
			}
			final = append(final, finalRow{o.key, o.file.rec})
			newKeys[o.key] = true
		}
		for id := range ids {
			for _, target := range []string{filepath.Join(liveDir, id+".json"), filepath.Join(disDir, id+".json")} {
				st, err := os.Lstat(target)
				if err != nil {
					continue
				}
				if st.Mode()&os.ModeSymlink != 0 {
					return mergeFail("SERVER_SYNC_CONFLICT")
				}
				b, rerr := os.ReadFile(target)
				m, ok := decodeObject(b)
				if rerr != nil || !ok || !belongsTo(m, subID) {
					return mergeFail("SERVER_SYNC_CONFLICT")
				}
			}
		}
	}
	sort.SliceStable(final, func(i, j int) bool { return jqRaw(final[i].rec["id"]) < jqRaw(final[j].rec["id"]) })
	finalKeys := map[string]bool{}
	for _, f := range final {
		finalKeys[f.key] = true
	}
	for _, o := range olds {
		if o.key != "" && !finalKeys[o.key] {
			res.Removed++
		}
	}

	// ---- active server ----
	var activeNew servers.Record
	activeNewID, nameChanged, configChanged := "", false, false
	if activeOld != nil {
		activeKey, _ := servers.CanonicalKey(activeOld.rec)
		for _, f := range final {
			if f.key == activeKey {
				activeNew = f.rec
				break
			}
		}
		if activeNew == nil {
			cleanup()
			return nil, fail("ACTIVE_SERVER_CONFLICT", "Активный сервер исчез из подписки. Обновление отменено, сохранена прежняя рабочая конфигурация.")
		}
		activeNewID = jqRaw(activeNew["id"])
		if nameOrID(activeOld.rec) != nameOrID(activeNew) {
			nameChanged, res.Impact = true, "display-name-changed"
		}
		if configHash(activeOld.rec) != configHash(activeNew) {
			configChanged, res.Impact = true, "configuration-changed"
			if err := testActiveConfig(ctx, s.d, activeNew); err != nil {
				cleanup()
				return nil, fail("ACTIVE_SERVER_CONFLICT", "Новая конфигурация активного сервера не прошла проверку Xray. Обновление отменено.")
			}
		} else if activeNewID != activeBefore {
			res.Impact = "configuration-changed"
		}
	}

	target := liveDir
	if !enabled {
		target = disDir
	}
	_ = os.MkdirAll(target, 0o700)
	if platform.WriteFileAtomic(filepath.Join(work, "backup", "active-server"), []byte(activeBefore+"\n"), 0o600) != nil ||
		platform.WriteFileAtomic(filepath.Join(work, "rollback-required"), []byte(subID+"\n"), 0o600) != nil {
		cleanup()
		return nil, fail("SERVER_SYNC_ERROR", "Не удалось подготовить восстановление серверов.")
	}
	rollback := func(code, msg string) (*syncResult, error) {
		if err := s.restoreBackup(subID, oldLive, oldDisabled, activeBefore); err != nil {
			return nil, fail("SERVER_SYNC_ROLLBACK_FAILED", "Восстановление серверов не завершено. Резервная копия сохранена: "+filepath.Join(work, "backup"))
		}
		cleanup()
		return nil, fail(code, msg)
	}
	for _, f := range final {
		id := jqRaw(f.rec["id"])
		p := filepath.Join(target, id+".json")
		if regular(p) {
			if b, err := os.ReadFile(p); err == nil {
				if m, ok := decodeObject(b); ok && sameIgnoringUpdatedAt(m, f.rec) {
					continue
				}
			}
		}
		if err := s.writeServer(target, f.rec); err != nil {
			return rollback("SERVER_SYNC_ERROR", "Не удалось атомарно сохранить серверы подписки.")
		}
	}
	if activeOld != nil {
		if !enabled {
			return rollback("ACTIVE_SERVER_CONFLICT", "Активный сервер не может быть перенесён в выключенный источник.")
		}
		if configChanged {
			if activeNewID != activeBefore {
				if err := s.writeActive(activeNewID); err != nil {
					return rollback("SERVER_SYNC_ERROR", "Не удалось обновить идентификатор активного сервера.")
				}
			}
			if err := s.d.Svc.ReapplyActiveServer(ctx, "subscription-update"); err != nil {
				s.d.Log.Printf("subscriptions: subscription=%s active server reapply failed: %v", subID, err)
				r, e := rollback("ACTIVE_SERVER_CONFLICT", "Новая версия активного сервера не была применена. Восстановлена прежняя рабочая версия.")
				if se, ok := e.(*subErr); ok && se.Code == "ACTIVE_SERVER_CONFLICT" {
					_ = s.d.Svc.ReapplyActiveServer(ctx, "subscription-rollback")
				}
				return r, e
			}
		} else if activeNewID != activeBefore {
			if err := s.writeActive(activeNewID); err != nil {
				return rollback("SERVER_SYNC_ERROR", "Не удалось обновить идентификатор активного сервера.")
			}
		}
		if nameChanged && !configChanged {
			ictx, cancel := context.WithTimeout(ctx, interfaceTimeout)
			err := s.d.Svc.EnsureInterface(ictx)
			cancel()
			if err != nil {
				s.d.Log.Printf("subscriptions: subscription=%s interface description sync failed: %v", subID, err)
				return rollback("ACTIVE_SERVER_CONFLICT", "Новое имя активного сервера не удалось синхронизировать с управляемым интерфейсом. Восстановлена прежняя версия подписки.")
			}
		}
	}
	keep := map[string]bool{}
	for _, f := range final {
		keep[jqRaw(f.rec["id"])] = true
	}
	for _, dir := range []string{liveDir, disDir} {
		for _, f := range filterBelongs(readDirServers(dir), subID) {
			if dir != target || !keep[jqRaw(f.rec["id"])] {
				_ = s.removeFile(f)
			}
		}
	}
	_ = os.Remove(disDir)
	cleanup()
	res.Total = len(final)
	if res.Retained > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Сохранено прежних серверов: %d. Ответ разобран не полностью; отсутствие этих серверов не подтверждено. Их параметры могут быть устаревшими. Удаление отсутствующих серверов возможно после обновления без отклонённых узлов.", res.Retained))
	}
	return res, nil
}

// remap is broray_server_subscription_remap: exact importKey match first,
// then a unique one-to-one continuity match keeps the old server id.
// It returns the remapped staged rows or an error code.
func (s *svc) remap(olds []oldRow, stage []servers.Record) ([]newRow, string) {
	type entry struct {
		key, cont, id string
		rec           servers.Record
	}
	var oldE, newE []entry
	for i := range olds {
		o := &olds[i]
		if o.key == "" {
			continue
		}
		id := jqRaw(o.file.rec["id"])
		if servers.Validate(o.file.rec, o.file.path) != nil || !safeFileID(id) || o.file.id != id {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		cont, err := servers.ContinuityKey(o.file.rec)
		if err != nil {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		o.cont = cont
		oldE = append(oldE, entry{o.key, cont, id, o.file.rec})
	}
	sorted := append([]servers.Record(nil), stage...)
	sort.SliceStable(sorted, func(i, j int) bool { return jqRaw(sorted[i]["id"]) < jqRaw(sorted[j]["id"]) })
	for _, r := range sorted {
		id, _ := r["id"].(string)
		src := sourceOf(r)
		ik, _ := src["importKey"].(string)
		if !safeFileID(id) || ik == "" {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		key, err1 := servers.CanonicalKey(r)
		cont, err2 := servers.ContinuityKey(r)
		if err1 != nil || err2 != nil {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		newE = append(newE, entry{key, cont, id, r})
	}
	dupKeys := func(es []entry) bool {
		seen := map[string]bool{}
		for _, e := range es {
			if seen[e.key] {
				return true
			}
			seen[e.key] = true
		}
		return false
	}
	if dupKeys(oldE) || dupKeys(newE) {
		return nil, "SERVER_SYNC_AMBIGUOUS_IDENTITY"
	}
	exact := make([]*entry, len(newE))
	matchedOld := map[string]bool{}
	for i, n := range newE {
		for j := range oldE {
			if oldE[j].key == n.key {
				exact[i] = &oldE[j]
				matchedOld[oldE[j].id] = true
				break
			}
		}
	}
	var remOld []*entry
	for j := range oldE {
		if !matchedOld[oldE[j].id] {
			remOld = append(remOld, &oldE[j])
		}
	}
	oldCount, newCount := map[string]int{}, map[string]int{}
	for _, o := range remOld {
		oldCount[o.cont]++
	}
	for i, n := range newE {
		if exact[i] == nil {
			newCount[n.cont]++
		}
	}
	for c, oc := range oldCount {
		if nc := newCount[c]; oc > 0 && nc > 0 && (oc > 1 || nc > 1) {
			return nil, "SERVER_SYNC_AMBIGUOUS_IDENTITY"
		}
	}
	out := make([]newRow, 0, len(newE))
	used := map[string]bool{}
	for i, n := range newE {
		o := exact[i]
		if o == nil {
			for _, r := range remOld {
				if r.cont == n.cont {
					o = r
					break
				}
			}
		}
		row := newRow{key: n.key, cont: n.cont, id: n.id, matchKey: n.key}
		if o != nil {
			row.id, row.matchKey = o.id, o.key
		}
		if used[row.id] {
			return nil, "SERVER_SYNC_CONFLICT"
		}
		used[row.id] = true
		rec := deepCopy(n.rec)
		rec["id"] = row.id
		if src, ok := rec["source"].(map[string]any); ok {
			src["importKey"] = n.key
		}
		row.rec = rec
		out = append(out, row)
	}
	return out, ""
}
