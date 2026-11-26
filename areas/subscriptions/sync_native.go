package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"golang.org/x/sys/unix"
)

type refreshCatalogPlan struct {
	members                []deletionMember
	active                 deletionImage
	activeID, previousName string
	final                  []servers.Record
	dpiChanged             []servers.Record
	removed, changed       []string
	reapply                bool
	result                 *syncResult
}

func (s *svc) planNativeRefresh(subID string, stage []servers.Record, enabled bool, policy string) (*refreshCatalogPlan, error) {
	if policy != "replace" && policy != "retain-unmatched" {
		return nil, errors.New("invalid refresh policy")
	}
	members, err := s.deletionMembers(subID)
	if err != nil {
		return nil, err
	}
	active, err := readDeletionImage(servers.ActiveFile(s.d), true)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(active.raw))
	if id != "" && !servers.ValidID(id) {
		return nil, errors.New("invalid active identity")
	}
	p := &refreshCatalogPlan{members: members, active: active, activeID: id, result: &syncResult{Accepted: len(stage), Policy: policy, Impact: "none", Warnings: []string{}}}
	olds := make([]oldRow, 0, len(members))
	oldByID := map[string]srvFile{}
	oldByKey := map[string]oldRow{}
	for _, m := range members {
		if _, dup := oldByID[m.id]; dup {
			return nil, errors.New("duplicate source identity across catalog directories")
		}
		rec, ok := decodeObject(m.record.raw)
		if !ok {
			return nil, errors.New("invalid old source record")
		}
		key, err := servers.CanonicalKey(rec)
		if err != nil || key == "" {
			return nil, errors.New("unproven old source identity")
		}
		side := "live"
		if filepath.Dir(m.record.path) == s.disabledDir(subID) {
			side = "disabled"
		}
		f := srvFile{id: m.id, path: m.record.path, rec: rec, raw: m.record.raw}
		o := oldRow{key: key, hash: nodeHash(rec), side: side, file: f}
		olds = append(olds, o)
		oldByID[m.id] = f
		if _, ok := oldByKey[key]; !ok {
			oldByKey[key] = o
		}
		if m.id == id {
			if side != "live" || !enabled {
				return nil, fail("ACTIVE_SERVER_CONFLICT", "Активный сервер не может быть перенесён в выключенный источник.")
			}
			p.previousName = nameOrID(rec)
		}
	}
	news, code := s.remap(olds, stage)
	if code != "" {
		return nil, fail(code, errMap)
	}
	finalKeys := map[string]bool{}
	ids := map[string]bool{}
	for _, n := range news {
		nid, _ := n.rec["id"].(string)
		if !servers.ValidID(nid) || ids[nid] || !belongsTo(n.rec, subID) {
			return nil, errors.New("ambiguous refreshed identity")
		}
		ids[nid] = true
		finalKeys[n.matchKey] = true
		for _, k := range n.previousKeys {
			finalKeys[k] = true
		}
		p.final = append(p.final, n.rec)
		if o, ok := oldByKey[n.matchKey]; !ok {
			p.result.Added++
		} else if o.hash == nodeHash(n.rec) {
			p.result.Unchanged++
		} else {
			p.result.Updated++
		}
	}
	if policy == "retain-unmatched" {
		seen := map[string]bool{}
		for _, o := range olds {
			if seen[o.key] {
				return nil, errors.New("ambiguous partial source identity")
			}
			seen[o.key] = true
			if finalKeys[o.key] {
				continue
			}
			if ids[o.file.id] {
				return nil, errors.New("partial identity collision")
			}
			ids[o.file.id] = true
			p.final = append(p.final, o.file.rec)
			p.result.Retained++
			if len(p.final) > maxNodes {
				return nil, fail("PARTIAL_UPDATE_LIMIT", "Не удалось безопасно объединить частичный ответ с прежними серверами. Прежний каталог сохранён.")
			}
		}
	}
	sort.SliceStable(p.final, func(i, j int) bool { return jqRaw(p.final[i]["id"]) < jqRaw(p.final[j]["id"]) })
	finalByID := map[string]servers.Record{}
	for _, r := range p.final {
		if err := servers.Validate(r, jqRaw(r["id"])+".json"); err != nil {
			return nil, err
		}
		finalByID[jqRaw(r["id"])] = r
		managed, err := servers.ManagedDPI(r)
		if err != nil {
			return nil, err
		}
		if previous, exists := oldByID[jqRaw(r["id"])]; managed && exists && configHash(previous.rec) != configHash(r) {
			p.dpiChanged = append(p.dpiChanged, r)
		}
	}
	for _, o := range olds {
		next, kept := finalByID[o.file.id]
		if !kept {
			p.result.Removed++
		}
		removed := !kept || (!enabled && o.side == "live")
		settingsChanged := kept && configHash(o.file.rec) != configHash(next)
		if removed {
			p.removed = append(p.removed, o.file.id)
		}
		if removed || settingsChanged {
			p.changed = append(p.changed, o.file.id)
		}
		if o.file.id == id {
			if !kept {
				p.result.Impact = "removed-replaced"
			} else if settingsChanged || nameOrID(o.file.rec) != nameOrID(next) {
				p.reapply = true
				p.result.Impact = "display-name-changed"
				if settingsChanged {
					p.result.Impact = "configuration-changed"
				}
			}
		}
	}
	sort.Strings(p.removed)
	sort.Strings(p.changed)
	p.result.Total = len(p.final)
	if p.result.Retained > 0 {
		p.result.Warnings = append(p.result.Warnings, fmt.Sprintf("Сохранено прежних серверов: %d. Ответ разобран не полностью; их отсутствие не подтверждено.", p.result.Retained))
	}
	return p, nil
}

// syncCatalogCommit owns strict catalog bytes. Protected refresh delegates runtime
// selection to the server service. Legacy refresh can only change proven inactive,
// non-reserve members and never borrows or fabricates runtime authority.
func (s *svc) syncCatalogCommit(ctx context.Context, subID string, stage []servers.Record, enabled bool, policy string, apply *pendingApplyObservation, op *compat.Operation) (*syncResult, error) {
	failRefresh := func(err error) error {
		var existing *subErr
		if errors.As(err, &existing) {
			return err
		}
		if errors.Is(err, compat.ErrXrayCandidateRejected) {
			return errors.Join(fail("ACTIVE_SERVER_CONFLICT", "Новая конфигурация активного сервера не прошла проверку Xray. Обновление отменено."), err)
		}
		return errors.Join(fail("SERVER_SYNC_ERROR", "Обновление серверов не завершено."), err)
	}
	protected := compat.FenceHeld(ctx)
	if ctx.Err() != nil {
		return nil, failRefresh(errors.New("refresh fence required"))
	}
	var p *refreshCatalogPlan
	var err error
	if apply != nil {
		p, err = s.planPendingProfileApply(ctx, subID, apply)
	} else {
		p, err = s.planNativeRefresh(subID, stage, enabled, policy)
	}
	if err != nil {
		return nil, failRefresh(err)
	}
	var pendingProfile *pendingProfileWrite
	if apply != nil {
		pendingProfile = &pendingProfileWrite{before: apply.binding.image, source: apply.binding.source}
	} else if protected {
		pendingProfile, err = s.planPendingActiveProfile(subID, p)
		if err != nil {
			return nil, failRefresh(err)
		}
	}
	// Changed managed profiles require a protected native validation provider.
	// Missing service remains a safe refusal before any catalog mutation.
	if len(p.dpiChanged) != 0 && (!protected || s.d.Svc.ServersValidateSourceDPIProposals == nil) {
		return nil, fail("DPI_REVALIDATION_REQUIRED", "Параметры сервера с DPI изменились. Требуется проверка нового подключения; прежний каталог сохранён.")
	}
	var dpiValidation compat.DPIValidation
	defer func() {
		if dpiValidation != nil {
			_ = dpiValidation.Close()
		}
	}()
	allIDs := make([]string, 0, len(p.members)+len(p.final))
	for _, m := range p.members {
		allIDs = append(allIDs, m.id)
	}
	for _, rec := range p.final {
		allIDs = append(allIDs, jqRaw(rec["id"]))
	}
	allowed := func() bool {
		if ctx.Err() != nil || (dpiValidation != nil && dpiValidation.Check(ctx) != nil) ||
			(pendingProfile != nil && pendingProfile.source.path != "" && !pendingProfile.source.unchanged()) {
			return false
		}
		if protected {
			return compat.FenceHeld(ctx)
		}
		for _, id := range allIDs {
			if id == p.activeID {
				return false
			}
		}
		return servers.CheckSourceRemovalLegacy(ctx, s.d, allIDs) == nil
	}
	if !allowed() {
		return nil, fail("SOURCE_REFRESH_QUEUE_REQUIRED", "Для обновления этого источника требуется защищённая операция. Обновите страницу и повторите действие.")
	}
	target := servers.ServersDir(s.d)
	if !enabled {
		target = s.disabledDir(subID)
	}
	if err := platform.MkdirAllDurable(target, 0700); err != nil {
		return nil, failRefresh(err)
	}
	type writeEntry struct {
		before, after deletionImage
		data          []byte
	}
	var writes []*writeEntry
	expected := map[string]deletionImage{}
	for _, m := range p.members {
		expected[m.record.path] = m.record
	}
	for _, rec := range p.final {
		id := jqRaw(rec["id"])
		path := filepath.Join(target, id+".json")
		image, err := readDeletionImage(path, true)
		if err != nil {
			return nil, failRefresh(err)
		}
		if image.info != nil {
			old, ok := decodeObject(image.raw)
			if !ok || old["id"] != id || !belongsTo(old, subID) {
				return nil, failRefresh(errors.New("foreign refresh destination"))
			}
			if sameIgnoringUpdatedAt(old, rec) {
				expected[path] = image
				continue
			}
		}
		data, err := servers.Marshal(rec)
		if err != nil {
			return nil, failRefresh(err)
		}
		writes = append(writes, &writeEntry{before: image, data: data})
		expected[path] = image
	}
	if pendingProfile != nil {
		before := pendingProfile.before
		expected[before.path] = before
		if !bytes.Equal(before.raw, pendingProfile.data) {
			writes = append(writes, &writeEntry{before: before, data: pendingProfile.data})
		}
	}
	// The complete original snapshot is rechecked after planning and before any
	// backup/owner/catalog mutation; permissive display scans are never used.
	verify := func(active deletionImage) bool {
		if !allowed() || !active.unchanged() {
			return false
		}
		for _, image := range expected {
			if !image.unchanged() {
				return false
			}
		}
		for _, m := range p.members {
			if !m.quality.unchanged() {
				return false
			}
		}
		members, e := s.deletionMembers(subID)
		if e != nil {
			return false
		}
		for _, m := range members {
			image, ok := expected[m.record.path]
			if !ok || image.info == nil || !bytes.Equal(image.raw, m.record.raw) {
				return false
			}
		}
		return true
	}
	if !verify(p.active) {
		return nil, failRefresh(errors.New("source snapshot changed"))
	}
	// Validate precisely the bytes staged above, before creating any recovery
	// marker or preparing reserve/runtime changes. The lease remains a dependency
	// of allowed() throughout the existing strict catalog transaction.
	if len(p.dpiChanged) != 0 {
		proposals := make([]compat.DPIProposal, 0, len(p.dpiChanged))
		for _, rec := range p.dpiChanged {
			id := jqRaw(rec["id"])
			var member *deletionMember
			for i := range p.members {
				if p.members[i].id == id {
					member = &p.members[i]
					break
				}
			}
			var payload []byte
			for _, w := range writes {
				if filepath.Base(w.before.path) == id+".json" {
					payload = w.data
					break
				}
			}
			if member == nil || len(payload) == 0 {
				return nil, failRefresh(errors.New("DPI proposal has no exact catalog write"))
			}
			proposals = append(proposals, compat.DPIProposal{Path: member.record.path, PreviousSHA256: compat.SHA256Hex(member.record.raw), Record: payload})
		}
		dpiValidation, err = s.d.Svc.ValidateSourceDPIProposals(ctx, subID, proposals)
		if err != nil {
			if errors.Is(err, compat.ErrDPIProposalUnhealthy) {
				return nil, errors.Join(fail("DPI_CONNECTION_FAILED", "Новое подключение с DPI не прошло проверку. Прежний каталог сохранён."), err)
			}
			return nil, failRefresh(err)
		}
		if dpiValidation == nil || dpiValidation.Check(ctx) != nil || !verify(p.active) {
			return nil, failRefresh(errors.New("DPI validation authority changed"))
		}
	}
	if apply != nil && apply.schedule != nil {
		if err := s.claimPendingProfileWindow(ctx, subID, apply); err != nil {
			return nil, failRefresh(err)
		}
	}
	tmp, err := platform.OpenDirNoFollow(s.d.Root.Tmp())
	if err != nil {
		return nil, failRefresh(err)
	}
	defer tmp.Close()
	name, err := platform.MkdirTempAtNoFollow(tmp, "server-subscription-sync.", 0700)
	if err != nil {
		return nil, failRefresh(err)
	}
	work, err := platform.OpenDirAtNoFollow(tmp, name)
	if err != nil {
		return nil, failRefresh(err)
	}
	defer work.Close()
	workPath := filepath.Join(s.d.Root.Tmp(), name)
	type backupImage struct {
		Path   string
		Raw    []byte
		Exists bool
	}
	backup := struct {
		SchemaVersion    int
		Source, ActiveID string
		Images, Proposed []backupImage
	}{SchemaVersion: 1, Source: subID, ActiveID: p.activeID}
	backup.Images = append(backup.Images, backupImage{p.active.path, p.active.raw, p.active.info != nil})
	for _, m := range p.members {
		backup.Images = append(backup.Images, backupImage{m.record.path, m.record.raw, true}, backupImage{m.quality.path, m.quality.raw, m.quality.info != nil})
	}
	for _, w := range writes {
		if w.before.info == nil || (pendingProfile != nil && w.before.path == pendingProfile.before.path) {
			backup.Images = append(backup.Images, backupImage{w.before.path, w.before.raw, w.before.info != nil})
		}
	}
	for _, w := range writes {
		backup.Proposed = append(backup.Proposed, backupImage{w.before.path, w.data, w.data != nil})
	}
	data, err := json.Marshal(backup)
	if err != nil {
		return nil, failRefresh(err)
	}
	if apply == nil {
		err = platform.WriteNewRegularAtNoFollow(work, "backup.json", data, 0600)
	}
	if err != nil {
		return nil, failRefresh(err)
	}
	recoveryAuthority := ""
	var recipe deletionImage
	if apply != nil {
		recoveryBytes, err := s.prepareProfileRecovery(subID, apply)
		if err != nil {
			return nil, failRefresh(err)
		}
		if err := platform.WriteFileAtomicNoFollow(filepath.Join(workPath, profileRecoveryFile), recoveryBytes, 0600); err != nil {
			return nil, failRefresh(err)
		}
		recipe, err = readDeletionImageLimit(filepath.Join(workPath, profileRecoveryFile), false, 3<<20)
		if err != nil || !bytes.Equal(recipe.raw, recoveryBytes) {
			return nil, failRefresh(errors.New("profile recovery preparation unconfirmed"))
		}
		recoveryAuthority = compat.SHA256Hex(recoveryBytes)
	}
	markerBytes := []byte(subID + "\n")
	if apply != nil {
		err = platform.WriteFileAtomicNoFollow(filepath.Join(workPath, sourceRecoveryMarker), markerBytes, 0600)
	} else {
		err = platform.WriteNewRegularAtNoFollow(work, sourceRecoveryMarker, markerBytes, 0600)
	}
	if err != nil {
		return nil, failRefresh(err)
	}
	pending := func(cause error) (*syncResult, error) {
		return nil, errors.Join(fail("SERVER_SYNC_ROLLBACK_FAILED", "Обновление требует восстановления. Резервная копия сохранена: "+workPath), compat.ErrReserveTransactionPending, cause)
	}
	sameWork := func() bool {
		current, e := platform.OpenDirAtNoFollow(tmp, name)
		if e != nil {
			return false
		}
		defer current.Close()
		a, e := current.Stat()
		b, be := work.Stat()
		return e == nil && be == nil && os.SameFile(a, b)
	}
	readWork := func(name string, want []byte) (os.FileInfo, error) {
		fd, e := unix.Openat(int(work.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if e != nil {
			return nil, e
		}
		f := os.NewFile(uintptr(fd), name)
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || info.Size() != int64(len(want)) {
			return nil, errors.New("refresh recovery image shape changed")
		}
		got, e := io.ReadAll(io.LimitReader(f, int64(len(want))+1))
		if e != nil || !bytes.Equal(got, want) {
			return nil, errors.Join(e, errors.New("refresh recovery bytes changed"))
		}
		after, e := f.Stat()
		if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return nil, errors.Join(e, errors.New("refresh recovery image drift"))
		}
		return info, nil
	}
	var backupInfo os.FileInfo
	if apply == nil {
		backupInfo, err = readWork("backup.json", data)
		if err != nil {
			return pending(err)
		}
	}
	markerInfo, err := readWork(sourceRecoveryMarker, markerBytes)
	if err != nil {
		return pending(err)
	}
	cleanup := func() error {
		if !sameWork() {
			return errors.New("refresh workspace changed")
		}
		currentMarker, e := readWork(sourceRecoveryMarker, markerBytes)
		if e != nil || !os.SameFile(markerInfo, currentMarker) {
			return errors.Join(e, errors.New("refresh marker replaced"))
		}
		if apply != nil {
			if !recipe.unchanged() {
				return errors.New("profile recovery record replaced")
			}
			// Retire the catalog marker before discarding the native commit.
			// If startup finds only a committed native journal it can safely
			// acknowledge it, because the catalog was verified first.
			if e := unix.Unlinkat(int(work.Fd()), sourceRecoveryMarker, 0); e != nil {
				return e
			}
			if e := work.Sync(); e != nil {
				return e
			}
			if sameWork() {
				s.cleanupRecoveredProfileArtifacts(recipe)
				_ = unix.Unlinkat(int(tmp.Fd()), name, unix.AT_REMOVEDIR)
			}
			return tmp.Sync()
		}
		currentBackup, e := readWork("backup.json", data)
		if e != nil || !os.SameFile(backupInfo, currentBackup) {
			return errors.Join(e, errors.New("refresh backup replaced"))
		}
		if e := unix.Unlinkat(int(work.Fd()), "backup.json", 0); e != nil {
			return e
		}
		if e := unix.Unlinkat(int(work.Fd()), "rollback-required", 0); e != nil {
			restored := platform.WriteNewRegularAtNoFollow(work, "backup.json", data, 0600)
			return errors.Join(e, restored)
		}
		if e := work.Sync(); e != nil {
			return e
		}
		if !sameWork() {
			return errors.New("refresh workspace changed at completion")
		}
		if e := unix.Unlinkat(int(tmp.Fd()), name, unix.AT_REMOVEDIR); e != nil {
			s.d.Log.Printf("subscriptions: empty refresh workspace cleanup: %v", e)
		}
		return tmp.Sync()
	}

	var written []*writeEntry
	rollback := func(cause error) (*syncResult, error) {
		transaction, e := reserve.TransactionPending(context.WithoutCancel(ctx), s.d.Root)
		if e != nil || transaction || !allowed() || !p.active.unchanged() || errors.Is(cause, compat.ErrReserveTransactionPending) {
			return pending(errors.Join(cause, e))
		}
		for i := len(written) - 1; i >= 0; i-- {
			w := written[i]
			if !w.after.unchanged() {
				return pending(cause)
			}
			if w.before.info == nil {
				e = platform.RemoveRegularNoFollow(w.before.path)
			} else {
				e = platform.WriteFileAtomicNoFollow(w.before.path, w.before.raw, 0600)
			}
			if e != nil {
				return pending(errors.Join(cause, e))
			}
			restored, re := readDeletionImage(w.before.path, w.before.info == nil)
			if re != nil || !bytes.Equal(restored.raw, w.before.raw) || (restored.info == nil) != (w.before.info == nil) {
				return pending(errors.Join(cause, re, errors.New("catalog rollback image unconfirmed")))
			}
			expected[w.before.path] = restored
		}
		if !verify(p.active) {
			return pending(errors.Join(cause, errors.New("catalog rollback membership unconfirmed")))
		}
		if e := cleanup(); e != nil {
			return pending(errors.Join(cause, e))
		}
		return nil, failRefresh(cause)
	}
	if !verify(p.active) {
		return rollback(errors.New("source snapshot changed before owner preparation"))
	}
	if protected && apply == nil {
		if err := s.d.Svc.PrepareSourceRefreshCatalog(ctx, subID, p.changed); err != nil {
			if errors.Is(err, compat.ErrServiceUnavailable) {
				return rollback(err)
			}
			return pending(err)
		}
	}

	if !verify(p.active) {
		return pending(errors.New("source snapshot changed during owner preparation"))
	}
	for _, w := range writes {
		if !allowed() || !p.active.unchanged() || !w.before.unchanged() {
			return rollback(errors.New("refresh destination changed"))
		}
		if w.data == nil {
			err = platform.RemoveRegularNoFollow(w.before.path)
		} else {
			err = platform.WriteFileAtomicNoFollow(w.before.path, w.data, 0600)
		}
		if err != nil {
			return pending(err)
		}
		image, err := readDeletionImage(w.before.path, w.data == nil)
		if err != nil || !bytes.Equal(image.raw, w.data) || (image.info == nil) != (w.data == nil) {
			return pending(errors.Join(err, errors.New("staged profile unconfirmed")))
		}
		w.after = image
		written = append(written, w)
		expected[w.before.path] = image
	}
	if !verify(p.active) {
		return rollback(errors.New("staged snapshot changed before runtime transition"))
	}
	active := p.active
	if protected && len(p.removed) > 0 {
		candidates := []string{}
		if enabled {
			for _, rec := range p.final {
				candidates = append(candidates, jqRaw(rec["id"]))
			}
		}
		selected, e := s.d.Svc.PrepareSourceRefresh(ctx, subID, p.removed, candidates)
		if e != nil {
			if errors.Is(e, compat.ErrXrayCandidateRejected) || errors.Is(e, compat.ErrServiceUnavailable) {
				return rollback(e)
			}
			return pending(e)
		}
		active, err = readDeletionImage(servers.ActiveFile(s.d), true)
		if err != nil || strings.TrimSpace(string(active.raw)) != selected {
			return pending(errors.Join(err, errors.New("native selected identity unconfirmed")))
		}
		if p.result.Impact == "removed-replaced" {
			present := false
			for _, id := range candidates {
				present = present || id == selected
			}
			if !present || selected == p.activeID {
				return pending(errors.New("native selection outside refreshed source"))
			}
		} else if selected != p.activeID {
			return pending(errors.New("unrelated selection changed"))
		}
	}
	if protected && p.reapply {
		var selected string
		var e error
		if apply != nil {
			if !apply.config.unchanged() || s.d.App == nil || s.d.App.Xray == nil || s.d.App.Xray.RuntimeSnapshot() != apply.snapshot {
				return rollback(errPendingProfileChanged)
			}
			e = s.d.Svc.ApplyPendingSourceProfile(ctx, compat.SourceProfileApply{
				SourceID: subID, ServerID: p.activeID, PreviousName: p.previousName,
				ConfigSHA256: compat.SHA256Hex(apply.config.raw), CandidateSHA256: apply.binding.profile.CandidateSHA256,
				PreviousRecord:    bytes.Clone(apply.binding.working.raw),
				RecoveryAuthority: recoveryAuthority,
			}, op)
			if e != nil && !errors.Is(e, compat.ErrReserveTransactionPending) {
				return rollback(e)
			}
			selected = p.activeID
		} else if p.result.Impact == "display-name-changed" {
			selected, e = s.d.Svc.RenameSourceRefresh(ctx, subID, p.activeID, p.previousName)
		} else {
			selected, e = s.d.Svc.ReapplySourceRefresh(ctx, subID, p.activeID, p.previousName)
		}
		if e != nil {
			// Pruning may already have committed; catalog rollback is safe only for a
			// provable pre-mutation rejection with unchanged selection and no journal.
			if errors.Is(e, compat.ErrXrayCandidateRejected) || errors.Is(e, compat.ErrServiceUnavailable) {
				return rollback(e)
			}
			return pending(e)
		}
		active, err = readDeletionImage(servers.ActiveFile(s.d), false)
		if err != nil || selected != p.activeID || strings.TrimSpace(string(active.raw)) != selected {
			return pending(errors.Join(err, errors.New("retained native identity unconfirmed")))
		}
	}
	if !verify(active) {
		return pending(errors.New("source snapshot changed after native transition"))
	}
	keep := map[string]bool{}
	for _, r := range p.final {
		keep[jqRaw(r["id"])] = true
	}
	// Runtime ownership has committed. A cleanup error must retain current staged
	// profiles, selection and the marker; generic rollback would violate intent.
	for _, m := range p.members {
		if filepath.Dir(m.record.path) == target && keep[m.id] {
			continue
		}
		image := expected[m.record.path]
		if !allowed() || !active.unchanged() || !image.unchanged() || !m.quality.unchanged() {
			return pending(errors.New("obsolete source entry changed"))
		}
		if err := platform.RemoveRegularNoFollow(m.record.path); err != nil {
			return pending(err)
		}
		delete(expected, m.record.path)
		if !keep[m.id] && m.quality.info != nil {
			if !active.unchanged() || !m.quality.unchanged() {
				return pending(errors.New("obsolete quality changed"))
			}
			if err := platform.RemoveRegularNoFollow(m.quality.path); err != nil {
				return pending(err)
			}
		}
	}
	if !allowed() || !active.unchanged() {
		return pending(errors.New("selection changed at catalog commit"))
	}
	members, err := s.deletionMembers(subID)
	if err != nil {
		return pending(err)
	}
	if len(members) != len(p.final) {
		return pending(errors.New("committed source membership mismatch"))
	}
	for _, m := range members {
		image, ok := expected[m.record.path]
		if !ok || !image.unchanged() {
			return pending(errors.New("committed source image mismatch"))
		}
	}
	if pendingProfile != nil && !expected[pendingProfile.before.path].unchanged() {
		return pending(errors.New("pending profile changed at catalog commit"))
	}
	var journal *reserve.TransactionJournal
	if apply != nil {
		journal, err = reserve.OpenTransactionJournal(s.d.Root)
		if err != nil {
			return pending(err)
		}
		defer journal.Close()
		if err := journal.CheckSourceProfileCommit(ctx, recoveryAuthority); err != nil {
			return pending(err)
		}
	}
	if err := cleanup(); err != nil {
		return pending(err)
	}
	if journal != nil {
		if err := journal.FinishSourceProfile(ctx, recoveryAuthority); err != nil {
			return pending(err)
		}
	}
	return p.result, nil
}
