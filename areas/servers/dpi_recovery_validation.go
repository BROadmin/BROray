package servers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
	mxray "github.com/BROadmin/broray/modules/xray"
)

var errDPIRecoveryValidation = errors.New("DPI_RECOVERY_VALIDATION_UNCONFIRMED")

// RecoverDPIProfile validates exact recovered images with the installed core
// before releasing their journal. The caller retains the application recovery
// fence and actual daemon lock, before starting areas or the main child. This
// cold configuration proof does not replace live activation's traffic proof.
func RecoverDPIProfile(ctx context.Context, d *compat.Deps, guard *platform.Lock) error {
	if d == nil {
		return errDPIRecoveryValidation
	}
	return newSvc(d).recoverDPIProfile(ctx, guard)
}

func (s *svc) recoverDPIProfile(ctx context.Context, guard *platform.Lock) error {
	if guard == nil || !guard.Owns(s.d.Root.DaemonLock()) || !s.reserveRuntimeFence(ctx) || s.d.App == nil || s.d.App.Xray == nil {
		return errDPIRecoveryValidation
	}
	main := s.d.App.Xray
	snapshot := main.RuntimeSnapshot()
	if snapshot.Running || snapshot.Wanted {
		return errDPIRecoveryValidation
	}
	j, err := reserve.OpenProfileJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	return j.Recover(ctx, guard, func(ctx context.Context, record reserve.ProfileTransactionRecord) error {
		validMain := func() bool {
			return guard.Owns(s.d.Root.DaemonLock()) && s.reserveRuntimeFence(ctx) && s.d.App != nil &&
				s.d.App.Xray == main && main.RuntimeSnapshot() == snapshot
		}
		return s.verifyRecoveredDPIProfile(ctx, record, validMain)
	})
}

func (s *svc) verifyRecoveredDPIProfile(ctx context.Context, r reserve.ProfileTransactionRecord, validMain func() bool) error {
	decode := func(image reserve.TransactionImage) (Record, error) {
		v, err := strictjson.Decode(image.Data)
		m, ok := v.(map[string]any)
		if err != nil || !ok || m["id"] != r.ServerID || Validate(m, "DPI recovery") != nil {
			return nil, errDPIRecoveryValidation
		}
		return m, nil
	}
	previous, err := decode(r.Previous.Record)
	if err != nil {
		return errDPIRecoveryValidation
	}
	next, err := decode(r.Next.Record)
	if err != nil {
		return errDPIRecoveryValidation
	}
	previousEnabled, ep := ManagedDPI(previous)
	nextEnabled, en := ManagedDPI(next)
	if ep != nil || en != nil || previousEnabled == nextEnabled {
		return errDPIRecoveryValidation
	}
	expected, err := prepareDPIRecord(previous, nextEnabled)
	if err != nil || !dpiJSONEqual(expected, next) {
		return errDPIRecoveryValidation
	}
	want, rec := r.Previous, previous
	if r.Phase == "committed" {
		want, rec = r.Next, next
	}
	core, err := reserveRuntimeOpen(s.d.Root.XrayBinary())
	if err != nil {
		return errDPIRecoveryValidation
	}
	defer core.Close()
	coreInfo, err := core.Stat()
	if err != nil || coreInfo.Mode()&0111 == 0 {
		return errDPIRecoveryValidation
	}
	valid := func() bool {
		if !validMain() {
			return false
		}
		visible, err := reserveRuntimeOpen(s.d.Root.XrayBinary())
		if err != nil {
			return false
		}
		info, err := visible.Stat()
		visible.Close()
		if err != nil || !os.SameFile(coreInfo, info) || info.Size() != coreInfo.Size() || info.Mode() != coreInfo.Mode() || !info.ModTime().Equal(coreInfo.ModTime()) {
			return false
		}
		for _, file := range []struct {
			path  string
			image reserve.TransactionImage
			limit int64
		}{
			{RecordPath(s.d, r.ServerID), want.Record, 1 << 20}, {s.d.Root.XrayConfig(), want.Config, 2 << 20},
			{ActiveFile(s.d), r.Context.Active, 4096}, {reservePreferencesFile(s.d), r.Context.Preferences, 4096},
			{settingsFile(s.d), r.Context.Settings, 1 << 20}, {s.d.Path("config", "interface.json"), r.Context.Receipt, 64 << 10},
		} {
			image, err := dpiReadProbeImage(file.path, file.limit)
			if err != nil || image.present != file.image.Exists || (image.present && image.hash != file.image.SHA256) {
				return false
			}
		}
		return true
	}
	if !valid() {
		return errDPIRecoveryValidation
	}
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	version, versionErr := mxray.Version(versionCtx, s.d.Root.XrayBinary())
	cancel()
	if versionErr != nil || CheckCoreSupport(previous, version) != nil || CheckCoreSupport(next, version) != nil || !valid() {
		return errDPIRecoveryValidation
	}
	base, err := XrayConfig(rec, GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "none"}, "DPI recovery")
	if err != nil {
		return errDPIRecoveryValidation
	}
	if r.Context.Active.Exists && strings.TrimSpace(string(r.Context.Active.Data)) == r.ServerID {
		outbound, err := Outbound(normalize(rec))
		if err != nil || !dpiRecoveredProxyMatches(want.Config.Data, outbound) {
			return errDPIRecoveryValidation
		}
	}
	configs := [][]byte{base}
	if want.Config.Exists {
		configs = append(configs, want.Config.Data)
	}
	return s.testDPIRecoveryConfigs(ctx, configs, valid)
}

func dpiRecoveredProxyMatches(raw []byte, expected any) bool {
	v, err := strictjson.Decode(raw)
	cfg, ok := v.(map[string]any)
	if err != nil || !ok {
		return false
	}
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok || len(outbounds) == 0 || !dpiJSONEqual(outbounds[0], expected) {
		return false
	}
	for _, outbound := range outbounds[1:] {
		m, ok := outbound.(map[string]any)
		if !ok || m["tag"] == "proxy" {
			return false
		}
	}
	return true
}

// Use the same retained-parent temporary ownership primitives as native probes.
// No process is launched through the main supervisor, and no network probe is
// required to restore a previously accepted configuration after a cold boot.
func (s *svc) testDPIRecoveryConfigs(ctx context.Context, configs [][]byte, valid func() bool) (resultErr error) {
	if s.xrayTest == nil || valid == nil || len(configs) < 1 || len(configs) > 2 || !valid() {
		return errDPIRecoveryValidation
	}
	parent, err := platform.OpenDirNoFollow(s.d.Root.Tmp())
	if err != nil {
		return errDPIRecoveryValidation
	}
	defer parent.Close()
	name, err := platform.MkdirTempAtNoFollow(parent, "reserve-probe-", 0700)
	if err != nil {
		return errReserveProbeCleanup
	}
	held, err := platform.OpenDirAtNoFollow(parent, name)
	if err != nil {
		return errReserveProbeCleanup
	}
	defer held.Close()
	path := filepath.Join(s.d.Root.Tmp(), name)
	cleanupConfirmed := true
	var files []*os.File
	var imageChecks []func() bool
	created := map[string]bool{}
	defer func() {
		for _, file := range files {
			file.Close()
		}
	}()
	defer func() {
		for _, check := range imageChecks {
			if !check() {
				cleanupConfirmed = false
			}
		}
		if cleanupConfirmed {
			names, err := held.Readdirnames(len(created) + 1)
			if (err != nil && !errors.Is(err, io.EOF)) || len(names) != len(created) {
				cleanupConfirmed = false
			}
			for _, name := range names {
				if !created[name] {
					cleanupConfirmed = false
				}
			}
		}
		if !cleanupConfirmed {
			resultErr = errors.Join(resultErr, errReserveProbeCleanup)
			return
		}
		if err := platform.RemoveOwnedDirTreeAtNoFollow(parent, name, held); err != nil {
			resultErr = errors.Join(resultErr, errReserveProbeCleanup)
		}
		if !valid() {
			resultErr = errors.Join(resultErr, errDPIRecoveryValidation)
		}
	}()
	for i, config := range configs {
		if !valid() || !reserveProbeDirectoryMatches(s.d.Root.Tmp(), parent) || !reserveProbeDirectoryMatches(path, held) {
			return errDPIRecoveryValidation
		}
		filename := []string{"profile.json", "main.json"}[i]
		if err := platform.WriteNewRegularAtNoFollow(held, filename, config, 0600); err != nil {
			cleanupConfirmed = false
			return errDPIRecoveryValidation
		}
		created[filename] = true
		configPath := filepath.Join(path, filename)
		file, err := reserveRuntimeOpen(configPath)
		if err != nil {
			cleanupConfirmed = false
			return errDPIRecoveryValidation
		}
		files = append(files, file)
		info, err := file.Stat()
		if err != nil {
			cleanupConfirmed = false
			return errDPIRecoveryValidation
		}
		unchanged := func() bool {
			visible, err := reserveRuntimeOpen(configPath)
			if err != nil {
				return false
			}
			defer visible.Close()
			current, err := visible.Stat()
			if err != nil || !os.SameFile(info, current) || current.Mode() != info.Mode() || current.Size() != int64(len(config)) {
				return false
			}
			raw, err := io.ReadAll(io.LimitReader(visible, int64(len(config))+1))
			return err == nil && bytes.Equal(raw, config)
		}
		imageChecks = append(imageChecks, unchanged)
		if !unchanged() {
			cleanupConfirmed = false
			return errDPIRecoveryValidation
		}
		validationErr := s.xrayTest(ctx, configPath)
		if !unchanged() {
			cleanupConfirmed = false
			return errDPIRecoveryValidation
		}
		if validationErr != nil {
			return errDPIRecoveryValidation
		}
		if !valid() || !reserveProbeDirectoryMatches(s.d.Root.Tmp(), parent) || !reserveProbeDirectoryMatches(path, held) {
			return errDPIRecoveryValidation
		}
	}
	return nil
}
