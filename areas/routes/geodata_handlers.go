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
				t.Result("Геоданные проверены и сохранены.", "")
			}
			return err
		}}}})
}

func geoMutation(m routepolicy.Mutation) bool {
	return m.Operation == "geo-upsert" || m.Operation == "geo-update"
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
		if part.kind == m.Kind || *part.revision == "" {
			rev, err := e.geoResolve(ctx, part.kind)
			if err != nil {
				return nil, nil, err
			}
			*part.revision = rev
			dates[string(part.kind)] = e.geoTime().Format(time.RFC3339Nano)
		}
	}
	validate := func(ctx context.Context, snapshot geodata.Snapshot) error {
		// Always load every selected asset in the real core, even if all rules
		// are off and no active server exists. This config never handles traffic.
		state := routepolicy.State{SchemaVersion: 1, Rules: []routepolicy.Rule{{ID: "geodata-validation", Enabled: true, Action: routepolicy.Deny, CountryCodes: snapshot.Selection.CountryCodes, GeoSiteTags: snapshot.Selection.GeoSiteTags}}}
		base := []byte(`{"inbounds":[{"tag":"socks","protocol":"socks","listen":"127.0.0.1","port":1080}],"outbounds":[{"tag":"proxy","protocol":"blackhole"}]}`)
		cfg, err := e.renderRouting(base, state, snapshot)
		if err != nil {
			return err
		}
		return e.d.Svc.ValidateXrayConfig(ctx, cfg)
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
		if err := validate(ctx, previous); err != nil {
			return nil, nil, err
		}
		next.GeoUpdatedAt = dates
		return nil, nil, nil
	}
	store, err := e.openManagedGeodata()
	if err != nil {
		return nil, nil, err
	}
	candidate, err := store.PrepareReusing(ctx, sel, revs, e.geoFetch, validate)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	if candidate.Snapshot().PreviousRevision != previous.Revision {
		cleanup := store.Abort(context.WithoutCancel(ctx), candidate)
		store.Close()
		return nil, nil, errors.Join(errors.New("geodata changed before preparation"), cleanup)
	}
	next.GeoUpdatedAt = dates
	return store, candidate, nil
}
