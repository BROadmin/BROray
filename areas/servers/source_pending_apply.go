package servers

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

type pendingSourceProof struct {
	configSHA, candidateSHA string
	previousRecord          []byte
	recoveryAuthority       string
	op                      *compat.Operation
	probe                   func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome
}

func (s *svc) ApplyPendingSourceProfile(ctx context.Context, intent compat.SourceProfileApply, op *compat.Operation) error {
	return s.applyPendingSourceProfile(ctx, intent, op, reserve.Probe)
}

func (s *svc) applyPendingSourceProfile(ctx context.Context, intent compat.SourceProfileApply,
	op *compat.Operation, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) error {
	sourceID, serverID := intent.SourceID, intent.ServerID
	if !s.reserveRuntimeFence(ctx) || !ValidID(serverID) || probe == nil {
		return errReserveRuntime
	}
	description, err := s.reserveInterfaceDescription()
	if err != nil {
		return err
	}
	if description == "" {
		description, err = s.d.Svc.PreviewOrdinaryDescription(intent.PreviousName)
		if err != nil || description == "" || description == "BROray - mix-auto" {
			return errReserveRuntime
		}
	}
	proof := &pendingSourceProof{configSHA: intent.ConfigSHA256, candidateSHA: intent.CandidateSHA256,
		previousRecord: bytes.Clone(intent.PreviousRecord), recoveryAuthority: intent.RecoveryAuthority, op: op, probe: probe}
	nativeCommitted := false
	_, err = s.reconcileSourceOwner(ctx, []string{serverID}, true, false, func(ctx context.Context) (string, error) {
		if err := s.replaceSourceCandidateDescription(ctx, sourceID, serverID, serverID, true, description, false, proof); err != nil {
			return "", err
		}
		nativeCommitted = true
		return serverID, nil
	})
	if err != nil && nativeCommitted {
		return errors.Join(compat.ErrReserveTransactionPending, err)
	}
	return err
}

func (s *svc) previousPendingRecord(sourceID, serverID string, raw []byte) (Record, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, compat.ErrXrayCandidateRejected
	}
	value, err := strictjson.Decode(raw)
	record, ok := value.(map[string]any)
	if err != nil || !ok || record["id"] != serverID || Validate(Record(record), serverID) != nil {
		return nil, compat.ErrXrayCandidateRejected
	}
	source, _ := record["source"].(map[string]any)
	if source["type"] != "subscription" || source["subscriptionId"] != sourceID {
		return nil, compat.ErrXrayCandidateRejected
	}
	return Record(record), nil
}

func (s *svc) proveEmergencySourcePredecessor(ctx context.Context, sourceID, id, fingerprint string, previous, applied []byte) error {
	record, err := s.previousPendingRecord(sourceID, id, previous)
	if err != nil {
		return err
	}
	base, err := XrayConfig(record, s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil || compat.SHA256Hex(base) != fingerprint {
		return compat.ErrXrayCandidateRejected
	}
	routed, err := s.d.Svc.ConfigureRouting(ctx, base)
	if err != nil {
		return err
	}
	if endpoint, ok := reserve.DiagnosticEndpoint(applied); ok {
		routed, err = reserve.AttachHealthInbound(routed, endpoint)
	}
	if err != nil || !bytes.Equal(routed, applied) {
		return compat.ErrXrayCandidateRejected
	}
	return nil
}

func waitPendingSourceReady(ctx context.Context, current, ready func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for current() {
		if ready() && current() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(40 * time.Millisecond):
		}
	}
	return false
}

func (s *svc) pendingSourceVPNCheck(ctx context.Context, config []byte, current func() bool, proof *pendingSourceProof) compat.VPNCheck {
	endpoint, ok := reserve.DiagnosticEndpoint(config)
	if !ok {
		return compat.VPNUnconfirmed
	}
	process := s.d.App.Xray
	snapshot := process.RuntimeSnapshot()
	if !waitPendingSourceReady(ctx, current, func() bool {
		processID, err := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
		if err != nil {
			return false
		}
		_, err = reserve.VerifyProbeTarget(reserve.ProbeTarget{Process: processID,
			ConfigPath: s.d.Root.XrayConfig(), ConfigSHA256: compat.SHA256Hex(config), Endpoint: endpoint})
		return err == nil
	}) {
		return compat.VPNUnconfirmed
	}
	check := s.probeAppliedPolicyVPNWith(ctx, config, proof.probe)
	if !current() {
		return compat.VPNUnconfirmed
	}
	return check
}
