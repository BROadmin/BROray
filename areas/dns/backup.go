package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
)

// Settings backup part "dns" (compat.BackupPart): the owner's DNS-over-TLS
// selection (config.json requestedIds) and the auto-check setting
// (auto-check.json). Receipts of the servers BROray wrote to Keenetic
// (config.managed), tests and the transaction marker are not saved, and an
// import (or its undo) merges only the selection fields into the live
// config.json (BackupStage.TakeFields).
//
// Import only changes the saved selection; writing it to Keenetic needs a
// fresh test and «Применить» on the DNS-over-TLS page (follow-up).

type dnsBackup struct {
	RequestedIDs     *[]string `json:"requestedIds"`
	AppliedIDs       []string  `json:"appliedIds"` // informational: what was on Keenetic when the backup was made
	AutoCheckEnabled *bool     `json:"autoCheckEnabled"`
}

func registerBackup(s *service) {
	compat.RegisterBackupPart(compat.BackupPart{
		Name: "dns", Order: 40,
		Export: func(ctx context.Context) (any, error) { return s.backupExport(), nil },
		Stage:  s.backupStage,
	})
}

// liveSelection returns config.json requestedIds and effectiveIds (ok=false if unreadable).
func (s *service) liveSelection() (requested, effective []string, ok bool) {
	cfg, err := readMap(s.configPath())
	if err != nil {
		return nil, nil, false
	}
	requested, ok1 := stringList(cfg["requestedIds"])
	if !ok1 {
		requested, ok1 = stringList(cfg["selectedIds"])
	}
	effective, _ = stringList(cfg["effectiveIds"])
	return requested, effective, ok1
}

func (s *service) backupExport() any {
	out := map[string]any{}
	if req, eff, ok := s.liveSelection(); ok {
		out["requestedIds"] = strSlice(req)
		out["appliedIds"] = strSlice(eff)
	}
	if enabled, valid := s.autoEnabled(); valid {
		out["autoCheckEnabled"] = enabled
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func catalogNames(ids []string) string {
	var names []string
	for _, id := range ids {
		if c, ok := findCatalog(id); ok {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		return "нет"
	}
	return strings.Join(names, ", ")
}

func (s *service) backupStage(ctx context.Context, raw json.RawMessage, st *compat.BackupStage) (compat.BackupPlan, error) {
	var plan compat.BackupPlan
	var in dnsBackup
	if len(raw) == 0 || string(raw) == "null" {
		return plan, nil
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return plan, fmt.Errorf("BACKUP_INVALID: раздел DNS-over-TLS повреждён")
	}
	ss := newService(st.St)
	ss.now = s.now
	liveReq, liveEff, _ := s.liveSelection()
	if in.RequestedIDs != nil {
		ids := *in.RequestedIDs
		if len(ids) > maxServers || len(uniqueSorted(ids)) != len(ids) {
			return plan, fmt.Errorf("BACKUP_INVALID: выбор DNS-over-TLS некорректен")
		}
		for _, id := range ids {
			if _, ok := findCatalog(id); !ok {
				return plan, fmt.Errorf("BACKUP_INVALID: неизвестный сервер DNS-over-TLS %q", id)
			}
		}
		if !equalStrings(ids, liveReq) {
			plan.Add("setting", "dotSelection", "Выбор DNS-over-TLS", "replace", catalogNames(ids))
			// only the selection is restored: config.managed and the other
			// receipts of what BROray wrote to Keenetic stay as they are live
			if err := st.TakeFields(compat.DataRel("routes", "dot", "config.json"), "requestedIds", "selectedIds"); err != nil {
				return plan, err
			}
			f, err := ss.ensureFiles()
			if err != nil {
				return plan, fmt.Errorf("DOT_CONFIG_INVALID: текущая конфигурация DNS-over-TLS повреждена; выбор не восстановлен")
			}
			cfg := f.config
			cfg["requestedIds"], cfg["selectedIds"], cfg["updatedAt"] = strSlice(ids), strSlice(ids), stamp(ss.now())
			if err := writeMap(ss.configPath(), cfg); err != nil {
				return plan, err
			}
		}
		// Keenetic is never changed here: ask for test + apply when the
		// restored selection is not what this router has applied and either
		// the import changed it or the backup's router had it applied.
		changed := !equalStrings(ids, liveReq)
		wasApplied := len(in.AppliedIDs) > 0 && equalStrings(sortedIDs(in.AppliedIDs), sortedIDs(ids))
		if (changed || wasApplied) && !equalStrings(sortedIDs(ids), sortedIDs(liveEff)) {
			plan.FollowUps = append(plan.FollowUps, "Проверьте и примените DNS-over-TLS на странице «DNS-over-TLS» (выбрано: "+catalogNames(ids)+").")
		}
	}
	if in.AutoCheckEnabled != nil {
		cur, _ := s.autoEnabled()
		if cur != *in.AutoCheckEnabled {
			state := "выключена"
			if *in.AutoCheckEnabled {
				state = "включена"
			}
			plan.Add("setting", "dotAutoCheck", "Автопроверка DNS-over-TLS", "replace", state)
			if err := st.Take(compat.DataRel("routes", "dot", "auto-check.json")); err != nil {
				return plan, err
			}
			if err := ss.autoSave(*in.AutoCheckEnabled); err != nil {
				return plan, err
			}
		}
	}
	return plan, nil
}
