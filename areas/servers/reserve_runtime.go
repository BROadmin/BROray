package servers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
	"golang.org/x/sys/unix"
)

var errReserveRuntime = errors.New("RESERVE_RUNTIME_UNCONFIRMED")

type reserveRuntimePlan struct {
	owner                                       *svc
	node                                        reserve.Node
	revision, recordSHA, settingsSHA, configSHA string
	config                                      []byte
	endpoint                                    netip.AddrPort
	outsideRoster                               bool
}

type reserveActiveBinding struct {
	plan                    reserveRuntimePlan
	process                 *xray.Process
	snapshot                xray.RuntimeSnapshot
	target                  reserve.ProbeTarget
	proofIdentity, identity string
}

func (s *svc) prepareReserveRuntime(ctx context.Context, id string, endpoint netip.AddrPort) (reserveRuntimePlan, error) {
	return s.prepareReserveRuntimeFor(ctx, id, endpoint, false)
}

func (s *svc) prepareReserveRuntimeFor(ctx context.Context, id string, endpoint netip.AddrPort, candidate bool) (reserveRuntimePlan, error) {
	if !s.reserveRuntimeFence(ctx) || !ValidID(id) {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	prefs, err := readReservePreferences(s.d)
	outside := !reserveRosterContains(prefs, id)
	if err != nil || !prefs.Configured || outside && !candidate &&
		(prefs.Emergency == nil || prefs.Emergency.ServerID != id) {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	record, err := reserveRuntimeFile(RecordPath(s.d, id), 1<<20)
	if err != nil {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	v, err := strictjson.Decode(record)
	m, ok := v.(map[string]any)
	if err != nil || !ok || m["id"] != id {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	settings, err := s.reserveRuntimeSettings()
	if err != nil {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	base, err := XrayConfig(Record(m), s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	routed, err := s.d.Svc.ConfigureRouting(ctx, base)
	if err != nil {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	config, err := reserve.AttachHealthInbound(routed, endpoint)
	if err != nil {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	plan := reserveRuntimePlan{owner: s, node: reserve.Node{ID: id, Fingerprint: compat.SHA256Hex(base)},
		revision: prefs.Revision, recordSHA: compat.SHA256Hex(record), settingsSHA: settings,
		configSHA: compat.SHA256Hex(config), config: config, endpoint: endpoint, outsideRoster: outside}
	if outside && !candidate && prefs.Emergency.Fingerprint != plan.node.Fingerprint {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	if !s.reservePlanInputs(ctx, plan, false, false) {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	return plan, nil
}

func (s *svc) bindReserveRuntime(ctx context.Context, plan reserveRuntimePlan, previous *reserveActiveBinding) (reserveActiveBinding, error) {
	if !s.reservePlanInputs(ctx, plan, false, true) || s.d.App == nil || s.d.App.Xray == nil {
		return reserveActiveBinding{}, errReserveRuntime
	}
	p := s.d.App.Xray
	snapshot := p.RuntimeSnapshot()
	if !snapshot.Wanted || !snapshot.Running || snapshot.ExitConfirmed || snapshot.PID <= 0 ||
		snapshot.Generation == 0 || snapshot.Intent == 0 || snapshot.Generation == ^uint64(0) || snapshot.Intent == ^uint64(0) {
		return reserveActiveBinding{}, errReserveRuntime
	}
	identity, err := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
	if err != nil {
		return reserveActiveBinding{}, errReserveRuntime
	}
	target := reserve.ProbeTarget{Process: identity, ConfigPath: s.d.Root.XrayConfig(), ConfigSHA256: plan.configSHA, Endpoint: plan.endpoint}
	proof, err := reserve.VerifyProbeTarget(target)
	if err != nil || !s.reservePlanInputs(ctx, plan, false, true) || s.d.App.Xray != p || p.RuntimeSnapshot() != snapshot {
		return reserveActiveBinding{}, errReserveRuntime
	}
	b := reserveActiveBinding{plan: plan, process: p, snapshot: snapshot, target: target, proofIdentity: proof}
	if previous != nil && previous.process == p && previous.snapshot.Intent == snapshot.Intent &&
		previous.snapshot.Wanted && sameReservePlan(previous.plan, plan) && len(previous.identity) == 64 {
		b.identity = previous.identity
	} else {
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return reserveActiveBinding{}, errReserveRuntime
		}
		b.identity = hex.EncodeToString(token[:])
	}
	return b, nil
}

func (s *svc) probeReserveRuntime(ctx context.Context, binding reserveActiveBinding) reserve.Sample {
	return s.probeReserveRuntimeWith(ctx, binding, reserve.Probe)
}

func (s *svc) probeReserveRuntimeWith(ctx context.Context, binding reserveActiveBinding, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) reserve.Sample {
	started := time.Now()
	unknown := func() reserve.Sample {
		return reserve.Sample{ServerID: binding.plan.node.ID, Fingerprint: binding.plan.node.Fingerprint,
			Verdict: "unknown", StartedAt: started, FinishedAt: time.Now()}
	}
	valid := func() bool {
		return len(binding.identity) == 64 && binding.process != nil && s.d.App != nil && s.d.App.Xray == binding.process &&
			s.reservePlanInputs(ctx, binding.plan, true, true) && s.reserveCoreMatches(binding.target.Process)
	}
	if !valid() {
		return unknown()
	}
	confirmedExit := func() bool {
		after := binding.process.RuntimeSnapshot()
		return after.ConfirmsUnexpectedExit(binding.snapshot) && valid() && binding.process.RuntimeSnapshot() == after
	}
	failure := func() reserve.Sample {
		return reserve.Sample{ServerID: binding.plan.node.ID, Fingerprint: binding.plan.node.Fingerprint,
			Identity: binding.identity, Verdict: "unhealthy", Verified: true, StartedAt: started, FinishedAt: time.Now()}
	}
	if confirmedExit() {
		return failure()
	}
	if binding.process.RuntimeSnapshot() != binding.snapshot {
		return unknown()
	}
	proof, err := reserve.VerifyProbeTarget(binding.target)
	if err != nil || proof != binding.proofIdentity || probe == nil {
		return unknown()
	}
	out := probe(ctx, binding.target)
	if !valid() {
		return unknown()
	}
	if confirmedExit() {
		return failure()
	}
	if binding.process.RuntimeSnapshot() != binding.snapshot {
		return unknown()
	}
	proof, err = reserve.VerifyProbeTarget(binding.target)
	finished := time.Now()
	if err != nil || proof != binding.proofIdentity || out.Identity != proof ||
		(out.Verdict != "healthy" && out.Verdict != "unhealthy") || out.StartedAt.Before(started) ||
		out.FinishedAt.Before(out.StartedAt) || out.FinishedAt.After(finished) ||
		!valid() || binding.process.RuntimeSnapshot() != binding.snapshot {
		return unknown()
	}
	return reserve.Sample{ServerID: binding.plan.node.ID, Fingerprint: binding.plan.node.Fingerprint, Identity: binding.identity,
		Verdict: out.Verdict, Verified: true, StartedAt: started, FinishedAt: finished}
}

func (s *svc) reserveRuntimeFence(ctx context.Context) bool {
	return ctx.Err() == nil && compat.FenceHeld(ctx) && s.d.Queue != nil && (!s.d.Queue.Paused() || ctx.Value(reserveModeContextKey{}) == true) && !s.coreMaintenanceBlocked()
}

func reserveRosterContains(p reservePreferencesSnapshot, id string) bool {
	if id == p.PrimaryServerID {
		return true
	}
	for _, backup := range p.BackupServerIDs {
		if backup == id {
			return true
		}
	}
	return false
}

func sameReservePlan(a, b reserveRuntimePlan) bool {
	return a.owner == b.owner && a.node == b.node && a.revision == b.revision && a.recordSHA == b.recordSHA &&
		a.settingsSHA == b.settingsSHA && a.configSHA == b.configSHA && a.endpoint == b.endpoint &&
		a.outsideRoster == b.outsideRoster
}

// Keep missing settings distinct from an empty file and cover the daemon's
// fallback defaults without running Version or a network lookup per sample.
func (s *svc) reserveRuntimeSettings() (string, error) {
	data, err := reserveRuntimeFile(settingsFile(s.d), 1<<20)
	exists := !errors.Is(err, os.ErrNotExist)
	if err != nil && exists {
		return "", errReserveRuntime
	}
	if exists {
		v, err := strictjson.Decode(data)
		if _, ok := v.(map[string]any); err != nil || !ok {
			return "", errReserveRuntime
		}
	}
	defaults := []any{nil, nil, nil}
	if s.d.App != nil {
		defaults = []any{s.d.App.Cfg.ListenAddress, s.d.App.Cfg.SocksPort, s.d.App.Cfg.LogLevel}
	}
	b, err := json.Marshal([]any{exists, compat.SHA256Hex(data), defaults})
	if err != nil {
		return "", errReserveRuntime
	}
	return compat.SHA256Hex(b), nil
}

func (s *svc) reservePlanInputs(ctx context.Context, p reserveRuntimePlan, enabled, active bool) bool {
	return s.reserveRuntimeFence(ctx) && s.reservePlanCurrent(p, enabled, active)
}

func (s *svc) reservePlanCurrent(p reserveRuntimePlan, enabled, active bool) bool {
	if p.owner != s || !ValidID(p.node.ID) {
		return false
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || prefs.Revision != p.revision || !prefs.Configured ||
		p.outsideRoster == reserveRosterContains(prefs, p.node.ID) || (enabled && !prefs.Enabled) {
		return false
	}
	record, err := reserveRuntimeFile(RecordPath(s.d, p.node.ID), 1<<20)
	if err != nil || compat.SHA256Hex(record) != p.recordSHA {
		return false
	}
	settings, err := s.reserveRuntimeSettings()
	if err != nil || settings != p.settingsSHA {
		return false
	}
	if active {
		if prefs.Enabled && p.outsideRoster &&
			(prefs.Emergency == nil || prefs.Emergency.ServerID != p.node.ID || prefs.Emergency.Fingerprint != p.node.Fingerprint) {
			return false
		}
		id, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
		if err != nil || strings.TrimSpace(string(id)) != p.node.ID {
			return false
		}
		config, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if err != nil || compat.SHA256Hex(config) != p.configSHA {
			return false
		}
	}
	return true
}

func reserveRuntimeOpen(path string) (*os.File, error) {
	parent, err := platform.OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errReserveRuntime
	}
	return f, nil
}

func reserveRuntimeFile(path string, limit int64) ([]byte, error) {
	f, err := reserveRuntimeOpen(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() > limit {
		return nil, errReserveRuntime
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errReserveRuntime
	}
	return b, nil
}

// A core replacement must invalidate even an observed dead child. Do not read
// the entire binary for each probe; compare its held regular-file identity.
func (s *svc) reserveCoreMatches(p reserve.ProcessIdentity) bool {
	if p.Executable != s.d.Root.XrayBinary() {
		return false
	}
	f, err := reserveRuntimeOpen(p.Executable)
	if err != nil {
		return false
	}
	defer f.Close()
	var st unix.Stat_t
	return unix.Fstat(int(f.Fd()), &st) == nil && st.Mode&0111 != 0 && uint64(st.Dev) == p.Device && st.Ino == p.Inode
}
