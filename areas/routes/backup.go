package routes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
)

// Settings backup part "routes" (compat.BackupPart): the custom route sets
// as their uploaded BAT sources (name + files, like custom-preview.cgi
// receives them) and the ids of the installed sets. Route lists, Keenetic
// registries/receipts, transactions and catalogs of built-in sets are not
// saved: they are rebuilt from the sources or the pinned catalog.
//
// Import runs the normal custom-preview + custom-commit pipeline (same BAT
// safety checks) on the staging root, keeping the bundle ids of the backup.
// Nothing is installed on Keenetic: installing sets is a follow-up the
// owner starts on the «Маршруты» page.

const backupMaxCustom = 64

type backupFile struct {
	Name          string `json:"name"`
	ContentBase64 string `json:"contentBase64"`
}

type backupCustom struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Installed bool         `json:"installed"`
	Files     []backupFile `json:"files"`
}

type routesBackup struct {
	Custom           []backupCustom  `json:"custom"`
	BuiltinInstalled []string        `json:"builtinInstalled"`
	Schedule         *backupSchedule `json:"schedule,omitempty"` // scheduled catalog updates (optional)
}

type backupSchedule struct {
	IntervalHours     int  `json:"intervalHours"`
	AutoApply         bool `json:"autoApply"`
	MaxRemovalPercent int  `json:"maxRemovalPercent"`
}

func registerBackup(e *engine) {
	compat.RegisterBackupPart(compat.BackupPart{
		Name: "routes", Order: 30,
		Export: func(ctx context.Context) (any, error) { return e.backupExport() },
		Stage:  e.backupStage,
		After: func(ctx context.Context, _ any) compat.BackupPlan {
			e.ensureCustomRegistries()
			return compat.BackupPlan{}
		},
	})
}

// ensureCustomRegistries runs after an import or undo (global fence held):
// every listed custom set gets its empty registry (as prepare seeds the
// built-in ones), and an empty seeded registry of a custom set that is no
// longer listed is removed. A registry with any receipt is never touched.
func (e *engine) ensureCustomRegistries() {
	iface := e.managedInterface()
	if iface == "" {
		iface = "Proxy0"
	}
	listed := map[string]bool{}
	for _, id := range e.customIDs() {
		listed[id] = true
		if !validUserID(id) || fileExists(e.registryFile(id)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(e.registryFile(id)), 0o700); err == nil {
			if err := writeObj(e.registryFile(id), seedRegistry(id, iface)); err != nil {
				e.log.Printf("routes: backup: registry %s: %v", id, err)
			}
		}
	}
	m, _ := filepath.Glob(e.p("installed", "bundles", "*.json"))
	for _, p := range m {
		id := strings.TrimSuffix(filepath.Base(p), ".json")
		if !validUserID(id) || listed[id] || e.bundleListed(id) {
			continue
		}
		r, ok := readObj(p)
		if ok && str(r, "bundleId") == id && r["installedVersion"] == nil && r["installedAt"] == nil &&
			len(anyList(r["managedRouteKeys"])) == 0 && len(anyList(r["routeKeys"])) == 0 && len(anyList(r["externalRouteKeys"])) == 0 {
			_ = os.Remove(p)
		}
	}
}

// stateInstalled: state and registry both record an installed version.
func (e *engine) stateInstalled(id string) bool {
	st, ok1 := readObj(e.stateFile(id))
	reg, ok2 := readObj(e.registryFile(id))
	return ok1 && ok2 && st["installedVersion"] != nil && reg["installedVersion"] != nil
}

// customSources reads the stored BAT sources of a custom bundle (verified
// against source-files.json) in their upload order.
func (e *engine) customSources(id string) ([]backupFile, error) {
	dir := e.catalogDir(id)
	var meta any
	if readJSONAny(filepath.Join(dir, "source-files.json"), &meta) != nil || !verifySourceFiles(filepath.Join(dir, "source"), meta) {
		return nil, fmt.Errorf("BACKUP_SOURCE_DAMAGED: исходные BAT-файлы набора %s повреждены", id)
	}
	var out []backupFile
	for _, x := range anyList(meta) {
		m, _ := x.(obj)
		b, err := os.ReadFile(filepath.Join(dir, "source", str(m, "storedName")))
		if err != nil {
			return nil, err
		}
		out = append(out, backupFile{Name: str(m, "originalName"), ContentBase64: base64.StdEncoding.EncodeToString(b)})
	}
	return out, nil
}

func (e *engine) backupExport() (any, error) {
	out := routesBackup{Custom: []backupCustom{}, BuiltinInstalled: []string{}}
	for _, id := range e.customIDs() {
		if !validUserID(id) {
			continue
		}
		entry, _ := e.customEntry(id)
		files, err := e.customSources(id)
		if err != nil {
			return nil, err
		}
		out.Custom = append(out.Custom, backupCustom{ID: id, Name: str(entry, "name"), Installed: e.stateInstalled(id), Files: files})
	}
	for _, id := range builtinIDs() {
		if e.stateInstalled(id) {
			out.BuiltinInstalled = append(out.BuiltinInstalled, id)
		}
	}
	ss := e.scheduleSettings()
	out.Schedule = &backupSchedule{IntervalHours: ss.IntervalHours, AutoApply: ss.AutoApply, MaxRemovalPercent: ss.MaxRemovalPercent}
	return out, nil
}

func backupInvalid(format string, args ...any) error {
	return fmt.Errorf("BACKUP_INVALID: "+format, args...)
}

// sameSources: same name and the same uploaded files in the same order.
func (e *engine) sameSources(id, name string, files []backupFile) bool {
	entry, ok := e.customEntry(id)
	if !ok || str(entry, "name") != name {
		return false
	}
	cur, err := e.customSources(id)
	if err != nil || len(cur) != len(files) {
		return false
	}
	for i := range cur {
		a, okA := decodeBase64(cur[i].ContentBase64)
		b, okB := decodeBase64(files[i].ContentBase64)
		if !okA || !okB || cur[i].Name != files[i].Name || sha256Hex(a) != sha256Hex(b) {
			return false
		}
	}
	return true
}

func (e *engine) backupStage(ctx context.Context, raw json.RawMessage, st *compat.BackupStage) (compat.BackupPlan, error) {
	var plan compat.BackupPlan
	var in routesBackup
	if len(raw) == 0 || string(raw) == "null" {
		return plan, nil
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return plan, backupInvalid("раздел маршрутов повреждён")
	}
	if len(in.Custom) > backupMaxCustom {
		return plan, backupInvalid("слишком много пользовательских наборов (%d)", len(in.Custom))
	}
	var install []string
	seenBuiltin := map[string]bool{}
	for _, id := range in.BuiltinInstalled {
		b, ok := findBuiltin(id)
		if !ok {
			return plan, backupInvalid("неизвестный набор маршрутов %q", id)
		}
		if !seenBuiltin[id] && !e.stateInstalled(id) {
			install = append(install, b.Name)
		}
		seenBuiltin[id] = true
	}
	se := newEngine(st.St) // the staging root: same code, no Keenetic transport
	if in.Schedule != nil {
		sc := *in.Schedule
		if (sc.IntervalHours != 0 && !validInterval(sc.IntervalHours)) || !validRemovalPct(sc.MaxRemovalPercent) {
			return plan, backupInvalid("некорректные настройки планового обновления наборов")
		}
		cur := e.scheduleSettings()
		if cur.IntervalHours != sc.IntervalHours || cur.AutoApply != sc.AutoApply || cur.MaxRemovalPercent != sc.MaxRemovalPercent {
			if err := st.Take(compat.DataRel("routes", "catalog-schedule.json")); err != nil {
				return plan, err
			}
			if err := se.saveScheduleSettings(schedSettings{IntervalHours: sc.IntervalHours, AutoApply: sc.AutoApply, MaxRemovalPercent: sc.MaxRemovalPercent}); err != nil {
				return plan, err
			}
			plan.Add("setting", "routes-schedule", "Плановое обновление готовых наборов", "replace", "")
		}
	}
	seen := map[string]bool{}
	staged := false
	for _, c := range in.Custom {
		if !validUserID(c.ID) {
			return plan, backupInvalid("некорректный идентификатор пользовательского набора %q", c.ID)
		}
		if seen[c.ID] {
			return plan, backupInvalid("пользовательский набор %s указан дважды", c.ID)
		}
		seen[c.ID] = true
		name, ok := userName(c.Name)
		if !ok || !validUserName(name) {
			return plan, backupInvalid("некорректное название пользовательского набора %s", c.ID)
		}
		exists := e.userBundleExists(c.ID)
		if exists && e.sameSources(c.ID, name, c.Files) {
			plan.Add("customRoutes", c.ID, name, "unchanged", "")
			if c.Installed && !e.stateInstalled(c.ID) {
				install = append(install, name)
			}
			continue
		}
		if !staged {
			for _, rel := range []string{"custom.json", "bundles.json"} {
				if err := st.Take(compat.DataRel("routes", rel)); err != nil {
					return plan, err
				}
			}
			if err := st.Copy(compat.DataRel("routes", "config.json")); err != nil {
				return plan, err
			}
			staged = true
		}
		for _, rel := range []string{filepath.Join("catalog", c.ID), filepath.Join("manifests", c.ID+".json"), filepath.Join("state", c.ID+".json")} {
			if err := st.Take(compat.DataRel("routes", rel)); err != nil {
				return plan, err
			}
		}
		// a registry records Keenetic ownership: it is never swapped (an
		// undo must not drop receipts of an install made after the import);
		// a missing one is seeded live by After (ensureCustomRegistries)
		if err := st.Copy(compat.DataRel("routes", "installed", "bundles", c.ID+".json")); err != nil {
			return plan, err
		}
		files := make([]any, 0, len(c.Files))
		for _, f := range c.Files {
			files = append(files, obj{"name": f.Name, "contentBase64": f.ContentBase64})
		}
		report, aerr := se.userPreview(obj{"name": name, "files": files})
		if aerr != nil {
			return plan, backupInvalid("пользовательский набор «%s» не прошёл проверку: %s", name, strings.TrimSuffix(aerr.Message, "."))
		}
		req := obj{"token": str(report, "token"), "name": name, "bundleId": nil}
		newID := c.ID
		if exists {
			req["bundleId"], newID = c.ID, ""
		}
		if _, aerr := se.userCommitAs(req, newID); aerr != nil {
			return plan, fmt.Errorf("BACKUP_STAGE_FAILED: пользовательский набор «%s» не сохранён: %s", name, aerr.Message)
		}
		action := "add"
		if exists {
			action = "replace"
		}
		plan.Add("customRoutes", c.ID, name, action, fmt.Sprintf("маршрутов: %d", intOr(report["exportRouteCount"], 0)))
		if c.Installed || (exists && e.stateInstalled(c.ID)) {
			install = append(install, name)
		}
	}
	for _, id := range e.customIDs() {
		if !seen[id] && validUserID(id) {
			entry, _ := e.customEntry(id)
			plan.Add("customRoutes", id, str(entry, "name"), "keep", "")
		}
	}
	if len(install) > 0 {
		plan.FollowUps = append(plan.FollowUps, "Установите наборы маршрутов на странице «Маршруты»: "+strings.Join(install, ", ")+".")
	}
	return plan, nil
}
