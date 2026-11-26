package subscriptions

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"syscall"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/xray"
)

const actionPendingProfileApply = compat.SourceProfileApplyAction

var errPendingProfileChanged = errors.New("SUBSCRIPTION_PROFILE_CHANGED")

type pendingApplyObservation struct {
	binding  pendingActiveBinding
	config   deletionImage
	snapshot xray.RuntimeSnapshot
	context  string
	schedule *pendingProfileScheduleClaim
}

func (s *svc) observePendingApply(subID, revision string) (*pendingApplyObservation, error) {
	binding, err := s.bindPendingActiveProfile(subID)
	if err != nil || binding.revision() != revision || s.d.App == nil || s.d.App.Xray == nil {
		return nil, errPendingProfileChanged
	}
	config, err := readDeletionImageLimit(s.d.Root.XrayConfig(), false, 2<<20)
	if err != nil {
		return nil, errPendingProfileChanged
	}
	stat, ok := binding.active.info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errPendingProfileChanged
	}
	p := s.d.App.Xray
	snapshot := p.RuntimeSnapshot()
	identity, err := compat.CanonicalSHA256(struct {
		Device, Inode uint64
		Modified      int64
		Config        string
		Runtime       xray.RuntimeSnapshot
	}{uint64(stat.Dev), uint64(stat.Ino), binding.active.info.ModTime().UnixNano(), compat.SHA256Hex(config.raw), snapshot})
	if err != nil || s.d.App.Xray != p || p.RuntimeSnapshot() != snapshot || !config.unchanged() || !binding.unchanged() {
		return nil, errPendingProfileChanged
	}
	return &pendingApplyObservation{binding: binding, config: config, snapshot: snapshot, context: revision + ":" + identity}, nil
}

func exactApplyHex(s string, length int) bool {
	if len(s) != length || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (s *svc) servePendingProfileApply(c *cgi.Req) {
	body, ok := compat.OpsBody(c)
	if !ok {
		return
	}
	var id, revision, nonce string
	if len(body) != 3 || json.Unmarshal(body["id"], &id) != nil || json.Unmarshal(body["revision"], &revision) != nil ||
		json.Unmarshal(body["nonce"], &nonce) != nil || validID(id) != nil || len(id) > 96 || strings.ContainsAny(id[:1], ".-") ||
		!exactApplyHex(revision, 64) || !exactApplyHex(nonce, 32) {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректные параметры применения подписки."))
		return
	}
	accept := func(r *compat.Request, coalesced bool) {
		receipt := r.Receipt(coalesced)
		receipt["accepted"] = true
		cgi.WriteJSON(c.W, 202, receipt)
	}
	if r, exists := s.d.Queue.Lookup(nonce); exists {
		if r.Action != actionPendingProfileApply || r.Target != id || r.Source != "USER" || !strings.HasPrefix(r.Context, revision+":") {
			cgi.WriteErr(c.W, cgi.E(409, "REQUEST_MISMATCH", "Параметры прежнего запроса отличаются."))
			return
		}
		accept(r, r.Nonce != nonce)
		return
	}
	obs, err := s.observePendingApply(id, revision)
	if err != nil {
		cgi.WriteErr(c.W, cgi.E(409, "SUBSCRIPTION_PROFILE_CHANGED", "Ожидающие параметры изменились. Обновите страницу и повторите действие."))
		return
	}
	r, coalesced, err := s.d.Queue.Submit(actionPendingProfileApply, id, "USER", obs.context, nonce)
	if err != nil {
		compat.WriteOpsError(c.W, err)
		return
	}
	accept(r, coalesced)
}

func (s *svc) stagePendingProfileApply(t *compat.Task) error {
	parts := strings.Split(t.Context, ":")
	scheduled := t.Source == "SCHEDULER"
	if !compat.FenceHeld(t.Ctx) || (t.Source != "USER" && !scheduled) ||
		(!scheduled && len(parts) != 2) || (scheduled && len(parts) != 4) {
		return stepFail(t, "SUBSCRIPTION_PROFILE_CHANGED", "Подтверждение применения не найдено.")
	}
	obs, err := s.observePendingApply(t.Target, parts[0])
	if err != nil || obs.context != parts[0]+":"+parts[1] {
		return stepFail(t, "SUBSCRIPTION_PROFILE_CHANGED", "Ожидающие параметры или активный сервер изменились.")
	}
	if scheduled {
		obs.schedule = &pendingProfileScheduleClaim{revision: parts[2], day: parts[3]}
		if _, err := s.pendingProfileWindow(t.Ctx, t.Target, obs); err != nil {
			return stepFail(t, "SUBSCRIPTION_PROFILE_WINDOW_EXPIRED", "Время автоматического применения истекло или настройки изменились.")
		}
	}
	_, err = s.syncCatalogCommit(t.Ctx, t.Target, nil, true, "replace", obs, t.Op)
	switch {
	case err == nil:
		t.Result("Параметры активного сервера из подписки применены.", "")
		return nil
	case errors.Is(err, compat.ErrReserveTransactionPending):
		return err
	case errors.Is(err, errPendingProfileChanged):
		return stepFail(t, "SUBSCRIPTION_PROFILE_CHANGED", "Ожидающие параметры или активный сервер изменились.")
	case errors.Is(err, errPendingProfileWindow):
		return stepFail(t, "SUBSCRIPTION_PROFILE_WINDOW_EXPIRED", "Время автоматического применения истекло или настройки изменились.")
	case errors.Is(err, compat.ErrSourceProfileRolledBack):
		return stepFail(t, "SUBSCRIPTION_PROFILE_VPN_ROLLED_BACK", "Связь с новыми параметрами не подтверждена. Предыдущая конфигурация восстановлена.")
	default:
		return errors.Join(stepFail(t, "SUBSCRIPTION_PROFILE_APPLY_FAILED", "Не удалось применить ожидающие параметры подписки."), err)
	}
}

func (s *svc) planPendingProfileApply(ctx context.Context, subID string, obs *pendingApplyObservation) (*refreshCatalogPlan, error) {
	b := obs.binding
	if !compat.FenceHeld(ctx) || !b.unchanged() || !obs.config.unchanged() ||
		s.d.App == nil || s.d.App.Xray == nil || s.d.App.Xray.RuntimeSnapshot() != obs.snapshot {
		return nil, errPendingProfileChanged
	}
	members, err := s.deletionMembers(subID)
	if err != nil {
		return nil, err
	}
	p := &refreshCatalogPlan{members: members, active: b.active, activeID: b.profile.ServerID,
		reapply: true, result: &syncResult{Policy: "replace", Impact: "configuration-changed"}}
	candidate, ok := decodeObject(b.profile.Candidate)
	if !ok {
		return nil, errPendingProfileChanged
	}
	for _, m := range members {
		if m.record.path != servers.RecordPath(s.d, m.id) {
			return nil, errPendingProfileChanged
		}
		rec, ok := decodeObject(m.record.raw)
		if !ok {
			return nil, errPendingProfileChanged
		}
		if m.id == p.activeID {
			if compat.SHA256Hex(m.record.raw) != b.profile.ActiveSHA256 || configHash(rec) == configHash(candidate) {
				return nil, errPendingProfileChanged
			}
			p.previousName = nameOrID(rec)
			p.result.Updated = 1
			managed, err := servers.ManagedDPI(candidate)
			if err != nil {
				return nil, err
			}
			if managed {
				p.dpiChanged = append(p.dpiChanged, candidate)
			}
			rec = candidate
		}
		p.final = append(p.final, rec)
	}
	if p.previousName == "" || !b.unchanged() {
		return nil, errPendingProfileChanged
	}
	p.result.Total = len(p.final)
	return p, nil
}
