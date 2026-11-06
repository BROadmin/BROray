package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

// Settings backup part "subscriptions" (compat.BackupPart): every
// subscription record (URL, User-Agent, device-info and schedule settings,
// last provider data) and its server catalog — enabled servers from
// servers/ and the servers of a disabled subscription from
// config/disabled-subscription-servers/<id>/. Locks, run state and logs
// are not saved.
//
// Import: a subscription of the backup is added or replaces the one with
// the same id together with its whole catalog (servers of that subscription
// missing from the backup are removed); other subscriptions stay. It stages
// before the servers part, which then checks the active server still exists.

const backupMaxSubscriptions = 256

type subCatalog struct {
	Live     []json.RawMessage `json:"live"`
	Disabled []json.RawMessage `json:"disabled"`
}

type subsBackup struct {
	Records  []json.RawMessage     `json:"records"`
	Catalogs map[string]subCatalog `json:"catalogs"`
}

func registerBackup(s *svc) {
	compat.RegisterBackupPart(compat.BackupPart{
		Name: "subscriptions", Order: 10,
		Export: func(ctx context.Context) (any, error) { return s.backupExport(), nil },
		Stage:  s.backupStage,
	})
}

func (s *svc) backupExport() any {
	records := []any{}
	catalogs := map[string]any{}
	live := s.liveServers()
	for _, rec := range s.validRecords() {
		id, _ := rec["id"].(string)
		if validID(id) != nil {
			continue
		}
		records = append(records, rec)
		l, d := []any{}, []any{}
		for _, f := range filterBelongs(live, id) {
			l = append(l, f.rec)
		}
		for _, f := range filterBelongs(s.disabledServers(id), id) {
			d = append(d, f.rec)
		}
		catalogs[id] = map[string]any{"live": l, "disabled": d}
	}
	return map[string]any{"records": records, "catalogs": catalogs}
}

func backupInvalid(format string, args ...any) error {
	return fmt.Errorf("BACKUP_INVALID: "+format, args...)
}

func subName(m map[string]any) string {
	if n, ok := m["name"].(string); ok && n != "" {
		return n
	}
	s, _ := m["id"].(string)
	return s
}

func canonicalEqual(a, b any) bool {
	x, err1 := compat.Canonical(a)
	y, err2 := compat.Canonical(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

func (s *svc) backupStage(ctx context.Context, raw json.RawMessage, st *compat.BackupStage) (compat.BackupPlan, error) {
	var plan compat.BackupPlan
	var in subsBackup
	if len(raw) == 0 || string(raw) == "null" {
		return plan, nil
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return plan, backupInvalid("раздел подписок повреждён")
	}
	if len(in.Records) > backupMaxSubscriptions {
		return plan, backupInvalid("слишком много подписок (%d)", len(in.Records))
	}
	stageSvc := newSvc(st.St)
	seen := map[string]bool{}
	type item struct {
		rec  map[string]any
		live []servers.Record
		dis  []servers.Record
	}
	var items []item
	for _, r := range in.Records {
		rec, ok := decodeObject(r)
		id, _ := rec["id"].(string)
		if !ok || validID(id) != nil || id[0] == '.' || len(id) > 128 {
			return plan, backupInvalid("некорректный идентификатор подписки %q", id)
		}
		if seen[id] {
			return plan, backupInvalid("подписка %s указана дважды", id)
		}
		seen[id] = true
		if rec["lastUpdateStatus"] == "running" {
			// the refresh that was running when the backup was made did not finish here
			rec["lastUpdateStatus"] = "error"
			rec["lastError"] = "Обновление прервано. Сохранены последние доступные данные подписки."
		}
		if !validRecord(rec) {
			return plan, backupInvalid("подписка «%s» не прошла проверку", subName(rec))
		}
		if _, err := parseURL(rec["url"].(string)); err != nil {
			return plan, backupInvalid("адрес подписки «%s» некорректен: %s", subName(rec), err.Msg)
		}
		it := item{rec: rec}
		cat := in.Catalogs[id]
		for _, list := range []struct {
			raws []json.RawMessage
			dst  *[]servers.Record
		}{{cat.Live, &it.live}, {cat.Disabled, &it.dis}} {
			for _, sr := range list.raws {
				srv, err := servers.DecodeBackupRecord(sr, "subscriptions/"+id)
				if err != nil {
					return plan, err
				}
				if !belongsTo(srv, id) {
					return plan, backupInvalid("сервер %v не принадлежит подписке «%s»", srv["id"], subName(rec))
				}
				*list.dst = append(*list.dst, srv)
			}
		}
		if len(it.live)+len(it.dis) > maxNodes {
			return plan, backupInvalid("у подписки «%s» слишком много серверов", subName(rec))
		}
		if rec["enabled"] == false && len(it.live) > 0 {
			return plan, backupInvalid("выключенная подписка «%s» содержит включённые серверы", subName(rec))
		}
		items = append(items, it)
	}
	for id := range in.Catalogs {
		if !seen[id] {
			return plan, backupInvalid("серверы указаны для отсутствующей подписки %q", id)
		}
	}
	if len(items) == 0 {
		return plan, nil
	}
	for _, rel := range []string{compat.DataRel("config", "subscriptions"), compat.DataRel("config", "disabled-subscription-servers"), compat.DataRel("servers")} {
		if err := st.Take(rel); err != nil {
			return plan, err
		}
	}
	srvIDs := map[string]string{} // server id → subscription id (uniqueness across the backup)
	changedAny := false
	for _, it := range items {
		id := it.rec["id"].(string)
		cur, _, err := s.readRecord(id)
		switch {
		case err != nil:
			plan.Add("subscription", id, subName(it.rec), "add", "")
			changedAny = true
		case canonicalEqual(cur, it.rec):
			plan.Add("subscription", id, subName(it.rec), "unchanged", "")
		default:
			plan.Add("subscription", id, subName(it.rec), "replace", "")
			changedAny = true
		}
		if err := stageSvc.writeRecord(it.rec); err != nil {
			return plan, fmt.Errorf("BACKUP_STAGE_FAILED: подписка %s не записана", id)
		}
		detail := "подписка «" + subName(it.rec) + "»"
		want := map[string]bool{}
		for _, srv := range append(append([]servers.Record(nil), it.live...), it.dis...) {
			sid := srv["id"].(string)
			if other, dup := srvIDs[sid]; dup {
				return plan, backupInvalid("сервер %s указан дважды (подписки %s и %s)", sid, other, id)
			}
			srvIDs[sid] = id
			want[sid] = true
		}
		// current catalog (live tree) → planned changes
		current := map[string]servers.Record{}
		for _, f := range append(filterBelongs(s.liveServers(), id), filterBelongs(s.disabledServers(id), id)...) {
			current[f.id] = f.rec
		}
		for _, srv := range append(append([]servers.Record(nil), it.live...), it.dis...) {
			sid := srv["id"].(string)
			switch old, ok := current[sid]; {
			case !ok:
				plan.Add("server", sid, srvName(srv), "add", detail)
			case canonicalEqual(old, srv):
				plan.Add("server", sid, srvName(srv), "unchanged", detail)
			default:
				plan.Add("server", sid, srvName(srv), "replace", detail)
			}
		}
		for sid, old := range current {
			if !want[sid] {
				plan.Add("server", sid, srvName(old), "remove", detail)
			}
		}
		// stage: drop the subscription's servers, then write the backup catalog
		for _, f := range filterBelongs(stageSvc.liveServers(), id) {
			if err := os.Remove(f.path); err != nil {
				return plan, err
			}
		}
		for _, f := range stageSvc.liveServers() {
			if want[f.id] {
				return plan, fmt.Errorf("BACKUP_CONFLICT: сервер %s уже существует и не принадлежит подписке «%s»", f.id, subName(it.rec))
			}
		}
		if err := os.RemoveAll(stageSvc.disabledDir(id)); err != nil {
			return plan, err
		}
		for _, srv := range it.live {
			if err := servers.WriteRecord(st.St, srv); err != nil {
				return plan, fmt.Errorf("BACKUP_STAGE_FAILED: сервер %v не записан: %v", srv["id"], err)
			}
		}
		if len(it.dis) > 0 {
			dir := stageSvc.disabledDir(id)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return plan, err
			}
			for _, srv := range it.dis {
				if err := compat.WriteJSON(filepath.Join(dir, srv["id"].(string)+".json"), srv); err != nil {
					return plan, err
				}
			}
		}
	}
	for _, id := range s.recordIDs() {
		if !seen[id] {
			if rec, _, err := s.readRecord(id); err == nil {
				plan.Add("subscription", id, subName(rec), "keep", "")
			}
		}
	}
	if changedAny {
		plan.FollowUps = append(plan.FollowUps, "Подписки восстановлены с последним сохранённым составом серверов; при необходимости обновите их на странице «Подписки».")
	}
	return plan, nil
}

func srvName(rec servers.Record) string {
	if n, ok := rec["name"].(string); ok && strings.TrimSpace(n) != "" {
		return n
	}
	s, _ := rec["id"].(string)
	return s
}
