package xray

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

type maintenanceState struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Status          string `json:"status"`
	Mode            string `json:"mode,omitempty"`
	Phase           string `json:"phase,omitempty"`
	PreviousVersion string `json:"previousVersion,omitempty"`
	TargetVersion   string `json:"targetVersion,omitempty"`
	PreviousStatus  string `json:"previousStatus,omitempty"`
	WasRunning      bool   `json:"wasRunning,omitempty"`
	BackupPath      string `json:"backupPath,omitempty"`
	BackupSHA256    string `json:"backupSha256,omitempty"`
	CandidateSHA256 string `json:"candidateSha256,omitempty"`
	StagingOwned    bool   `json:"stagingOwned,omitempty"`
	RecoveryOwned   bool   `json:"recoveryOwned,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

func (s *service) maintenancePath() string {
	return filepath.Join(s.d.Root.State(), "xray-maintenance.json")
}

func (s *service) readMaintenance() (maintenanceState, error) {
	var m maintenanceState
	st, err := os.Lstat(s.maintenancePath())
	if os.IsNotExist(err) {
		return maintenanceState{SchemaVersion: 1, Status: "ready"}, nil
	}
	if err != nil {
		return m, err
	}
	if !st.Mode().IsRegular() || st.Size() > 16384 {
		return m, errors.New("XRAY_MAINTENANCE_STATE_INVALID")
	}
	b, err := os.ReadFile(s.maintenancePath())
	if err != nil {
		return m, err
	}
	if json.Unmarshal(b, &m) != nil || m.SchemaVersion != 1 {
		return m, errors.New("XRAY_MAINTENANCE_STATE_INVALID")
	}
	switch m.Status {
	case "ready", "removed", "removing", "installing", "interrupted":
		return m, nil
	}
	return m, errors.New("XRAY_MAINTENANCE_STATE_INVALID")
}

func (s *service) writeMaintenance(m maintenanceState) error {
	m.SchemaVersion = 1
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := platform.MkdirAllDurable(s.d.Root.State(), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := platform.WriteFileAtomic(s.maintenancePath(), append(b, '\n'), 0600); err != nil {
		return err
	}
	s.invalidateStatus()
	return nil
}

func (s *service) maintenanceBlocksAuto() bool {
	m, err := s.readMaintenance()
	return err != nil || m.Status != "ready"
}

func (s *service) removeCore(ctx context.Context, op *compat.Operation, log *jobLog, req installRequest) (map[string]any, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	bin := s.d.Root.XrayBinary()
	st, err := os.Lstat(bin)
	if err != nil || !st.Mode().IsRegular() {
		return nil, jerr("Штатный файл Xray не найден или его принадлежность не подтверждена.")
	}
	version := installedVersion(s.versionOutput(ctx, bin))
	if version == "" || version != req.CurrentVersion {
		return nil, jerr("Версия Xray изменилась. Обновите страницу перед удалением.")
	}
	previous, err := s.readMaintenance()
	if err != nil || previous.Status == "installing" || previous.Status == "interrupted" || exists(bin+".old") || exists(bin+".new") {
		return nil, jerr("Сначала завершите восстановление прерванной установки Xray.")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	op.Phase("removing")
	op.SetCancelability("protected")
	ctx = context.WithoutCancel(ctx)
	wasRunning := s.d.App.Xray.Running()
	if err := s.d.App.Xray.Stop(ctx); err != nil || !s.waitStopped(ctx) {
		return nil, jerr("Не удалось остановить Xray перед удалением.")
	}
	m := maintenanceState{Status: "removing", PreviousVersion: version, WasRunning: wasRunning, BackupSHA256: coreSHA256(bin)}
	if err := s.writeMaintenance(m); err != nil {
		s.restartIf(ctx, wasRunning)
		return nil, jerr("Не удалось сохранить намерение удалить Xray. Ядро сохранено.")
	}
	if err := os.Remove(bin); err != nil {
		if s.writeMaintenance(previous) == nil {
			s.restartIf(ctx, wasRunning)
		}
		return nil, jerr("Не удалось удалить Xray. Проверьте состояние обслуживания.")
	}
	if err := platform.SyncDir(s.d.Root.Runtime()); err != nil {
		return nil, jerr("Удаление Xray выполнено, но запись каталога не подтверждена.")
	}
	m.Status = "removed"
	m.WasRunning = false
	if err := s.writeMaintenance(m); err != nil {
		return nil, jerr("Ядро удалено; завершение записи состояния не подтверждено.")
	}
	log.printf("Xray удалён. Серверы, подписки, настройки и маршруты сохранены.")
	return map[string]any{"success": true, "installed": false, "running": false, "previousVersion": version, "message": "Xray удалён. Выберите версию для установки. Настройки BROray сохранены."}, nil
}

func (s *service) recoverRemoval() bool {
	m, err := s.readMaintenance()
	if err != nil {
		s.d.Log.Printf("xray: maintenance state invalid; automatic installation blocked")
		return true
	}
	if m.Status == "removed" {
		return true
	}
	if m.Status != "removing" {
		return false
	}
	if s.d.App.Xray.Stop(context.Background()) != nil {
		return true
	}
	bin := s.d.Root.XrayBinary()
	if st, err := os.Lstat(bin); err == nil {
		if !st.Mode().IsRegular() || m.BackupSHA256 == "" || coreSHA256(bin) != m.BackupSHA256 {
			return true
		}
		if s.d.App.Xray.Running() {
			return true
		}
		if os.Remove(bin) != nil || platform.SyncDir(s.d.Root.Runtime()) != nil {
			return true
		}
	} else if !os.IsNotExist(err) {
		return true
	}
	m.Status = "removed"
	m.WasRunning = false
	_ = s.writeMaintenance(m)
	return true
}
