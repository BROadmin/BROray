package subscriptions

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

func (s *svc) defineSourceRemovalAction() {
	s.d.Queue.Define(compat.ActionSpec{Name: compat.SourceRemovalAction, Type: "subscription_operation", Priority: map[string]int{"*": 1}, Stages: []compat.Stage{{Name: "remove", Phase: "committing", Global: true, Protected: true, Run: s.sourceRemovalStep}}})
}

func (s *svc) sourceRemovalImage(id string) (deletionImage, error) {
	image, err := readDeletionImage(s.recPath(id), false)
	if errors.Is(err, os.ErrNotExist) {
		return image, errNotFound
	}
	if err != nil {
		return image, errPersistence
	}
	value, err := strictjson.Decode(image.raw)
	record, ok := value.(map[string]any)
	if err != nil || !ok || record["id"] != id {
		return image, errPersistence
	}
	return image, nil
}

func (s *svc) handleSourceRemoval(c *cgi.Req) {
	if _, ok := compat.OpsBody(c); !ok {
		return
	}
	value, err := strictjson.Decode(c.Body())
	body, ok := value.(map[string]any)
	invalid := func() {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректные параметры удаления подписки."))
	}
	if err != nil || !ok || len(body) != 2 {
		invalid()
		return
	}
	id, iok := body["id"].(string)
	nonce, nok := body["nonce"].(string)
	if !iok || !nok || !validSourceRemovalID(id) || len(nonce) != 32 || strings.ToLower(nonce) != nonce {
		invalid()
		return
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		invalid()
		return
	}
	image, err := s.sourceRemovalImage(id)
	if err != nil {
		cgi.WriteErr(c.W, toAPI(err))
		return
	}
	request, coalesced, err := s.d.Queue.Submit(compat.SourceRemovalAction, compat.ReserveOperationTarget, "USER", id+":"+compat.SHA256Hex(image.raw), nonce)
	if err != nil {
		compat.WriteOpsError(c.W, err)
		return
	}
	receipt := request.Receipt(coalesced)
	receipt["accepted"] = true
	cgi.WriteJSON(c.W, 202, receipt)
}

type sourceRemovalCandidate struct {
	id    string
	image deletionImage
}

// Only live, unambiguously identified records outside the removed source are
// eligible. Unsupported profile contents are classified by the native owner.
func (s *svc) sourceRemovalCandidates(id string) ([]sourceRemovalCandidate, error) {
	var result []sourceRemovalCandidate
	dir, err := platform.OpenDirNoFollow(servers.ServersDir(s.d))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		node := strings.TrimSuffix(name, ".json")
		if !servers.ValidID(node) {
			return nil, errPersistence
		}
		image, err := readDeletionImage(filepath.Join(servers.ServersDir(s.d), name), false)
		if err != nil {
			return nil, err
		}
		value, err := strictjson.Decode(image.raw)
		record, ok := value.(map[string]any)
		if err != nil || !ok || record["id"] != node {
			return nil, errPersistence
		}
		if source, present := record["source"]; present && source != nil {
			if _, ok := source.(map[string]any); !ok {
				return nil, errPersistence
			}
		}
		if belongsTo(record, id) {
			continue
		}
		result = append(result, sourceRemovalCandidate{node, image})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result, nil
}

func (s *svc) sourceRemovalStep(t *compat.Task) error {
	refuse := func(err error) error {
		var sub *subErr
		if errors.As(err, &sub) {
			t.Result(sub.Msg, sub.Code)
		} else {
			t.Result("Удаление подписки не подтверждено. Записи источника сохранены, если удаление ещё не началось.", "SERVER_SOURCE_REMOVE_FAILED")
		}
		return err
	}
	id, revision, ok := strings.Cut(t.Context, ":")
	if !ok || !validSourceRemovalID(id) || len(revision) != 64 || t.Target != compat.ReserveOperationTarget || !compat.FenceHeld(t.Ctx) {
		return stepFail(t, "INVALID_REQUEST", "Некорректная операция удаления подписки.")
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return stepFail(t, "INVALID_REQUEST", "Некорректная версия подписки.")
	}
	source, err := s.sourceRemovalImage(id)
	if err != nil || compat.SHA256Hex(source.raw) != revision {
		return stepFail(t, "SUBSCRIPTION_CHANGED", "Подписка изменилась до удаления. Обновите список и повторите действие.")
	}
	if exists(s.d.RunPath("subscriptions", id+".lock")) {
		return stepFail(t, "UPDATE_ALREADY_RUNNING", "Состояние прежней операции подписки требует восстановления.")
	}
	if err := s.syncMarkers(); err != nil {
		return refuse(err)
	}
	active, err := readDeletionImage(servers.ActiveFile(s.d), true)
	if err != nil {
		return refuse(err)
	}
	activeID := strings.TrimSpace(string(active.raw))
	if active.info != nil && !servers.ValidID(activeID) {
		return refuse(errPersistence)
	}
	members, err := s.deletionMembers(id)
	if err != nil {
		return refuse(err)
	}
	candidates, err := s.sourceRemovalCandidates(id)
	if err != nil {
		return refuse(err)
	}
	catalogCurrent := func() bool {
		if !source.unchanged() {
			return false
		}
		for _, m := range members {
			if !m.record.unchanged() || !m.quality.unchanged() {
				return false
			}
		}
		for _, c := range candidates {
			if !c.image.unchanged() {
				return false
			}
		}
		return true
	}
	if !active.unchanged() || !catalogCurrent() {
		return refuse(errPersistence)
	}
	removedIDs := make([]string, 0, len(members))
	for _, m := range members {
		removedIDs = append(removedIDs, m.id)
	}
	candidateIDs := make([]string, 0, len(candidates))
	for _, c := range candidates {
		candidateIDs = append(candidateIDs, c.id)
	}
	chosen, err := s.d.Svc.PrepareSourceRemoval(t.Ctx, id, removedIDs, candidateIDs)
	if err != nil {
		return refuse(err)
	}
	after, err := readDeletionImage(servers.ActiveFile(s.d), true)
	if err != nil {
		return refuse(err)
	}
	if strings.TrimSpace(string(after.raw)) != chosen || (chosen == "" && after.info != nil) || (chosen != "" && !servers.ValidID(chosen)) {
		return refuse(errPersistence)
	}
	for _, node := range removedIDs {
		if node == chosen {
			return refuse(errPersistence)
		}
	}
	if !catalogCurrent() {
		return refuse(errPersistence)
	}
	guard := func() bool {
		return t.Ctx.Err() == nil && compat.FenceHeld(t.Ctx) && source.unchanged() && after.unchanged()
	}
	if _, err := s.removeServers(id, guard); err != nil {
		return refuse(err)
	}
	if !guard() {
		return refuse(errPersistence)
	}
	if err := s.finishSourceRemoval(id, source, after); err != nil {
		return refuse(err)
	}
	s.logf("subscription=" + id + " action=delete-queued")
	t.Result("Подписка и серверы этого источника удалены.", "")
	return nil
}

// A final rescan is necessary: checking only pre-existing record images cannot
// detect a newly added source member. Never unlink the subscription first.
func (s *svc) finishSourceRemoval(id string, source, active deletionImage) error {
	if source.path != s.recPath(id) || active.path != servers.ActiveFile(s.d) || !source.unchanged() || !active.unchanged() {
		return errPersistence
	}
	if err := s.syncMarkers(); err != nil {
		return err
	}
	remaining, err := s.deletionMembers(id)
	if err != nil || len(remaining) != 0 {
		return fail("SERVER_SOURCE_REMOVE_FAILED", "Не все записи источника удалены. Подписка сохранена.")
	}
	if !source.unchanged() || !active.unchanged() {
		return errPersistence
	}
	if err := platform.RemoveRegularNoFollow(source.path); err != nil {
		return fail("PERSISTENCE_ERROR", "Удаление записи подписки не подтверждено.")
	}
	after, err := readDeletionImage(source.path, true)
	if err != nil || after.info != nil || source.parent == nil || after.parent == nil || !os.SameFile(source.parent, after.parent) {
		return errPersistence
	}
	return nil
}

func (s *svc) removeLegacySource(id string) (any, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	source, err := s.sourceRemovalImage(id)
	if err != nil {
		return nil, err
	}
	if exists(s.d.RunPath("subscriptions", id+".lock")) {
		return nil, fail("UPDATE_ALREADY_RUNNING", "Состояние прежней операции подписки требует восстановления.")
	}
	active, err := readDeletionImage(servers.ActiveFile(s.d), true)
	if err != nil {
		return nil, errPersistence
	}
	guard := func() bool { return source.unchanged() && active.unchanged() }
	removed, err := s.removeServers(id, guard)
	if err != nil {
		return nil, err
	}
	if err := s.finishSourceRemoval(id, source, active); err != nil {
		return nil, err
	}
	s.logf("subscription=" + id + " action=delete")
	return map[string]any{"subscriptionId": id, "removed": removed, "activeServerImpact": "none"}, nil
}

func validSourceRemovalID(id string) bool {
	return validID(id) == nil && len(id) <= 96 && id[0] != '.' && id[0] != '-'
}
