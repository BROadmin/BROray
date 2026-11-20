package subscriptions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	if err := compat.CheckSourceSyncWorkspaces(s.d.Root); err != nil {
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
	previousKeys            []string
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
	return s.syncCatalogCommit(ctx, subID, stage, enabled, policy)
}
