package routes

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
	modxray "github.com/BROadmin/broray/modules/xray"
)

func (e *engine) configureRouting(ctx context.Context, base []byte) ([]byte, error) {
	if prepared, ok := ctx.Value(preparedPolicyKey{}).(preparedPolicy); ok {
		if !compat.FenceHeld(ctx) {
			return nil, errors.New("prepared policy requires mutation fence")
		}
		state, err := routepolicy.DecodeState([]byte(prepared.state))
		if err != nil {
			return nil, err
		}
		var snapshot geodata.Snapshot
		if err := json.Unmarshal([]byte(prepared.snapshot), &snapshot); err != nil {
			return nil, err
		}
		return e.renderRouting(base, state, snapshot)
	}
	state, err := routepolicy.ReadState(ctx, e.root)
	if err != nil {
		return nil, err
	}
	var snapshot geodata.Snapshot
	if state.NeedsGeodata() {
		if state.GeoRevision == "" {
			return nil, errors.New("routing policy geodata revision missing")
		}
		store, err := geodata.Open(filepath.Join(e.d.Root.Runtime(), geodata.AssetDirectory))
		if err != nil {
			return nil, err
		}
		defer store.Close()
		snapshot, err = store.Load(ctx)
		if err != nil {
			return nil, err
		}
		if snapshot.Revision != state.GeoRevision {
			return nil, errors.New("routing policy geodata revision changed")
		}
	}
	return e.renderRouting(base, state, snapshot)
}

type preparedPolicyKey struct{}
type preparedPolicy struct{ state, snapshot string }

func withPreparedPolicy(ctx context.Context, state routepolicy.State, snapshot geodata.Snapshot) (context.Context, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if _, err := routepolicy.DecodeState(data); err != nil {
		return nil, err
	}
	assets, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, preparedPolicyKey{}, preparedPolicy{string(data), string(assets)}), nil
}

func (e *engine) renderRouting(base []byte, state routepolicy.State, snapshot geodata.Snapshot) ([]byte, error) {
	directInterface := ""
	for _, rule := range state.Rules {
		if rule.Enabled && rule.Action == routepolicy.Direct {
			resolve := e.directInterface
			if resolve == nil {
				resolve = modxray.DirectInterface
			}
			var err error
			directInterface, err = resolve()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	return routepolicy.Render(base, state.Rules, snapshot, geodata.AssetDirectory,
		routepolicy.OutboundTags{VPN: "proxy", Direct: "broray-direct", Deny: "broray-deny"}, []string{"socks"}, directInterface)
}
