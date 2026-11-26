// Package servers ports the 3.x "servers" WebUI area (contract:
// docs/contracts/servers.md): server records, share-link import, Xray config
// generation, activation, quality checks, auto-switch/failover and the
// connection monitor. The record/parser API used by other areas is
// documented in API.md.
package servers

import (
	"context"

	"github.com/BROadmin/broray/internal/compat"
)

func init() { compat.Area("servers", 50, setup) }

func setup(d *compat.Deps) error {
	s := newSvc(d)
	LabelDir = d.Root.Tmp()
	current.Store(s)
	registerOnce.Do(register)
	s.defineActions()
	d.Svc.ServersReapplyActive = s.ReapplyActive
	d.Svc.ServersPreviewActiveConfig = s.PreviewActiveConfig
	d.Svc.ServersPreviewBackupBaseConfig = s.PreviewBackupBaseConfig
	d.Svc.ServersPreparePolicyVPN = s.preparePolicyVPN
	d.Svc.ServersProbePolicyVPN = s.probeAppliedPolicyVPN
	d.Svc.ServersPremiumVPNIdentity = s.premiumVPNIdentity
	d.Svc.ServersPremiumVPNProbe = s.premiumVPNProbe
	d.Svc.ServersWithdrawActive = s.WithdrawActive
	d.Svc.ServersPrepareSourceRemoval = s.PrepareSourceRemoval
	d.Svc.ServersPrepareSourceRefresh = s.PrepareSourceRefresh
	d.Svc.ServersPrepareSourceRefreshCatalog = s.PrepareSourceRefreshCatalog
	d.Svc.ServersReapplySourceRefresh = s.ReapplySourceRefresh
	d.Svc.ServersRenameSourceRefresh = s.RenameSourceRefresh
	d.Svc.ServersApplyPendingSourceProfile = s.ApplyPendingSourceProfile
	d.Svc.ServersValidateSourceDPIProposals = s.ValidateSourceDPIProposals
	d.Svc.ServersInterfaceDescription = s.reserveInterfaceDescription
	registerBackup(s)
	compat.RegisterSnapshot("servers", func(ctx context.Context) (map[string]any, error) { return s.summary(ctx) })
	if !d.Maintenance {
		s.seedAutoSwitch() // 3.x state-seed
		go s.autoSwitchLoop(d.Ctx)
		go s.monitorLoop(d.Ctx)
		go s.reserveLoop(d.Ctx)
	}
	return nil
}
