package servers

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
	"os"
	"sync"
	"time"
)

type dpiSourceValidation struct {
	mu     sync.Mutex
	core   *os.File
	cancel context.CancelFunc
	valid  func(context.Context) bool
}

func (v *dpiSourceValidation) Check(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.core == nil || !v.valid(ctx) {
		return errReserveRuntime
	}
	return nil
}
func (v *dpiSourceValidation) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.core == nil {
		return nil
	}
	v.cancel()
	err := v.core.Close()
	v.core = nil
	return err
}
func (s *svc) ValidateSourceDPIProposals(ctx context.Context, source string, proposals []compat.DPIProposal) (compat.DPIValidation, error) {
	return s.validateSourceDPIProposalsWith(ctx, source, proposals, reserve.Probe)
}
func (s *svc) validateSourceDPIProposalsWith(ctx context.Context, source string, proposals []compat.DPIProposal, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (compat.DPIValidation, error) {
	if !s.reserveRuntimeFence(ctx) || !ValidID(source) || len(proposals) == 0 || len(proposals) > 500 || probe == nil || s.d.App == nil || s.d.App.Xray == nil {
		return nil, errReserveRuntime
	}
	if err := compat.CheckSourceSyncWorkspaces(s.d.Root); err != nil {
		return nil, errors.Join(err, compat.ErrReserveTransactionPending)
	}
	if err := reserve.CheckProbeWorkspaces(s.d.Root); err != nil {
		return nil, errors.Join(err, compat.ErrReserveTransactionPending)
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return nil, errors.Join(err, compat.ErrReserveTransactionPending)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	core, err := reserveRuntimeOpen(s.d.Root.XrayBinary())
	if err != nil {
		cancel()
		return nil, err
	}
	lease := &dpiSourceValidation{core: core, cancel: cancel}
	success := false
	defer func() {
		if !success {
			_ = lease.Close()
		}
	}()
	coreInfo, err := core.Stat()
	if err != nil || coreInfo.Mode()&0111 == 0 {
		return nil, errReserveRuntime
	}
	settings, err := s.reserveRuntimeSettings()
	if err != nil {
		return nil, err
	}
	copied := make([]compat.DPIProposal, len(proposals))
	records := make([]Record, len(proposals))
	images := make([]inactiveImage, len(proposals))
	ids := map[string]bool{}
	total := 0
	for i, p := range proposals {
		total += len(p.Record)
		if len(p.Record) == 0 || len(p.Record) > 1<<20 || total > 16<<20 {
			return nil, errReserveRuntime
		}
		copied[i] = compat.DPIProposal{Path: p.Path, PreviousSHA256: p.PreviousSHA256, Record: bytes.Clone(p.Record)}
		value, e := strictjson.Decode(copied[i].Record)
		r, ok := value.(map[string]any)
		if e != nil || !ok || !s.dpiProposalPath(p.Path, r) {
			return nil, errReserveRuntime
		}
		id := asString(r["id"])
		origin, ok := r["source"].(map[string]any)
		if !ok || origin["type"] != "subscription" || origin["subscriptionId"] != source || ids[id] {
			return nil, errReserveRuntime
		}
		ids[id] = true
		enabled, e := ManagedDPI(r)
		if e != nil || !enabled {
			return nil, errReserveRuntime
		}
		image, _, e := readInactiveImage(p.Path, false)
		if e != nil || image.hash != p.PreviousSHA256 {
			return nil, errReserveRuntime
		}
		images[i] = image
		records[i] = r
	}
	// These dependencies define the probed connection and must stay fixed even
	// after intentional reserve/runtime/catalog mutations begin under the fence.
	lease.valid = func(checkCtx context.Context) bool {
		if !s.reserveRuntimeFence(checkCtx) || !s.reserveRuntimeFence(ctx) {
			return false
		}
		f, e := reserveRuntimeOpen(s.d.Root.XrayBinary())
		if e != nil {
			return false
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil || !os.SameFile(coreInfo, info) || coreInfo.Size() != info.Size() || !coreInfo.ModTime().Equal(info.ModTime()) || coreInfo.Mode() != info.Mode() {
			return false
		}
		st, e := s.reserveRuntimeSettings()
		if e != nil || st != settings || len(copied) != len(proposals) {
			return false
		}
		for i, p := range proposals {
			old := copied[i]
			if p.Path != old.Path || p.PreviousSHA256 != old.PreviousSHA256 || !bytes.Equal(p.Record, old.Record) {
				return false
			}
		}
		return true
	}
	main := s.d.App.Xray
	snapshot := main.RuntimeSnapshot()
	active, err := dpiReadProbeImage(ActiveFile(s.d), 4096)
	if err != nil {
		return nil, err
	}
	config, err := dpiReadProbeImage(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		return nil, err
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return nil, err
	}
	unchanged := func() bool {
		if lease.Check(ctx) != nil || s.d.App == nil || s.d.App.Xray != main || main.RuntimeSnapshot() != snapshot {
			return false
		}
		a, e := dpiReadProbeImage(ActiveFile(s.d), 4096)
		if e != nil || a != active {
			return false
		}
		c, e := dpiReadProbeImage(s.d.Root.XrayConfig(), 2<<20)
		if e != nil || c != config {
			return false
		}
		p, e := readReservePreferences(s.d)
		if e != nil || p.Revision != prefs.Revision {
			return false
		}
		for _, image := range images {
			if !image.unchanged() {
				return false
			}
		}
		pending, e := reserve.TransactionPending(ctx, s.d.Root)
		return e == nil && !pending
	}
	for i, p := range copied {
		if !unchanged() {
			return nil, errReserveRuntime
		}
		sample, e := s.probeDPIProposalWith(ctx, p.Path, p.PreviousSHA256, records[i], probe)
		if errors.Is(e, errReserveProbeCleanup) {
			return nil, errors.Join(e, compat.ErrReserveTransactionPending)
		}
		if e != nil {
			return nil, e
		}
		if !unchanged() || !sample.Verified || sample.ServerID != asString(records[i]["id"]) {
			return nil, errReserveRuntime
		}
		if sample.Verdict == "unhealthy" {
			return nil, compat.ErrDPIProposalUnhealthy
		}
		if sample.Verdict != "healthy" {
			return nil, errReserveRuntime
		}
	}
	if !unchanged() {
		return nil, errReserveRuntime
	}
	success = true
	return lease, nil
}
