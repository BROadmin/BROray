package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

// Settings backup on the BROray page (card «Резервная копия настроек»):
//
//	POST /api/broray/backup-export.cgi  {encrypt,password|acknowledgeUnencrypted} → the file (download)
//	POST /api/broray/backup-preview.cgi {file,password}                          → what an import changes + confirmToken
//	POST /api/broray/backup-import.cgi  {file,password,confirmToken}             → applied result
//	GET  /api/broray/backup-status.cgi                                           → pre-import copies on disk
//	POST /api/broray/backup-undo.cgi    {copyId}                                 → restore a pre-import copy
//
// Every POST needs X-BROray-Request: 1 (the BROray page rule) and a
// same-origin Origin (compat.OriginAllowed), after method and session.
// Responses are envelope B except the successful export, which is the file.
//
// Contents come from the areas' compat.BackupPart registrations (servers,
// subscriptions, routes, dns) plus the "settings" part of this area
// (config.v1.json logLevel and health). Keenetic receipts, operation
// journals, sessions, logs, caches, run state and the Xray binary are never
// part of a backup. An import takes the global fence, stages every part,
// refuses when the preview's confirmToken no longer matches, swaps the
// staged paths in and keeps the replaced originals as a pre-import copy
// under data/backup/settings-before-import-<UTC time>-<hex>/ (undo). The
// copy's manifest records a digest of every swapped path as the import left
// it; undo is refused (409) once any of them changed, so it never brings
// back state older than an install or DoT apply made after the import.

const (
	backupBodyMax        = 8 << 20
	backupKeep           = 5
	backupCopyRoot       = "root"
	backupStageOwnerFile = ".broray-stage-owner"
	backupStageOwnerText = "BROray backup stage v1\n"
)

var (
	errBackupBusy  = berr(409, "BACKUP_OPERATION_BUSY", "Другая операция BROray уже выполняется. Повторите позже.")
	errBackupToken = berr(409, "BACKUP_PLAN_CHANGED", "Состояние BROray изменилось после проверки файла. Проверьте файл заново.")
	// undo would overwrite what changed after the import (installed route sets, DoT receipts, edits)
	errBackupUndoChanged = berr(409, "BACKUP_UNDO_STATE_CHANGED", "Состояние изменилось после восстановления — отмена невозможна.")
	reBackupCopyID       = regexp.MustCompile(`^settings-before-import-[0-9]{8}-[0-9]{6}-[0-9a-f]{8}$`)
	reBackupStageID      = regexp.MustCompile(`^\.stage-[0-9a-f]{16}$`)
	reBackupPruneCopyID  = regexp.MustCompile(`^settings-before-(import|undo)-[0-9]{8}-[0-9]{6}-[0-9a-f]{8}$`)
)

func registerBackup() {
	tooLarge := cgi.TooLarge(cgi.E(413, "BACKUP_TOO_LARGE", "Файл резервной копии слишком большой (больше 4 МБ)."))
	small := []cgi.Opt{cgi.POST(), cgi.TooLarge(tooLarge413)}
	cgi.RegisterRaw("/api/broray/backup-export.cgi", func(c *cgi.Req) {
		if backupGuard(c) {
			current().backupExportHandler(c)
		}
	}, small...)
	cgi.RegisterRaw("/api/broray/backup-preview.cgi", func(c *cgi.Req) {
		if backupGuard(c) {
			current().backupPreviewHandler(c)
		}
	}, cgi.POST(), cgi.Limit(backupBodyMax), tooLarge)
	cgi.RegisterRaw("/api/broray/backup-import.cgi", func(c *cgi.Req) {
		if backupGuard(c) {
			current().backupImportHandler(c)
		}
	}, cgi.POST(), cgi.Limit(backupBodyMax), tooLarge)
	cgi.Register("/api/broray/backup-status.cgi", func(c *cgi.Req) (any, error) {
		return current().backupStatus(), nil
	})
	cgi.RegisterRaw("/api/broray/backup-undo.cgi", func(c *cgi.Req) {
		if backupGuard(c) {
			current().backupUndoHandler(c)
		}
	}, small...)
}

// backupGuard: CSRF header of the BROray page, then the Origin check of the
// operations API (a backup carries every server secret, so a cross-site
// form post must never reach it).
func backupGuard(c *cgi.Req) bool {
	return requireBROrayOrigin(c)
}

func writeBackupErr(c *cgi.Req, err error) {
	var be *backupError
	if errors.As(err, &be) {
		cgi.WriteErr(c.W, cgi.E(be.Status, be.Code, be.Message))
		return
	}
	e := cgi.MapError(err)
	switch e.Code {
	case "BACKUP_STAGE_FAILED", "BACKUP_APPLY_FAILED", "BACKUP_APPLIED_MANIFEST_INCOMPLETE":
		e.Status = 500
	case "BACKUP_INVALID":
		e.Message = "Резервная копия не прошла проверку: " + e.Message + "."
	}
	cgi.WriteErr(c.W, e)
}

func (a *area) backupParts() []compat.BackupPart {
	if a.parts != nil {
		return a.parts()
	}
	return compat.BackupParts()
}

// backupLock takes the global fence for a backup step.
func (a *area) backupLock(action string) (compat.Release, error) {
	if a.page.running() {
		return nil, errBackupBusy
	}
	rel, ok := a.d.Lock.TryAcquire("system", "system:"+action, "")
	if !ok {
		return nil, errBackupBusy
	}
	return rel, nil
}

func rawString(m map[string]json.RawMessage, k string) (string, bool) {
	raw, ok := m[k]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func rawBool(m map[string]json.RawMessage, k string) (bool, bool) {
	raw, ok := m[k]
	if !ok {
		return false, false
	}
	var b bool
	if json.Unmarshal(raw, &b) != nil {
		return false, false
	}
	return b, true
}

// ---------- export ----------

func (a *area) collectBackup(ctx context.Context) (backupPayload, error) {
	p := backupPayload{Format: backupFormat, SchemaVersion: backupSchema, BROrayVersion: app.Version,
		CreatedAt: compat.StampUTC(a.now()), Parts: map[string]json.RawMessage{}}
	for _, part := range a.backupParts() {
		if part.Export == nil {
			continue
		}
		v, err := part.Export(ctx)
		if err != nil {
			return p, fmt.Errorf("BACKUP_EXPORT_FAILED: раздел %s не прочитан: %v", part.Name, err)
		}
		if v == nil {
			continue
		}
		b, err := marshalCompact(v)
		if err != nil {
			return p, err
		}
		p.Parts[part.Name] = b
	}
	return p, nil
}

func (a *area) backupExportHandler(c *cgi.Req) {
	m, ok := jsonObject(c.Body())
	if !ok {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_JSON", "Тело запроса содержит некорректный JSON."))
		return
	}
	encrypt, ok := rawBool(m, "encrypt")
	if !ok {
		cgi.WriteErr(c.W, cgi.E(400, "BACKUP_OPTIONS_INVALID", "Не указано, шифровать ли резервную копию."))
		return
	}
	password := ""
	if encrypt {
		password, _ = rawString(m, "password")
		if !validBackupPassword(password) {
			cgi.WriteErr(c.W, cgi.E(400, "BACKUP_PASSWORD_INVALID", fmt.Sprintf("Пароль должен содержать от %d до %d символов.", backupMinPassword, backupMaxPassword)))
			return
		}
	} else if ack, _ := rawBool(m, "acknowledgeUnencrypted"); !ack {
		cgi.WriteErr(c.W, cgi.E(400, "BACKUP_UNENCRYPTED_NOT_CONFIRMED", "Подтвердите, что файл без шифрования будет содержать ключи серверов и адреса подписок."))
		return
	}
	release, err := a.backupLock("backup-export")
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	payload, err := a.collectBackup(c.R.Context())
	release() // the key derivation below needs no fence
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	file, err := encodeBackupFile(payload, password)
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	if len(file) > backupMaxFileBytes {
		writeBackupErr(c, errBackupTooLarge)
		return
	}
	a.d.Log.Printf("backup: export encrypted=%v parts=%d bytes=%d", encrypt, len(payload.Parts), len(file))
	name := "broray-settings-" + a.now().UTC().Format("20060102-150405") + ".json"
	cgi.SetJSONHeaders(c.W)
	c.W.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	c.W.WriteHeader(http.StatusOK)
	_, _ = c.W.Write(file)
}

// ---------- preview / import ----------

type stagedImport struct {
	payload   backupPayload
	encrypted bool
	fileSHA   string
	stage     *compat.BackupStage
	plans     map[string]compat.BackupPlan
	changes   []compat.BackupChange
	followUps []string
	notes     []string
	token     string
}

func (a *area) backupDir(parts ...string) string {
	return a.d.Path(append([]string{"backup"}, parts...)...)
}

// clearStages removes only unreferenced staging roots. An interrupted import
// may still need its stage to restore the settings that were live before it.
func (a *area) clearStages() error {
	referenced, err := a.incompleteStageRefs()
	if err != nil {
		return err
	}
	m, _ := filepath.Glob(a.backupDir(".stage-*"))
	for _, p := range m {
		if !reBackupStageID.MatchString(filepath.Base(p)) {
			continue
		}
		if referenced[filepath.Base(p)] {
			continue
		}
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() {
			continue
		}
		owner, err := os.ReadFile(filepath.Join(p, backupStageOwnerFile))
		if err == nil && string(owner) == backupStageOwnerText {
			_ = os.RemoveAll(p)
		}
	}
	return nil
}

// readImportRequest parses {file,password[,confirmToken]}.
func readImportRequest(c *cgi.Req) (file []byte, password, token string, err error) {
	m, ok := jsonObject(c.Body())
	if !ok {
		return nil, "", "", berr(400, "INVALID_JSON", "Тело запроса содержит некорректный JSON.")
	}
	s, ok := rawString(m, "file")
	if !ok || strings.TrimSpace(s) == "" {
		return nil, "", "", berr(400, "BACKUP_FILE_REQUIRED", "Выберите файл резервной копии.")
	}
	if len(s) > backupMaxFileBytes {
		return nil, "", "", errBackupTooLarge
	}
	password, _ = rawString(m, "password")
	if len(password) > 4*backupMaxPassword {
		return nil, "", "", errBackupDecrypt
	}
	token, _ = rawString(m, "confirmToken")
	return []byte(s), password, token, nil
}

// stageImport stages previously decoded data; the caller removes the stage
// dir. The fence must be held for staging, but not for key derivation.
// backupAllocDir reserves one generated backup name without reusing an existing
// path. A failed sync leaves the new directory for inspection; no live path has
// changed at this point.
func backupAllocDir(parent, id string, syncDir func(string) error) (string, error) {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || syncDir == nil {
		return "", fmt.Errorf("invalid backup allocation")
	}
	if err := platform.MkdirAllDurable(parent, 0o700); err != nil {
		return "", err
	}
	dir := filepath.Join(parent, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	if err := syncDir(parent); err != nil {
		return "", fmt.Errorf("backup allocation parent sync %s: %w", parent, err)
	}
	return dir, nil
}

func createBackupStageDir(parent, id string) (string, error) {
	return createBackupStageDirWithSync(parent, id, platform.SyncDir)
}

func createBackupStageDirWithSync(parent, id string, syncDir func(string) error) (string, error) {
	dir, err := backupAllocDir(parent, id, syncDir)
	if err != nil {
		return "", err
	}
	if err := platform.WriteFileAtomic(filepath.Join(dir, backupStageOwnerFile), []byte(backupStageOwnerText), 0o600); err != nil {
		return "", fmt.Errorf("backup stage owner marker at %s: %w", dir, err)
	}
	return dir, nil
}
func (a *area) stageImport(ctx context.Context, file []byte, payload backupPayload, encrypted bool) (*stagedImport, error) {
	known := map[string]bool{}
	parts := a.backupParts()
	for _, p := range parts {
		known[p.Name] = true
	}
	for name := range payload.Parts {
		if !known[name] {
			return nil, berr(400, "BACKUP_PART_UNSUPPORTED", "Раздел «"+name+"» резервной копии не поддерживается этой версией BROray.")
		}
	}
	if err := a.clearStages(); err != nil {
		return nil, err
	}
	stageDir, err := createBackupStageDir(a.backupDir(), ".stage-"+compat.RandHex(8))
	if err != nil {
		return nil, err
	}
	stage, err := compat.NewBackupStage(a.d, stageDir)
	if err != nil {
		_ = os.RemoveAll(stageDir)
		return nil, err
	}
	si := &stagedImport{payload: payload, encrypted: encrypted, fileSHA: compat.SHA256Hex(file), stage: stage, plans: map[string]compat.BackupPlan{}}
	for _, p := range parts {
		if p.Stage == nil {
			continue
		}
		plan, err := p.Stage(ctx, payload.Parts[p.Name], stage)
		if err != nil {
			_ = os.RemoveAll(stage.Dir)
			return nil, err
		}
		si.plans[p.Name] = plan
		si.changes = append(si.changes, plan.Changes...)
		si.followUps = append(si.followUps, plan.FollowUps...)
		si.notes = append(si.notes, plan.Notes...)
	}
	if si.changes == nil {
		si.changes = []compat.BackupChange{}
	}
	tok, err := compat.CanonicalSHA256(map[string]any{"file": si.fileSHA, "changes": si.changes, "followUps": si.followUps, "paths": stage.Paths()})
	if err != nil {
		_ = os.RemoveAll(stage.Dir)
		return nil, err
	}
	si.token = tok
	return si, nil
}

var summaryKey = map[string]string{"server": "servers", "subscription": "subscriptions", "customRoutes": "customRoutes", "setting": "settings"}

func (si *stagedImport) view() map[string]any {
	summary := map[string]any{}
	counts := map[string]map[string]int{}
	for _, k := range []string{"servers", "subscriptions", "customRoutes", "settings"} {
		counts[k] = map[string]int{"add": 0, "replace": 0, "unchanged": 0, "remove": 0, "keep": 0}
	}
	has := false
	for _, ch := range si.changes {
		k := summaryKey[ch.Kind]
		if k == "" {
			k = "settings"
		}
		counts[k][ch.Action]++
		if ch.Action == "add" || ch.Action == "replace" || ch.Action == "remove" {
			has = true
		}
	}
	for k, v := range counts {
		summary[k] = v
	}
	parts := make([]string, 0, len(si.payload.Parts))
	for name := range si.payload.Parts {
		parts = append(parts, name)
	}
	sort.Strings(parts)
	follow, notes := si.followUps, si.notes
	if follow == nil {
		follow = []string{}
	}
	if notes == nil {
		notes = []string{}
	}
	return map[string]any{"schemaVersion": 1,
		"file": map[string]any{"encrypted": si.encrypted, "brorayVersion": si.payload.BROrayVersion, "createdAt": si.payload.CreatedAt,
			"sha256": si.fileSHA, "parts": parts},
		"summary": summary, "changes": si.changes, "hasChanges": has, "followUps": follow, "notes": notes, "confirmToken": si.token}
}

func (a *area) backupPreviewHandler(c *cgi.Req) {
	file, password, _, err := readImportRequest(c)
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	payload, encrypted, err := decodeBackupFile(file, password)
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	release, err := a.backupLock("backup-preview")
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	defer release()
	si, err := a.stageImport(c.R.Context(), file, payload, encrypted)
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	_ = os.RemoveAll(si.stage.Dir)
	cgi.WriteOK(c.W, si.view())
}

type copyManifest struct {
	SchemaVersion   int                `json:"schemaVersion"`
	Kind            string             `json:"kind"` // pre-import | pre-undo
	StageRel        string             `json:"stageRel,omitempty"`
	Recovered       bool               `json:"recovered,omitempty"`
	CreatedAt       string             `json:"createdAt"`
	BROrayVersion   string             `json:"brorayVersion"`
	BackupCreatedAt string             `json:"backupCreatedAt,omitempty"`
	BackupSHA256    string             `json:"backupSha256,omitempty"`
	Complete        bool               `json:"complete"` // false: written before the swap finished
	Paths           []compat.SwapEntry `json:"paths"`
}

// newCopyDir allocates data/backup/<prefix>-<UTC>-<hex>.
func (a *area) newCopyDir(prefix string) (string, string, error) {
	id := prefix + "-" + a.now().UTC().Format("20060102-150405") + "-" + compat.RandHex(4)
	dir, err := createBackupCopyDir(a.backupDir(), id)
	if err != nil {
		return "", "", err
	}
	return id, dir, nil
}

func createBackupCopyDir(parent, id string) (string, error) {
	return createBackupCopyDirWithSync(parent, id, platform.SyncDir)
}

func createBackupCopyDirWithSync(parent, id string, syncDir func(string) error) (string, error) {
	dir, err := backupAllocDir(parent, id, syncDir)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(filepath.Join(dir, backupCopyRoot), 0o700); err != nil {
		return "", err
	}
	if err := syncDir(dir); err != nil {
		return "", fmt.Errorf("backup copy root sync %s: %w", dir, err)
	}
	return dir, nil
}

// pruneCopies keeps the newest backupKeep copies of a prefix.
func (a *area) pruneCopies(prefix string) {
	m, _ := filepath.Glob(a.backupDir(prefix + "-*"))
	owned := make([]string, 0, len(m))
	for _, p := range m {
		if a.ownedCopyForPrune(prefix, p) {
			owned = append(owned, p)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(owned)))
	for i, p := range owned {
		if i >= backupKeep {
			_ = os.RemoveAll(p)
		}
	}
}

func (a *area) ownedCopyForPrune(prefix, p string) bool {
	id := filepath.Base(p)
	if !reBackupPruneCopyID.MatchString(id) || !strings.HasPrefix(id, prefix+"-") {
		return false
	}
	info, err := os.Lstat(p)
	if err != nil || !info.IsDir() {
		return false
	}
	wantKind := ""
	switch prefix {
	case "settings-before-import":
		wantKind = "pre-import"
	case "settings-before-undo":
		wantKind = "pre-undo"
	default:
		return false
	}
	var man copyManifest
	if err := compat.ReadJSON(filepath.Join(p, "manifest.json"), &man); err != nil || man.SchemaVersion != 1 || !man.Complete || man.Kind != wantKind {
		return false
	}
	for _, entry := range man.Paths {
		if !copyPathAllowed(entry.Path) || entry.SHA256 == "" {
			return false
		}
	}
	return true
}

func (a *area) recoverFailedBackupSwap(copy incompleteBackupCopy, cause error) error {
	if recErr := a.rollbackIncompleteCopy(copy); recErr != nil {
		pendingErr := a.d.Lock.MarkPending(false)
		return fmt.Errorf("BACKUP_RECOVERY_REQUIRED: откат не завершён: %w", errors.Join(cause, recErr, pendingErr))
	}
	return cause
}

// swapWithCopy swaps paths (stage → live), keeps the originals in a new copy
// dir and runs the parts' After hooks. Field-merged files (fields) are first
// rebuilt from the live file, so only the restored fields change. The fence
// is held by the caller.
func (a *area) swapWithCopy(ctx context.Context, stageRoot string, paths []string, fields map[string][]string, m copyManifest, prefix string) (string, compat.BackupPlan, error) {
	var out compat.BackupPlan
	parts := a.backupParts()
	before := map[string]any{}
	for _, p := range parts {
		if p.Before != nil {
			before[p.Name] = p.Before()
		}
	}
	for _, p := range paths {
		if f := fields[p]; len(f) > 0 {
			if err := compat.MergeBackupFields(a.d.Root.Dir, stageRoot, p, f); err != nil {
				return "", out, fmt.Errorf("BACKUP_APPLY_FAILED: не удалось применить резервную копию, прежнее состояние сохранено (%v)", err)
			}
		}
	}
	stageRel, err := a.relativeBackupStage(stageRoot, m.Kind)
	if err != nil {
		return "", out, err
	}
	m.StageRel = stageRel
	if err := platform.SyncTree(stageRoot); err != nil {
		return "", out, fmt.Errorf("BACKUP_INPUT_UNSYNCED: не удалось сохранить подготовленные файлы на диске (%w)", err)
	}
	m.Paths = nil
	seen := map[string]bool{}
	for _, p := range paths {
		if !copyPathAllowed(p) || seen[p] {
			return "", out, fmt.Errorf("BACKUP_APPLY_FAILED: некорректный путь резервной копии")
		}
		seen[p] = true
		if err := platform.SyncPathAndParents(a.d.Root.Dir, filepath.FromSlash(p)); err != nil {
			return "", out, fmt.Errorf("BACKUP_INPUT_UNSYNCED: не удалось сохранить исходный путь %s на диске (%w)", p, err)
		}
		liveExists, err := backupPathExists(filepath.Join(a.d.Root.Dir, filepath.FromSlash(p)))
		if err != nil {
			return "", out, err
		}
		stageExists, err := backupPathExists(filepath.Join(stageRoot, filepath.FromSlash(p)))
		if err != nil {
			return "", out, err
		}
		entry := compat.SwapEntry{Path: p, Existed: liveExists, Staged: stageExists, Fields: fields[p]}
		if liveExists {
			entry.OriginalSHA256, err = compat.BackupDigest(a.d.Root.Dir, p, nil)
			if err != nil {
				return "", out, err
			}
		}
		if stageExists {
			entry.StageSHA256, err = compat.BackupDigest(stageRoot, p, nil)
			if err != nil {
				return "", out, err
			}
		}
		m.Paths = append(m.Paths, entry)
	}
	id, dir, err := a.newCopyDir(prefix)
	if err != nil {
		return "", out, err
	}
	m.SchemaVersion, m.CreatedAt, m.BROrayVersion = 1, compat.StampUTC(a.now()), app.Version
	if err := compat.WriteJSON(filepath.Join(dir, "manifest.json"), m); err != nil {
		_ = os.RemoveAll(dir)
		return "", out, err
	}
	planned := append([]compat.SwapEntry(nil), m.Paths...)
	recoverFailed := func(cause error) (string, compat.BackupPlan, error) {
		m.Paths, m.Complete = planned, false
		return "", out, a.recoverFailedBackupSwap(incompleteBackupCopy{dir: dir, stageRoot: stageRoot, manifest: m}, cause)
	}
	entries, err := compat.SwapBackupPaths(a.d.Root.Dir, stageRoot, filepath.Join(dir, backupCopyRoot), paths)
	if err != nil {
		return recoverFailed(fmt.Errorf("BACKUP_APPLY_FAILED: не удалось применить резервную копию (%w)", err))
	}
	byPath := map[string]compat.SwapEntry{}
	for _, entry := range planned {
		byPath[entry.Path] = entry
	}
	for i := range entries {
		initial, ok := byPath[entries[i].Path]
		if !ok || initial.Existed != entries[i].Existed {
			return recoverFailed(fmt.Errorf("BACKUP_APPLY_FAILED: список заменённых путей изменился"))
		}
		entries[i] = initial
	}
	m.Paths = entries
	if m.Paths == nil {
		m.Paths = []compat.SwapEntry{}
	}
	// The first complete marker must already describe the live state. A crash
	// before or during After must never expose a copy whose undo digest is empty.
	for i := range m.Paths {
		sum, err := compat.BackupDigest(a.d.Root.Dir, m.Paths[i].Path, m.Paths[i].Fields)
		if err != nil {
			return recoverFailed(fmt.Errorf("BACKUP_APPLY_FAILED: контрольная сумма применённых настроек: %w", err))
		}
		m.Paths[i].SHA256 = sum
	}
	m.Complete = true
	if err := compat.WriteJSON(filepath.Join(dir, "manifest.json"), m); err != nil {
		return recoverFailed(fmt.Errorf("BACKUP_APPLY_FAILED: запись манифеста: %w", err))
	}
	for _, p := range parts {
		if p.After != nil {
			r := p.After(ctx, before[p.Name])
			out.FollowUps = append(out.FollowUps, r.FollowUps...)
			out.Notes = append(out.Notes, r.Notes...)
		}
	}
	// what the swap left live: an undo is refused once any of it changed
	// (an install or DoT apply after the import wrote state the copy lacks)
	for i := range m.Paths {
		sum, err := compat.BackupDigest(a.d.Root.Dir, m.Paths[i].Path, m.Paths[i].Fields)
		if err != nil {
			return id, out, fmt.Errorf("BACKUP_APPLIED_MANIFEST_INCOMPLETE: настройки применены, но не удалось проверить состояние копии %s (%w); повторно не применяйте резервную копию", id, err)
		}
		m.Paths[i].SHA256 = sum
	}
	if err := compat.WriteJSON(filepath.Join(dir, "manifest.json"), m); err != nil {
		return id, out, fmt.Errorf("BACKUP_APPLIED_MANIFEST_INCOMPLETE: настройки применены, но не удалось завершить запись копии %s (%w); повторно не применяйте резервную копию", id, err)
	}
	a.pruneCopies(prefix)
	return id, out, nil
}

func (a *area) backupImportHandler(c *cgi.Req) {
	file, password, token, err := readImportRequest(c)
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	if !reHex64.MatchString(token) {
		cgi.WriteErr(c.W, cgi.E(400, "BACKUP_CONFIRM_REQUIRED", "Сначала проверьте файл и подтвердите восстановление."))
		return
	}
	payload, encrypted, err := decodeBackupFile(file, password)
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	release, err := a.backupLock("backup-import")
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	defer release()
	op := a.d.Queue.StartOperation("", "unknown", "system:backup-import", "USER", "protected", "")
	op.Phase("checking")
	ctx := c.R.Context()
	si, err := a.stageImport(ctx, file, payload, encrypted)
	if err != nil {
		op.Finish(err)
		writeBackupErr(c, err)
		return
	}
	defer func() {
		refs, err := a.incompleteStageRefs()
		if err == nil && !refs[filepath.Base(si.stage.Dir)] {
			_ = os.RemoveAll(si.stage.Dir)
		}
	}()
	if si.token != token {
		op.Finish(errBackupToken)
		writeBackupErr(c, errBackupToken)
		return
	}
	op.Phase("committing")
	// the import must finish even if the browser goes away mid-request
	ctx = context.WithoutCancel(ctx)
	copyID, after, err := a.swapWithCopy(ctx, si.stage.Dir, si.stage.Paths(), si.stage.Fields(),
		copyManifest{Kind: "pre-import", BackupCreatedAt: si.payload.CreatedAt, BackupSHA256: si.fileSHA}, "settings-before-import")
	if err != nil {
		op.Finish(err)
		writeBackupErr(c, err)
		return
	}
	op.Finish(nil)
	a.d.Log.Printf("backup: import applied copy=%s paths=%d", copyID, len(si.stage.Paths()))
	out := si.view()
	out["applied"] = true
	out["preImportCopy"] = map[string]any{"id": copyID, "path": filepath.ToSlash(filepath.Join("data", "backup", copyID))}
	out["followUps"] = append(out["followUps"].([]string), after.FollowUps...)
	out["notes"] = append(out["notes"].([]string), after.Notes...)
	delete(out, "confirmToken")
	cgi.WriteOK(c.W, out)
}

var reHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ---------- pre-import copies: status / undo ----------

func (a *area) readCopy(id string) (copyManifest, bool) {
	var m copyManifest
	if !reBackupCopyID.MatchString(id) {
		return m, false
	}
	if err := compat.ReadJSON(a.backupDir(id, "manifest.json"), &m); err != nil || m.SchemaVersion != 1 || !m.Complete {
		return m, false
	}
	for _, e := range m.Paths {
		if !copyPathAllowed(e.Path) {
			return m, false
		}
	}
	return m, true
}

// copyPathAllowed: a clean relative path below data/ or config/ (the swap
// itself refuses anything else too).
func copyPathAllowed(p string) bool {
	if p == "" || p != filepath.ToSlash(filepath.Clean(p)) || strings.Contains(p, "..") || strings.HasPrefix(p, "/") {
		return false
	}
	return strings.HasPrefix(p, "data/") || strings.HasPrefix(p, "config/")
}

func (a *area) backupStatus() map[string]any {
	m, _ := filepath.Glob(a.backupDir("settings-before-import-*"))
	sort.Sort(sort.Reverse(sort.StringSlice(m)))
	copies := []any{}
	for _, p := range m {
		id := filepath.Base(p)
		if man, ok := a.readCopy(id); ok {
			copies = append(copies, map[string]any{"id": id, "createdAt": man.CreatedAt, "backupCreatedAt": man.BackupCreatedAt,
				"pathCount": len(man.Paths)})
		}
	}
	return map[string]any{"schemaVersion": 1, "preImportCopies": copies,
		"contents": []string{"servers", "subscriptions", "customRoutes", "builtinRouteSelection", "dnsOverTls", "serverAutoSwitch", "settings"}}
}

// changedSinceImport returns the first swapped path whose live state is no
// longer what the import left ("" if none). A copy without recorded digests
// (interrupted import, older build) counts as changed: undoing it blindly
// could bring back stale Keenetic ownership state.
func (a *area) changedSinceImport(man copyManifest) string {
	for _, e := range man.Paths {
		if e.SHA256 == "" {
			return e.Path
		}
		sum, err := compat.BackupDigest(a.d.Root.Dir, e.Path, e.Fields)
		if err != nil || sum != e.SHA256 {
			return e.Path
		}
	}
	return ""
}

func (a *area) backupUndoHandler(c *cgi.Req) {
	m, ok := jsonObject(c.Body())
	if !ok {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_JSON", "Тело запроса содержит некорректный JSON."))
		return
	}
	id, _ := rawString(m, "copyId")
	if !reBackupCopyID.MatchString(id) {
		cgi.WriteErr(c.W, cgi.E(400, "BACKUP_COPY_INVALID", "Некорректный идентификатор копии."))
		return
	}
	release, err := a.backupLock("backup-undo")
	if err != nil {
		writeBackupErr(c, err)
		return
	}
	defer release()
	man, ok := a.readCopy(id)
	if !ok {
		cgi.WriteErr(c.W, cgi.E(404, "BACKUP_COPY_NOT_FOUND", "Копия настроек до восстановления не найдена или повреждена."))
		return
	}
	if changed := a.changedSinceImport(man); changed != "" {
		a.d.Log.Printf("backup: undo of %s refused: %s changed after the import", id, changed)
		writeBackupErr(c, errBackupUndoChanged)
		return
	}
	op := a.d.Queue.StartOperation("", "unknown", "system:backup-undo", "USER", "protected", "")
	op.Phase("committing")
	paths := make([]string, 0, len(man.Paths))
	fields := map[string][]string{}
	for _, e := range man.Paths {
		paths = append(paths, e.Path)
		if len(e.Fields) > 0 {
			fields[e.Path] = e.Fields
		}
	}
	// a path that did not exist before the import is absent in the copy, so the swap removes it
	src := a.backupDir(id, backupCopyRoot)
	undoID, after, err := a.swapWithCopy(context.WithoutCancel(c.R.Context()), src, paths, fields, copyManifest{Kind: "pre-undo"}, "settings-before-undo")
	if err != nil {
		op.Finish(err)
		writeBackupErr(c, err)
		return
	}
	_ = os.RemoveAll(a.backupDir(id)) // its files are live again
	op.Finish(nil)
	a.d.Log.Printf("backup: undo of %s applied, previous state kept in %s", id, undoID)
	follow, notes := after.FollowUps, after.Notes
	if follow == nil {
		follow = []string{}
	}
	if notes == nil {
		notes = []string{}
	}
	cgi.WriteOK(c.W, map[string]any{"undone": true, "copyId": id, "followUps": follow, "notes": notes})
}

// ---------- the "settings" part: config.v1.json (logLevel, health) ----------

type settingsBackup struct {
	LogLevel *string         `json:"logLevel"`
	Health   json.RawMessage `json:"health"`
}

func (a *area) settingsPart() compat.BackupPart {
	return compat.BackupPart{
		Name: "settings", Order: 50,
		Export: func(ctx context.Context) (any, error) {
			cfg, err := platform.LoadConfig(a.d.Root)
			if err != nil {
				return nil, err
			}
			return map[string]any{"logLevel": cfg.LogLevel, "health": cfg.Health}, nil
		},
		Stage: a.settingsStage,
	}
}

var reSettingsServerID = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]{0,127}$`)

func (a *area) settingsStage(ctx context.Context, raw json.RawMessage, st *compat.BackupStage) (compat.BackupPlan, error) {
	var plan compat.BackupPlan
	var in settingsBackup
	if len(raw) == 0 || string(raw) == "null" {
		return plan, nil
	}
	bad := func(what string) (compat.BackupPlan, error) {
		return plan, fmt.Errorf("BACKUP_INVALID: параметры BROray некорректны: %s", what)
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return bad("раздел повреждён")
	}
	live, err := platform.LoadConfig(a.d.Root)
	if err != nil {
		return plan, fmt.Errorf("BACKUP_STAGE_FAILED: текущий config.v1.json не прочитан")
	}
	next := live
	if in.LogLevel != nil {
		switch *in.LogLevel {
		case "debug", "info", "warning", "error", "none":
			next.LogLevel = *in.LogLevel
		default:
			return bad("logLevel")
		}
	}
	if len(in.Health) > 0 && string(in.Health) != "null" {
		h := live.Health
		h.ProbeURLs = nil
		if err := json.Unmarshal(in.Health, &h); err != nil {
			return bad("health")
		}
		if len(h.ProbeURLs) < 1 || len(h.ProbeURLs) > 8 {
			return bad("probeUrls")
		}
		for _, u := range h.ProbeURLs {
			pu, err := url.Parse(u)
			if err != nil || len(u) > 512 || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" || pu.User != nil {
				return bad("probeUrls")
			}
		}
		switch {
		case h.IntervalSec < 5 || h.IntervalSec > 3600:
			return bad("intervalSec")
		case h.FailThreshold < 1 || h.FailThreshold > 10:
			return bad("failureThreshold")
		case h.CooldownMin < 1 || h.CooldownMin > 120:
			return bad("cooldownMinutes")
		case h.SelectionRule != "best-quality" && h.SelectionRule != "lowest-ping" && h.SelectionRule != "preferred":
			return bad("selectionRule")
		case h.PreferredID != "" && !reSettingsServerID.MatchString(h.PreferredID):
			return bad("preferredServerId")
		}
		next.Health = h
	}
	x, _ := compat.Canonical(live)
	y, _ := compat.Canonical(next)
	if string(x) == string(y) {
		plan.Add("setting", "config", "Параметры BROray (журнал Xray, проверка соединения)", "unchanged", "")
		return plan, nil
	}
	plan.Add("setting", "config", "Параметры BROray (журнал Xray, проверка соединения)", "replace", "")
	if err := st.Take("config/config.v1.json"); err != nil {
		return plan, err
	}
	if err := os.MkdirAll(st.St.Root.Config(), 0o700); err != nil {
		return plan, err
	}
	if err := platform.SaveConfig(st.St.Root, next); err != nil {
		return plan, err
	}
	plan.FollowUps = append(plan.FollowUps, "Параметры BROray из config.v1.json вступят в силу после перезапуска BROray.")
	return plan, nil
}
