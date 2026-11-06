package compat

import (
	"context"
	"errors"

	"github.com/BROadmin/broray/internal/keenetic"
)

// Services are the few cross-area calls. Each field is set by its owner
// area in Setup; callers must use the wrapper methods, which return
// ErrServiceUnavailable when the owner did not provide the function.
// Everything else crosses areas through the same files as in 3.x
// (config/interface.json, run/keenetic-status.json, servers/*.json, …).
type Services struct {
	// xray area: validate config bytes with `xray -test`, install them
	// atomically at Root.XrayConfig(), (re)start Xray, wait for the SOCKS
	// inbound, roll back to the previous config on failure.
	XrayApplyConfig func(ctx context.Context, config []byte) error
	// xray area: is the supervised Xray running.
	XrayRunning func() bool

	// keenetic area: create or repair the owned ProxyN → Xray SOCKS
	// interface (receipt config/interface.json), save and verify.
	KeeneticEnsureInterface func(ctx context.Context) error
	// keenetic area: the cached read-only IPv6 fact
	// {state, wan, lanPrefix, warning, checkedAt} (keenetic.IPv6Facts.Object).
	KeeneticIPv6 func(ctx context.Context) map[string]any
	// keenetic area: finish active UDP checks and remove their temporary routes before daemon exit.
	KeeneticWaitUDPJobs func(ctx context.Context) error

	// servers area: regenerate and apply the Xray config of the active
	// server (after its record changed, e.g. by a subscription refresh).
	ServersReapplyActive func(ctx context.Context, reason string) error
}

var ErrServiceUnavailable = errors.New("SERVICE_UNAVAILABLE: Модуль BROray недоступен.")

func (s *Services) ApplyXrayConfig(ctx context.Context, cfg []byte) error {
	if s.XrayApplyConfig == nil {
		return ErrServiceUnavailable
	}
	return s.XrayApplyConfig(ctx, cfg)
}

func (s *Services) IsXrayRunning() bool { return s.XrayRunning != nil && s.XrayRunning() }

func (s *Services) EnsureInterface(ctx context.Context) error {
	if s.KeeneticEnsureInterface == nil {
		return ErrServiceUnavailable
	}
	return s.KeeneticEnsureInterface(ctx)
}

func (s *Services) WaitKeeneticUDPJobs(ctx context.Context) error {
	if s.KeeneticWaitUDPJobs == nil {
		return ErrServiceUnavailable
	}
	return s.KeeneticWaitUDPJobs(ctx)
}

func (s *Services) ReapplyActiveServer(ctx context.Context, reason string) error {
	if s.ServersReapplyActive == nil {
		return ErrServiceUnavailable
	}
	return s.ServersReapplyActive(ctx, reason)
}

// IPv6 is the keenetic area's IPv6 fact, or state "unknown" (never a
// warning) when that area did not provide it.
func (s *Services) IPv6(ctx context.Context) map[string]any {
	if s.KeeneticIPv6 == nil {
		return keenetic.IPv6Facts{}.Object("")
	}
	return s.KeeneticIPv6(ctx)
}
