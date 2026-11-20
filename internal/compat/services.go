package compat

import (
	"context"
	"errors"
	"net/netip"
	"os"

	"github.com/BROadmin/broray/internal/keenetic"
)

// ErrXrayCandidateRejected means the bound native core completely read and
// rejected candidate bytes. Operational failures must not match this sentinel.
var ErrXrayCandidateRejected = errors.New("XRAY_CANDIDATE_REJECTED")

// Services are the few cross-area calls. Each field is set by its owner
// area in Setup; callers must use the wrapper methods, which return
// ErrServiceUnavailable when the owner did not provide the function.
// Everything else crosses areas through the same files as in 3.x
// (config/interface.json, run/keenetic-status.json, servers/*.json, …).
// DPIProposal carries the exact proposed catalog bytes and the existing image
// they replace. This is an internal service contract, never a public endpoint.
type DPIProposal struct {
	Path           string
	PreviousSHA256 string
	Record         []byte
}

// A validation lease pins connection dependencies until the caller finishes its
// protected catalog transaction. Close must run on every success/error path.
type DPIValidation interface {
	Check(context.Context) error
	Close() error
}

var ErrDPIProposalUnhealthy = errors.New("DPI_CONNECTION_FAILED")

type Services struct {
	// routes area: decorate an active server configuration with saved policy.
	// Health probes use the pure generator and do not call this transform.
	RoutesConfigureXray func(context.Context, []byte) ([]byte, error)
	// xray area: validate config bytes with `xray -test`, install them
	// atomically at Root.XrayConfig(), (re)start Xray, wait for the SOCKS
	// inbound, roll back to the previous config on failure.
	XrayApplyConfig func(ctx context.Context, config []byte) error
	// Validate candidate bytes with the actual core without applying or starting it.
	XrayValidateConfig       func(context.Context, []byte) error
	XrayValidateStagedConfig func(context.Context, []byte, *os.File) error
	// xray area: is the supervised Xray running.
	XrayRunning func() bool

	// keenetic area: create or repair the owned ProxyN → Xray SOCKS
	// interface (receipt config/interface.json), save and verify.
	KeeneticEnsureInterface       func(ctx context.Context) error
	KeeneticVerifyInterface       func(context.Context, string) (string, error)
	KeeneticVerifyInterfaceTarget func(context.Context, string) (OwnedInterfaceTarget, error)
	// Owned running/startup configuration only; no claim that Xray or link is up.
	KeeneticVerifyInterfaceConfiguration func(context.Context, string) (OwnedInterfaceTarget, error)
	KeeneticOrdinaryDescription          func() (string, error)
	KeeneticPreviewOrdinaryDescription   func(string) (string, error)
	KeeneticSyncOwnedDescription         func(context.Context) error
	// Optional description override supplied by the servers area; empty is ordinary mode.
	ServersInterfaceDescription func() (string, error)
	// keenetic area: the cached read-only IPv6 fact
	// {state, wan, lanPrefix, warning, checkedAt} (keenetic.IPv6Facts.Object).
	KeeneticIPv6 func(ctx context.Context) map[string]any
	// keenetic area: finish active UDP checks and remove their temporary routes before daemon exit.
	KeeneticWaitUDPJobs func(ctx context.Context) error

	// servers area: regenerate and apply the Xray config of the active
	// server (after its record changed, e.g. by a subscription refresh).
	ServersReapplyActive func(ctx context.Context, reason string) error
	// Preview validates the active server without applying it; nil means no active server.
	ServersPreviewActiveConfig     func(context.Context) ([]byte, error)
	ServersPreviewBackupBaseConfig func(context.Context, *BackupStage) ([]byte, error)
	// servers area: withdraw an active node removed by its subscription. Caller
	// holds the global fence. Rollback must restore catalog records first, then
	// call the returned undo even when withdrawal returned an error.
	ServersWithdrawActive func(context.Context) (func(context.Context) error, error)
	// Prepare runtime, reserve roster and display state before source catalog removal.
	ServersPrepareSourceRemoval func(context.Context, string, []string, []string) (string, error)
	// Prepare vanished members while restricting replacement to the refreshed source.
	ServersPrepareSourceRefresh        func(context.Context, string, []string, []string) (string, error)
	ServersPrepareSourceRefreshCatalog func(context.Context, string, []string) error
	ServersReapplySourceRefresh        func(context.Context, string, string, string) (string, error)
	ServersValidateSourceDPIProposals  func(context.Context, string, []DPIProposal) (DPIValidation, error)
}

// PreviewOrdinaryDescription computes the owner's canonical description without
// changing the active marker or applying any router metadata.
func (s *Services) PreviewOrdinaryDescription(name string) (string, error) {
	if s.KeeneticPreviewOrdinaryDescription == nil {
		return "", ErrServiceUnavailable
	}
	return s.KeeneticPreviewOrdinaryDescription(name)
}

func (s *Services) OrdinaryInterfaceDescription() (string, error) {
	if s.KeeneticOrdinaryDescription == nil {
		return "", ErrServiceUnavailable
	}
	return s.KeeneticOrdinaryDescription()
}

func (s *Services) SyncOwnedInterfaceDescription(ctx context.Context) error {
	if s.KeeneticSyncOwnedDescription == nil {
		return ErrServiceUnavailable
	}
	return s.KeeneticSyncOwnedDescription(ctx)
}

// OwnedInterfaceTarget is a fresh router ownership/readback result, not proof
// that the upstream socket belongs to Xray or that Internet traffic succeeds.
type OwnedInterfaceTarget struct {
	Name            string
	Upstream        netip.AddrPort
	DescriptionLine string
}

func (s *Services) VerifyOwnedInterfaceConfiguration(ctx context.Context, description string) (OwnedInterfaceTarget, error) {
	if s.KeeneticVerifyInterfaceConfiguration == nil {
		return OwnedInterfaceTarget{}, ErrServiceUnavailable
	}
	return s.KeeneticVerifyInterfaceConfiguration(ctx, description)
}

func (s *Services) VerifyOwnedInterfaceTarget(ctx context.Context, description string) (OwnedInterfaceTarget, error) {
	if s.KeeneticVerifyInterfaceTarget == nil {
		return OwnedInterfaceTarget{}, ErrServiceUnavailable
	}
	return s.KeeneticVerifyInterfaceTarget(ctx, description)
}

func (s *Services) ConfigureRouting(ctx context.Context, cfg []byte) ([]byte, error) {
	if s.RoutesConfigureXray == nil {
		return cfg, nil
	}
	return s.RoutesConfigureXray(ctx, cfg)
}

var ErrServiceUnavailable = errors.New("SERVICE_UNAVAILABLE: Модуль BROray недоступен.")

func (s *Services) PreviewActiveConfig(ctx context.Context) ([]byte, error) {
	if s.ServersPreviewActiveConfig == nil {
		return nil, ErrServiceUnavailable
	}
	return s.ServersPreviewActiveConfig(ctx)
}

func (s *Services) ApplyXrayConfig(ctx context.Context, cfg []byte) error {
	if s.XrayApplyConfig == nil {
		return ErrServiceUnavailable
	}
	return s.XrayApplyConfig(ctx, cfg)
}

func (s *Services) ValidateXrayStagedConfig(ctx context.Context, cfg []byte, assets *os.File) error {
	if s.XrayValidateStagedConfig == nil {
		return ErrServiceUnavailable
	}
	return s.XrayValidateStagedConfig(ctx, cfg, assets)
}

func (s *Services) ValidateXrayConfig(ctx context.Context, cfg []byte) error {
	if s.XrayValidateConfig == nil {
		return ErrServiceUnavailable
	}
	return s.XrayValidateConfig(ctx, cfg)
}

func (s *Services) IsXrayRunning() bool { return s.XrayRunning != nil && s.XrayRunning() }

func (s *Services) EnsureInterface(ctx context.Context) error {
	if s.KeeneticEnsureInterface == nil {
		return ErrServiceUnavailable
	}
	return s.KeeneticEnsureInterface(ctx)
}

func (s *Services) InterfaceDescription() (string, error) {
	if s.ServersInterfaceDescription == nil {
		return "", nil
	}
	return s.ServersInterfaceDescription()
}

func (s *Services) VerifyOwnedInterface(ctx context.Context, description string) (string, error) {
	if s.KeeneticVerifyInterface == nil {
		return "", ErrServiceUnavailable
	}
	return s.KeeneticVerifyInterface(ctx, description)
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

func (s *Services) WithdrawActiveServer(ctx context.Context) (func(context.Context) error, error) {
	if s.ServersWithdrawActive == nil {
		return nil, ErrServiceUnavailable
	}
	return s.ServersWithdrawActive(ctx)
}

// IPv6 is the keenetic area's IPv6 fact, or state "unknown" (never a
// warning) when that area did not provide it.
func (s *Services) IPv6(ctx context.Context) map[string]any {
	if s.KeeneticIPv6 == nil {
		return keenetic.IPv6Facts{}.Object("")
	}
	return s.KeeneticIPv6(ctx)
}

func (s *Services) PrepareSourceRemoval(ctx context.Context, sourceID string, removedIDs, candidates []string) (string, error) {
	if s.ServersPrepareSourceRemoval == nil {
		return "", ErrServiceUnavailable
	}
	return s.ServersPrepareSourceRemoval(ctx, sourceID, removedIDs, candidates)
}

func (s *Services) PrepareSourceRefresh(ctx context.Context, sourceID string, removedIDs, candidates []string) (string, error) {
	if s.ServersPrepareSourceRefresh == nil {
		return "", ErrServiceUnavailable
	}
	return s.ServersPrepareSourceRefresh(ctx, sourceID, removedIDs, candidates)
}

func (s *Services) PrepareSourceRefreshCatalog(ctx context.Context, sourceID string, changedIDs []string) error {
	if s.ServersPrepareSourceRefreshCatalog == nil {
		return ErrServiceUnavailable
	}
	return s.ServersPrepareSourceRefreshCatalog(ctx, sourceID, changedIDs)
}
func (s *Services) ReapplySourceRefresh(ctx context.Context, sourceID, id, previousName string) (string, error) {
	if s.ServersReapplySourceRefresh == nil {
		return "", ErrServiceUnavailable
	}
	return s.ServersReapplySourceRefresh(ctx, sourceID, id, previousName)
}

func (s *Services) ValidateSourceDPIProposals(ctx context.Context, source string, proposals []DPIProposal) (DPIValidation, error) {
	if s.ServersValidateSourceDPIProposals == nil {
		return nil, ErrServiceUnavailable
	}
	return s.ServersValidateSourceDPIProposals(ctx, source, proposals)
}

// PreviewBackupBaseConfig uses the final staged catalog with live generator
// settings. The routes owner decorates and validates it on staged assets.
func (s *Services) PreviewBackupBaseConfig(ctx context.Context, stage *BackupStage) ([]byte, error) {
	if s.ServersPreviewBackupBaseConfig == nil {
		return nil, ErrServiceUnavailable
	}
	return s.ServersPreviewBackupBaseConfig(ctx, stage)
}
