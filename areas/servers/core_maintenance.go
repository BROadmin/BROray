package servers

import (
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

func (s *svc) coreMaintenanceBlocked() bool { return platform.XrayMaintenanceBlocked(s.d.Root) }

func maintenanceRequired(t *compat.Task) error {
	return stepError(t, "XRAY_MAINTENANCE_REQUIRED", "Ядро Xray удалено или требует восстановления. Откройте страницу Xray. Настройки автовыбора сохранены.")
}
