package routes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
	"golang.org/x/sys/unix"
)

const geodataAction = "routes:geodata"

func (e *engine) defineGeodataQueue() {
	if e.queue == nil {
		return
	}
	e.queue.Define(compat.ActionSpec{Name: geodataAction, Type: "route_policy", Priority: map[string]int{"USER": 3, "*": 7},
		Stages: []compat.Stage{{Name: "apply", Phase: "waiting", Protected: true, Run: func(t *compat.Task) error {
			m, err := routepolicy.DecodeMutation([]byte(t.Context))
			if err != nil {
				return err
			}
			if !geoMutation(m) {
				return errors.New("invalid queued geodata operation")
			}
			ctx, cancel := context.WithTimeout(t.Ctx, jobDeadline)
			defer cancel()
			_, err = e.runPolicy(ctx, m, t.Op)
			if err == nil {
				switch m.Operation {
				case "geo-check":
					t.Result("Проверка геоданных завершена.", "")
				case "geo-apply":
					t.Result("Подготовленные геоданные применены.", "")
				default:
					t.Result("Геоданные проверены и сохранены.", "")
				}
			}
			return err
		}}}})
}

func geoMutation(m routepolicy.Mutation) bool {
	return m.Operation == "geo-upsert" || m.Operation == "geo-update" || m.Operation == "geo-check" || m.Operation == "geo-apply"
}

func (e *engine) queueGeodata(c *cgi.Req) (any, error) {
	if e.queue == nil {
		return nil, compat.ErrServiceUnavailable
	}
	nonce := c.Header("X-BROray-Nonce")
	if nonce == "" {
		nonce = compat.RandHex(16)
	}
	r, coalesced, err := e.queue.Submit(geodataAction, "policy", "USER", string(c.Body()), nonce)
	if err != nil {
		return nil, err
	}
	return r.Receipt(coalesced), nil
}

func policySelection(state routepolicy.State) geodata.Selection {
	sel := geodata.Selection{}
	for _, rule := range state.Rules {
		sel.CountryCodes = append(sel.CountryCodes, rule.CountryCodes...)
		sel.GeoSiteTags = append(sel.GeoSiteTags, rule.GeoSiteTags...)
	}
	return sel
}

func selectedGeodataKinds(sel geodata.Selection) []string {
	var kinds []string
	if len(sel.CountryCodes) != 0 {
		kinds = append(kinds, string(geodata.GeoIP))
	}
	if len(sel.GeoSiteTags) != 0 {
		kinds = append(kinds, string(geodata.GeoSite))
	}
	return kinds
}

func (e *engine) openManagedGeodata() (*geodata.Store, error) {
	path := e.d.Root.Runtime()
	parent, err := platform.OpenDirNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	err = unix.Mkdirat(int(parent.Fd()), geodata.AssetDirectory, 0700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err == nil {
		if err := parent.Sync(); err != nil {
			return nil, err
		}
	}
	child, err := platform.OpenDirAtNoFollow(parent, geodata.AssetDirectory)
	if err != nil {
		return nil, err
	}
	defer child.Close()
	visible, err := platform.OpenDirNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer visible.Close()
	a, err := parent.Stat()
	if err != nil {
		return nil, err
	}
	b, err := visible.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(a, b) {
		return nil, errors.New("geodata runtime root changed")
	}
	return geodata.Open(filepath.Join(path, geodata.AssetDirectory))
}

// Prepares selected files, including disabled rules, without publishing a new
// pointer. The caller journals the candidate before commit and config apply.
func (e *engine) preparePolicyGeodata(ctx context.Context, next *routepolicy.State, previous geodata.Snapshot, m routepolicy.Mutation) (*geodata.Store, *geodata.Candidate, error) {
	sel := policySelection(*next)
	if m.Operation == "geo-check" && len(sel.CountryCodes)+len(sel.GeoSiteTags) == 0 {
		return nil, nil, cgi.E(400, "POLICY_RULE_INVALID", "Сначала добавьте набор или страну.")
	}
	if (m.Kind == geodata.GeoIP && len(sel.CountryCodes) == 0) || (m.Kind == geodata.GeoSite && len(sel.GeoSiteTags) == 0) {
		return nil, nil, cgi.E(400, "POLICY_RULE_INVALID", "Сначала добавьте набор или страну.")
	}
	revs := geodata.Revisions{}
	for _, asset := range previous.Assets {
		if asset.Kind == geodata.GeoIP {
			revs.GeoIP = asset.Source.Revision
		} else {
			revs.GeoSite = asset.Source.Revision
		}
	}
	dates := map[string]string{}
	for kind, date := range next.GeoUpdatedAt {
		dates[kind] = date
	}
	for _, part := range []struct {
		kind     geodata.Kind
		selected bool
		revision *string
	}{{geodata.GeoIP, len(sel.CountryCodes) > 0, &revs.GeoIP}, {geodata.GeoSite, len(sel.GeoSiteTags) > 0, &revs.GeoSite}} {
		if !part.selected {
			continue
		}
		if m.Operation == "geo-check" || part.kind == m.Kind || *part.revision == "" {
			rev, err := e.geoResolve(ctx, part.kind)
			if err != nil {
				return nil, nil, err
			}
			*part.revision = rev
			dates[string(part.kind)] = e.geoTime().Format(time.RFC3339Nano)
		}
	}
	plan, err := geodata.SourcePlan(sel, revs)
	if err != nil {
		return nil, nil, err
	}
	oldPlan := make([]geodata.Source, 0, len(previous.Assets))
	for _, asset := range previous.Assets {
		oldPlan = append(oldPlan, asset.Source)
	}
	if previous.Revision != "" && reflect.DeepEqual(plan, oldPlan) {
		if err := e.validatePolicyGeodata(ctx, previous); err != nil {
			return nil, nil, err
		}
		next.GeoUpdatedAt = dates
		return nil, nil, nil
	}
	store, err := e.openManagedGeodata()
	if err != nil {
		return nil, nil, err
	}
	candidate, err := store.PrepareReusing(ctx, sel, revs, e.geoFetch, e.validatePolicyGeodata)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	if candidate.Snapshot().PreviousRevision != previous.Revision {
		cleanup := store.Abort(context.WithoutCancel(ctx), candidate)
		store.Close()
		return nil, nil, errors.Join(errors.New("geodata changed before preparation"), cleanup)
	}
	if (m.Operation == "geo-update" || m.Operation == "geo-check") && previous.Revision != "" {
		var used geodata.Selection
		for _, rule := range next.Rules {
			if rule.Enabled {
				used.CountryCodes = append(used.CountryCodes, rule.CountryCodes...)
				used.GeoSiteTags = append(used.GeoSiteTags, rule.GeoSiteTags...)
			}
		}
		if len(used.CountryCodes)+len(used.GeoSiteTags) > 0 {
			same, compareErr := store.SameUsedData(ctx, candidate, used)
			if same || compareErr != nil {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
				defer cancel()
				if err := errors.Join(compareErr, store.Abort(cleanup, candidate), store.Close()); err != nil {
					return nil, nil, err
				}
				next.GeoUpdatedAt = dates
				return nil, nil, nil
			}
		}
	}
	next.GeoUpdatedAt = dates
	return store, candidate, nil
}

func (e *engine) validatePolicyGeodata(ctx context.Context, snapshot geodata.Snapshot) error {
	// Load every selected asset in the real core, including disabled rules.
	// This validation config never handles traffic.
	state := routepolicy.State{SchemaVersion: 1, Rules: []routepolicy.Rule{{ID: "geodata-validation", Enabled: true, Action: routepolicy.Deny, CountryCodes: snapshot.Selection.CountryCodes, GeoSiteTags: snapshot.Selection.GeoSiteTags}}}
	base := []byte(`{"inbounds":[{"tag":"socks","protocol":"socks","listen":"127.0.0.1","port":1080}],"outbounds":[{"tag":"proxy","protocol":"blackhole"}]}`)
	cfg, err := e.renderRouting(base, state, snapshot)
	if err != nil {
		return err
	}
	return e.d.Svc.ValidateXrayConfig(ctx, cfg)
}

type geoPendingView struct {
	Revision  string    `json:"revision"`
	CheckedAt time.Time `json:"checkedAt"`
}

func pendingGeoView(p *geodata.PendingUpdate) *geoPendingView {
	if p == nil {
		return nil
	}
	return &geoPendingView{Revision: p.Revision, CheckedAt: p.CheckedAt}
}

func (e *engine) readPendingGeodata(ctx context.Context, policy, base string) (*geoPendingView, error) {
	store, err := geodata.Open(filepath.Join(e.d.Root.Runtime(), geodata.AssetDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	p, err := store.Pending(ctx, policy, base)
	return pendingGeoView(p), err
}

func (e *engine) reopenPolicyGeodata(ctx context.Context, next *routepolicy.State, m routepolicy.Mutation) (*geodata.Store, *geodata.Candidate, error) {
	changed := cgi.E(409, "GEODATA_PENDING_CHANGED", "Подготовленные данные изменились. Проверьте геоданные заново.")
	store, err := geodata.Open(filepath.Join(e.d.Root.Runtime(), geodata.AssetDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, changed
	}
	if err != nil {
		return nil, nil, err
	}
	candidate, p, err := store.ReopenPending(ctx, m.ExpectedRevision, m.ExpectedPendingRevision)
	if errors.Is(err, geodata.ErrPendingChanged) {
		err = changed
	}
	if err == nil {
		err = e.validatePolicyGeodata(ctx, candidate.Snapshot())
	}
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	dates := make(map[string]string, len(next.GeoUpdatedAt))
	for kind, at := range next.GeoUpdatedAt {
		dates[kind] = at
	}
	for _, asset := range candidate.Snapshot().Assets {
		dates[string(asset.Kind)] = p.CheckedAt.Format(time.RFC3339Nano)
	}
	next.GeoUpdatedAt = dates
	return store, candidate, nil
}
