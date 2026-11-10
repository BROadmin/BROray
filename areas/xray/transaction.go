package xray

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	mxray "github.com/BROadmin/broray/modules/xray"
	"golang.org/x/sys/unix"
)

func coreFileInfo(path string) (os.FileInfo, bool) {
	st, err := os.Lstat(path)
	return st, err == nil && st.Mode().IsRegular()
}

func coreSHA256(path string) string {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return ""
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > mxray.BinaryLimit {
		return ""
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func copyCore(source, dest, want string) error {
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	in := os.NewFile(uintptr(fd), source)
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > mxray.BinaryLimit {
		return errors.New("XRAY_BACKUP_INVALID")
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dest)
		}
	}()
	n, err := io.Copy(out, io.LimitReader(in, mxray.BinaryLimit+1))
	if err != nil {
		return err
	}
	if n != st.Size() {
		return errors.New("XRAY_BACKUP_SIZE_MISMATCH")
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if want == "" || coreSHA256(dest) != want {
		return errors.New("XRAY_BACKUP_DIGEST_MISMATCH")
	}
	ok = true
	return nil
}

func (s *service) prepareCore(ctx context.Context, inst *mxray.Installer, archive *mxray.Archive, tag string) (string, string, error) {
	candidate, version, err := inst.ExtractArchive(ctx, archive)
	if err != nil {
		return "", "", err
	}
	if version != strings.TrimPrefix(tag, "v") {
		_ = os.Remove(candidate)
		return "", "", jerr("Версия загруженного Xray не совпадает с выбранной.")
	}
	if cfg := s.d.Root.XrayConfig(); fileExists(cfg) {
		if out, ok := s.runTest(ctx, candidate, cfg); !ok {
			_ = os.Remove(candidate)
			return "", "", &jobError{msg: "Новая версия Xray несовместима с действующей конфигурацией BROray.", details: tailLines(out, 20)}
		}
	}
	return candidate, version, nil
}

func (s *service) installStaged(ctx context.Context, op *compat.Operation, log *jobLog, mode string, req installRequest, inst *mxray.Installer, archive *mxray.Archive, tag, previous string, oldSize int64) (result map[string]any, resultErr error) {
	before, err := s.readMaintenance()
	if err != nil {
		return nil, jerr("Повреждён журнал обслуживания Xray; установка не начата.")
	}
	if before.Status == "installing" || before.Status == "removing" {
		return nil, jerr("Сначала завершите восстановление предыдущей операции Xray.")
	}
	bin := s.d.Root.XrayBinary()
	if before.Status == "interrupted" && before.Mode == "ram-rollback" && !exists(bin) && !exists(bin+".old") && !safeRAMBackup(before.BackupPath) {
		// The owner explicitly selected a new installation after the RAM backup
		// was lost. Only reserved regular staging files may be discarded here.
		if err := s.clearOwnedStaging(before); err != nil {
			return nil, err
		}
		if err := platform.SyncDir(s.d.Root.Runtime()); err != nil {
			return nil, err
		}
	}
	if exists(bin+".old") || exists(bin+".new") || exists(bin+".recover") {
		return nil, jerr("Сохранены файлы незавершённой установки Xray. Требуется восстановление.")
	}
	resources, err := s.storageResources()
	if err != nil {
		return nil, jerr("Не удалось подтвердить доступную оперативную память для установки Xray.")
	}
	if err := checkStorage(resources, archive.BinarySize, oldSize, req.AllowLowSpace, false); err != nil {
		return nil, err
	}
	m := maintenanceState{Status: "installing", Mode: "normal", Phase: "staging", PreviousVersion: previous, PreviousStatus: before.Status, TargetVersion: strings.TrimPrefix(tag, "v"), BackupSHA256: coreSHA256(bin), StagingOwned: true, RecoveryOwned: true, WasRunning: s.d.App.Xray.Running()}
	if req.AllowLowSpace {
		m.Mode = "ram-rollback"
		if oldSize > 0 {
			m.BackupPath = filepath.Join(filepath.Dir(archive.Path), "rollback")
		}
	}
	// The paths were absent under the operation fence. Record their ownership
	// before writing even the first byte, including an explicit reinstall from removed.
	if err := s.writeMaintenance(m); err != nil {
		return nil, jerr("Не удалось сохранить начало подготовки Xray. Ядро сохранено.")
	}
	switching := false
	defer func() {
		if switching {
			return
		}
		if err := s.clearOwnedStaging(m); err != nil {
			resultErr = err
			return
		}
		if m.BackupPath != "" {
			_ = os.Remove(m.BackupPath)
		}
		if err := s.writeMaintenance(before); err != nil {
			resultErr = jerr("Не удалось завершить подготовку Xray. Сохранён журнал восстановления.")
		}
	}()
	candidate := ""
	version := m.TargetVersion
	if !req.AllowLowSpace {
		candidate, version, err = s.prepareCore(ctx, inst, archive, tag)
		if err != nil {
			return nil, err
		}
		defer os.Remove(candidate)
		m.CandidateSHA256 = coreSHA256(candidate)
	} else {
		m.Mode = "ram-rollback"
		if oldSize > 0 {
			m.BackupPath = filepath.Join(filepath.Dir(archive.Path), "rollback")
			if err := copyCore(bin, m.BackupPath, m.BackupSHA256); err != nil {
				return nil, jerr("Не удалось создать проверенную копию Xray в RAM. Ядро сохранено.")
			}
		}
	}
	// lifeMu protects the process throughout the destructive window. The global
	// operation fence also prevents activation/config changes while downloading.
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	// The normal candidate and the low-space backup now occupy their respective
	// filesystems. Recheck the remaining reserve before stopping the old core.
	resources, err = s.storageResources()
	checkErr := err
	if checkErr == nil {
		if req.AllowLowSpace {
			checkErr = checkStorage(resources, archive.BinarySize, oldSize, true, true)
		} else if resources.FlashFree < installReserve || resources.RAMFree < mxray.MemoryReserve {
			checkErr = errors.New("Недостаточно свободного места или RAM перед заменой. Рабочее ядро сохранено.")
		}
	}
	if checkErr != nil {
		if m.BackupPath != "" {
			_ = os.Remove(m.BackupPath)
		}
		return nil, jerr(checkErr.Error())
	}
	m.WasRunning = s.d.App.Xray.Running()
	if err := ctx.Err(); err != nil {
		if m.BackupPath != "" {
			_ = os.Remove(m.BackupPath)
		}
		return nil, err
	}
	if err := s.writeMaintenance(m); err != nil {
		if m.BackupPath != "" {
			_ = os.Remove(m.BackupPath)
		}
		return nil, jerr("Не удалось сохранить журнал установки Xray. Ядро сохранено.")
	}
	switching = true
	op.Phase("switching")
	op.SetCancelability("protected")
	ctx = context.WithoutCancel(ctx)
	fail := func(reason string) (map[string]any, error) {
		log.printf("%s Восстановление предыдущего состояния Xray.", reason)
		restored := s.restoreMaintenance(ctx, m) == nil
		message := reason + " Предыдущее состояние восстановлено."
		if !restored {
			m.Status = "interrupted"
			_ = s.writeMaintenance(m)
			message = reason + " Восстановление не подтверждено. Сохранены журнал и доступная резервная копия."
		}
		return map[string]any{"success": false, "rolledBack": true, "rollbackSuccess": restored, "error": message}, jerr(message)
	}
	if err := s.d.App.Xray.Stop(ctx); err != nil || !s.waitStopped(ctx) {
		if s.writeMaintenance(before) == nil && m.BackupPath != "" {
			_ = os.Remove(m.BackupPath)
		}
		return nil, jerr("Не удалось остановить Xray перед заменой. Файл ядра сохранён.")
	}
	if req.AllowLowSpace {
		resources, err = s.storageResources()
		if err != nil {
			return fail("Не удалось повторно проверить RAM.")
		}
		if err := checkStorage(resources, archive.BinarySize, oldSize, true, true); err != nil {
			return fail(err.Error())
		}
	}
	m.Phase = "switching"
	if err := s.writeMaintenance(m); err != nil {
		return fail("Не удалось сохранить этап замены Xray.")
	}
	if oldSize > 0 {
		if st, ok := coreFileInfo(bin); !ok || st.Size() != oldSize || coreSHA256(bin) != m.BackupSHA256 {
			return fail("Исходный Xray изменился перед заменой.")
		}
		if req.AllowLowSpace {
			err = os.Remove(bin)
		} else {
			err = os.Rename(bin, bin+".old")
		}
		if err != nil {
			return fail("Не удалось освободить место установленного Xray.")
		}
		if err := platform.SyncDir(s.d.Root.Runtime()); err != nil {
			return fail("Не удалось подтвердить запись каталога Xray.")
		}
	}
	if req.AllowLowSpace {
		candidate, version, err = s.prepareCore(ctx, inst, archive, tag)
		if err != nil {
			return fail("Не удалось распаковать и проверить выбранный Xray: " + err.Error())
		}
		defer os.Remove(candidate)
		m.CandidateSHA256 = coreSHA256(candidate)
		if err := s.writeMaintenance(m); err != nil {
			return fail("Не удалось сохранить результат проверки Xray.")
		}
	}
	if err := os.Rename(candidate, bin); err != nil {
		return fail("Не удалось установить новый Xray.")
	}
	if err := platform.SyncDir(s.d.Root.Runtime()); err != nil {
		return fail("Не удалось подтвердить замену Xray.")
	}
	// Free the ZIP before starting the full core; the verified rollback stays.
	_ = archive.Close()
	if installedVersion(s.versionOutput(ctx, bin)) != version {
		return fail("Новый Xray не прошёл проверку версии.")
	}
	if m.WasRunning {
		if err := s.d.App.Xray.Start(ctx); err != nil || !s.waitRunning(ctx) || !s.waitSocks(ctx, installStartWait) {
			return fail("Новый Xray не запустился.")
		}
	}
	if err := s.writeMaintenance(maintenanceState{Status: "ready", PreviousVersion: version, TargetVersion: version}); err != nil {
		return fail("Не удалось зафиксировать успешную установку Xray.")
	}
	if m.Mode == "normal" {
		_ = os.Remove(bin + ".old")
		_ = platform.SyncDir(s.d.Root.Runtime())
	} else if m.BackupPath != "" {
		_ = os.Remove(m.BackupPath)
		_ = os.Remove(filepath.Dir(m.BackupPath))
	}
	if m.WasRunning {
		s.recordDeviceTrial(version, coreSHA256(bin), mode)
	}
	return map[string]any{"success": true, "installed": true, "mode": mode, "storageMode": m.Mode, "previousVersion": nullStr(previous), "version": version, "sha256": coreSHA256(bin), "running": s.d.App.Xray.Running(), "message": "Xray установлен. Настройки BROray сохранены."}, nil
}

func safeRAMBackup(path string) bool {
	if filepath.Base(path) != "rollback" || filepath.Dir(filepath.Dir(path)) != "/tmp" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "broray-xray-") {
		return false
	}
	st, err := os.Lstat(filepath.Dir(path))
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return false
	}
	if _, err := mxray.RAMAvailable(filepath.Dir(path)); err != nil {
		return false
	}
	_, ok := coreFileInfo(path)
	return ok
}

func (s *service) restoreMaintenance(ctx context.Context, m maintenanceState) error {
	bin := s.d.Root.XrayBinary()
	if err := s.d.App.Xray.Stop(ctx); err != nil || !s.waitStopped(ctx) {
		return errors.New("XRAY_ROLLBACK_STOP_FAILED")
	}
	// Reclaim the candidate before copying the rollback, within the one-binary
	// flash budget. Do not remove unowned or changed completed staging.
	if err := removeOwnedStage(bin+".new", m.StagingOwned, m.CandidateSHA256); err != nil {
		return err
	}
	oldAlreadyPresent := m.BackupSHA256 != "" && coreSHA256(bin) == m.BackupSHA256
	if !oldAlreadyPresent && m.PreviousVersion != "" {
		backup := bin + ".old"
		if m.Mode == "ram-rollback" {
			backup = m.BackupPath
			if !safeRAMBackup(backup) {
				return errors.New("XRAY_RAM_BACKUP_UNAVAILABLE")
			}
		}
		if _, ok := coreFileInfo(backup); !ok || m.BackupSHA256 == "" || coreSHA256(backup) != m.BackupSHA256 {
			return errors.New("XRAY_ROLLBACK_UNCONFIRMED")
		}
		if exists(bin) && (m.CandidateSHA256 == "" || coreSHA256(bin) != m.CandidateSHA256) {
			return errors.New("XRAY_ROLLBACK_CURRENT_CHANGED")
		}
		if m.Mode == "ram-rollback" {
			if exists(bin) {
				if err := os.Remove(bin); err != nil {
					return err
				}
			}
			if !m.RecoveryOwned {
				return errors.New("XRAY_RECOVERY_OWNERSHIP_UNCONFIRMED")
			}
			if coreSHA256(bin+".recover") != m.BackupSHA256 {
				if err := removeOwnedStage(bin+".recover", true, ""); err != nil {
					return err
				}
				if err := copyCore(backup, bin+".recover", m.BackupSHA256); err != nil {
					return err
				}
			}
			if err := os.Rename(bin+".recover", bin); err != nil {
				return err
			}
		} else if err := os.Rename(backup, bin); err != nil {
			return err
		}
	} else if m.PreviousVersion == "" && exists(bin) {
		if m.CandidateSHA256 == "" || coreSHA256(bin) != m.CandidateSHA256 {
			return errors.New("XRAY_ROLLBACK_CURRENT_CHANGED")
		}
		if err := os.Remove(bin); err != nil {
			return err
		}
	}
	if err := removeOwnedStage(bin+".recover", m.RecoveryOwned, ""); err != nil {
		return err
	}
	if err := platform.SyncDir(s.d.Root.Runtime()); err != nil {
		return err
	}
	if m.WasRunning && m.PreviousVersion != "" {
		if err := s.d.App.Xray.Start(ctx); err != nil || !s.waitRunning(ctx) || !s.waitSocks(ctx, installStartWait) {
			return errors.New("XRAY_ROLLBACK_START_FAILED")
		}
	}
	status := m.PreviousStatus
	if status == "" {
		status = "ready"
	}
	if err := s.writeMaintenance(maintenanceState{Status: status, PreviousVersion: m.PreviousVersion}); err != nil {
		return err
	}
	if m.Mode == "ram-rollback" && safeRAMBackup(m.BackupPath) {
		_ = os.Remove(m.BackupPath)
		_ = os.Remove(filepath.Dir(m.BackupPath))
	}
	return nil
}

func removeOwnedStage(path string, owned bool, hash string) error {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !owned || !st.Mode().IsRegular() || st.Size() > mxray.BinaryLimit || hash != "" && coreSHA256(path) != hash {
		return errors.New("XRAY_STAGING_OWNERSHIP_UNCONFIRMED")
	}
	return os.Remove(path)
}

func (s *service) clearOwnedStaging(m maintenanceState) error {
	bin := s.d.Root.XrayBinary()
	if err := removeOwnedStage(bin+".new", m.StagingOwned, m.CandidateSHA256); err != nil {
		return err
	}
	return removeOwnedStage(bin+".recover", m.RecoveryOwned, "")
}

func (s *service) recoverTransaction() bool {
	m, err := s.readMaintenance()
	if err != nil {
		return true
	}
	if m.Status != "installing" && m.Status != "interrupted" {
		return false
	}
	if err := s.restoreMaintenance(context.Background(), m); err != nil {
		m.Status = "interrupted"
		_ = s.writeMaintenance(m)
		s.d.Log.Printf("xray: interrupted core replacement; recovery not confirmed")
	}
	return true
}
