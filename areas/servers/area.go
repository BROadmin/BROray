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
	registerBackup(s)
	compat.RegisterSnapshot("servers", func(ctx context.Context) (map[string]any, error) { return s.summary(ctx) })
	if !d.Maintenance {
		s.seedAutoSwitch() // 3.x state-seed
		go s.autoSwitchLoop(d.Ctx)
		go s.monitorLoop(d.Ctx)
	}
	return nil
}
