package routes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func registerPolicyHandlers() {
	cgi.Register("/api/routes/policy.cgi", wrap(hPolicy), cgi.Methods(http.MethodGet, http.MethodPost), cgi.Limit(routepolicy.MaxStateBytes))
	cgi.Register("/api/routes/native-domain-preview.cgi", wrap(hNativeDomainPreview), cgi.Methods(http.MethodGet, http.MethodPost), cgi.Limit(1024))
	cgi.Register("/api/routes/native-domain-apply.cgi", wrap(hNativeDomainApply), cgi.Methods(http.MethodGet, http.MethodPost), cgi.Limit(1024))
}

func hPolicy(e *engine, c *cgi.Req) (any, error) {
	if c.R.Method != http.MethodPost {
		return e.policyView(c.R.Context())
	}
	if err := requestGuard(c); err != nil {
		return nil, err
	}
	m, err := routepolicy.DecodeMutation(c.Body())
	if err != nil {
		return nil, cgi.E(400, "POLICY_REQUEST_INVALID", "Некорректное изменение правила.")
	}
	if geoMutation(m) {
		return e.queueGeodata(c)
	}
	return e.savePolicy(m)
}

type policyResult struct {
	State    routepolicy.State `json:"state"`
	Revision string            `json:"revision"`
	Busy     bool              `json:"busy"`
	Pending  bool              `json:"pending"`
}

func (e *engine) policyView(ctx context.Context) (policyResult, error) {
	busy := e.d.Lock.Busy()
	state, err := routepolicy.ReadState(ctx, e.root)
	if err != nil {
		return policyResult{}, err
	}
	revision, err := routepolicy.StateRevision(state)
	if err != nil {
		return policyResult{}, err
	}
	pending, err := routepolicy.RecoveryPending(ctx, e.d.Root)
	if err != nil {
		return policyResult{}, err
	}
	queued := false
	if e.queue != nil {
		_, queued = e.queue.FindLive(geodataAction, "policy")
	}
	return policyResult{state, revision, busy || e.d.Lock.Busy() || queued, pending}, nil
}

func (e *engine) savePolicy(m routepolicy.Mutation) (result any, resultErr error) {
	ctx, cancel := e.jobCtx()
	defer cancel()
	return e.runPolicy(ctx, m, nil)
}

func (e *engine) runPolicy(ctx context.Context, m routepolicy.Mutation, op *compat.Operation) (result any, resultErr error) {
	action := "routes:policy"
	if geoMutation(m) {
		action = geodataAction
	}
	var release compat.Release
	if op != nil {
		var err error
		release, err = e.d.Lock.Acquire(ctx, "routes", action, "policy")
		if err != nil {
			return nil, err
		}
	} else {
		var ok bool
		release, ok = e.d.Lock.TryAcquire("routes", action, "policy")
		if !ok {
			return nil, cgi.E(409, "POLICY_BUSY", "Дождитесь завершения текущей операции.")
		}
	}
	defer release()
	ctx = compat.WithFence(ctx)
	if op == nil {
		op = e.d.Queue.StartOperation("", "route_policy", action, "USER", "protected", "")
		defer func() { op.Finish(resultErr) }()
	}
	pending, pendingErr := routepolicy.RecoveryPending(ctx, e.d.Root)
	if pending || pendingErr != nil {
		return nil, e.policyUncertain(errors.Join(errors.New("previous policy transaction requires recovery"), pendingErr))
	}
	j, err := routepolicy.OpenJournal(e.d.Root)
	if err != nil {
		return nil, err
	}
	defer j.Close()
	state, baseline, err := j.Baseline(ctx)
	if err != nil {
		return nil, err
	}
	if m.ExpectedRevision != baseline.StateRevision {
		return nil, cgi.E(409, "POLICY_REVISION_CHANGED", "Правила уже изменились. Обновите данные и повторите действие.")
	}
	if m.Operation != "geo-update" && nativeOwnID.MatchString(m.ID) {
		if _, preset := nativeDomainPresetByID(m.ID); !preset {
			record, err := readNativeDomainRecord(nativeDomainRecordPath(e.root, m.ID), m.ID)
			if err != nil {
				return nil, err
			}
			if record != nil {
				return nil, cgi.E(409, "NATIVE_DOMAIN_IN_USE", "Сначала удалите маршрут этого списка из Keenetic или завершите его восстановление.")
			}
		}
	}
	if m.Operation == "enabled" && m.Enabled && strings.HasPrefix(m.ID, "own-") {
		for _, rule := range state.Rules {
			if rule.ID == m.ID && len(rule.Domains) > 0 && len(rule.CountryCodes) == 0 && len(rule.GeoSiteTags) == 0 {
				return nil, cgi.E(409, "NATIVE_DOMAIN_REQUIRED", "Включите список через раздел маршрутов Keenetic.")
			}
		}
	}
	next, err := m.Apply(state)
	if err != nil {
		return nil, cgi.E(400, "POLICY_REQUEST_INVALID", "Не удалось изменить выбранное правило.")
	}
	if geoMutation(m) {
		if err := e.checkGeoAutomation(ctx, m, op); err != nil {
			return nil, err
		}
		attempt, err := e.beginGeoAttempt(ctx, string(m.Kind), op)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := e.finishGeoAttempt(ctx, string(m.Kind), attempt, resultErr); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}()
	}
	snapshot, err := e.policySnapshot(ctx, baseline.GeoRevision)
	if err != nil {
		return nil, err
	}
	if state.NeedsGeodata() && state.GeoRevision != snapshot.Revision {
		return nil, errors.New("saved policy geodata revision changed")
	}
	var geoStore *geodata.Store
	var geoCandidate *geodata.Candidate
	journalOwnsCandidate := false
	if geoMutation(m) {
		op.Phase("fetching")
		geoStore, geoCandidate, err = e.preparePolicyGeodata(ctx, &next, snapshot, m)
		if err != nil {
			return nil, err
		}
		defer func() {
			if geoStore == nil {
				return
			}
			if !journalOwnsCandidate {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
				defer cancel()
				if err := geoStore.Abort(cleanup, geoCandidate); err != nil {
					resultErr = errors.Join(resultErr, err)
				}
			}
			geoStore.Close()
		}()
		if geoCandidate != nil {
			snapshot = geoCandidate.Snapshot()
		}
	}
	next.GeoRevision = snapshot.Revision
	// Selected geodata must remain installed even for disabled rules.
	check := append([]routepolicy.Rule{}, next.Rules...)
	for i := range check {
		if len(check[i].GeoSiteTags)+len(check[i].CountryCodes) > 0 {
			check[i].Enabled = true
		}
	}
	if _, err := routepolicy.Compile(check, snapshot, routepolicy.OutboundTags{VPN: "proxy", Direct: "broray-direct", Deny: "broray-deny"}, []string{"socks"}); err != nil {
		return nil, cgi.E(400, "POLICY_RULE_INVALID", "Проверьте правила и установленные геоданные.")
	}
	revision, err := routepolicy.StateRevision(next)
	if err != nil {
		return nil, err
	}
	ctx, err = withPreparedPolicy(ctx, next, snapshot)
	if err != nil {
		return nil, err
	}
	op.Phase("checking")
	cfg, err := e.d.Svc.PreviewActiveConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg != nil && baseline.ConfigSHA256 == "" {
		return nil, errors.New("active config missing; restore active connection before changing policy")
	}
	configHash := sha256.Sum256(cfg)
	needsApply := cfg != nil && hex.EncodeToString(configHash[:]) != baseline.ConfigSHA256
	if err := j.BeginChecked(ctx, next, cfg, snapshot.Revision, baseline); err != nil {
		pending, readErr := routepolicy.RecoveryPending(ctx, e.d.Root)
		if pending || readErr != nil || errors.Is(err, routepolicy.ErrJournalUncertain) {
			journalOwnsCandidate = true
			return nil, e.policyUncertain(err)
		}
		return nil, err
	}
	journalOwnsCandidate = true
	op.Phase("committing")
	if geoCandidate != nil {
		_, err = geoStore.Commit(ctx, geoCandidate, baseline.GeoRevision)
	}
	applyAttempted := false
	if err == nil && needsApply {
		applyAttempted = true
		err = e.d.Svc.ApplyXrayConfig(ctx, cfg)
	}
	if err == nil {
		err = j.InstallState(ctx)
	}
	if err != nil {
		op.Phase("recovering")
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		restore := func(ctx context.Context, old []byte, exists bool) error {
			if !applyAttempted {
				return nil
			}
			if !exists {
				return errors.New("active config predecessor missing")
			}
			return e.d.Svc.ApplyXrayConfig(ctx, old)
		}
		if rollbackErr := j.Rollback(recovery, restore); rollbackErr != nil {
			return nil, e.policyUncertain(errors.Join(err, rollbackErr))
		}
		return nil, cgi.E(500, "POLICY_APPLY_FAILED", "Изменение не применено. Предыдущие настройки восстановлены.")
	}
	// A failed confirmation may already have persisted the committed marker.
	// Keep the fence for guarded readback/recovery; never blindly roll it back.
	if err := j.Confirm(ctx); err != nil {
		return nil, e.policyUncertain(err)
	}
	return policyResult{State: next, Revision: revision}, nil
}

func (e *engine) policySnapshot(ctx context.Context, revision string) (geodata.Snapshot, error) {
	if revision == "" {
		return geodata.Snapshot{}, nil
	}
	store, err := geodata.Open(filepath.Join(e.d.Root.Runtime(), geodata.AssetDirectory))
	if err != nil {
		return geodata.Snapshot{}, err
	}
	defer store.Close()
	snapshot, err := store.Load(ctx)
	if err != nil {
		return geodata.Snapshot{}, err
	}
	if snapshot.Revision != revision {
		return geodata.Snapshot{}, errors.New("geodata changed during policy preview")
	}
	return snapshot, nil
}

func (e *engine) policyUncertain(cause error) error {
	err := e.d.Lock.MarkPending(false)
	e.log.Printf("routes: policy transaction requires recovery: %v", errors.Join(cause, err))
	return cgi.E(503, "POLICY_RECOVERY_REQUIRED", "Результат операции не подтверждён. Изменения заблокированы до восстановления.")
}
