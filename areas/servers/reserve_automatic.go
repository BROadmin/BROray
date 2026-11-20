package servers

import (
	"errors"

	"github.com/BROadmin/broray/internal/compat"
)

var errReserveModeActive = errors.New("RESERVE_MODE_ACTIVE")

// Invalid or unreadable mode ownership never authorizes automatic selection.
// Missing preferences are the normal, backwards-compatible OFF state.
func (s *svc) ordinarySelectionAllowed() error {
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return errReservePreferences
	}
	if prefs.Enabled {
		return errReserveModeActive
	}
	return nil
}

// Check at execution as well as admission: an old queued task must not take
// ownership from reserve mode. Mode changes must use the same coordinator and
// global apply fence; this check is not a substitute for that transaction.
func (s *svc) requireOrdinarySelection(t *compat.Task) error {
	err := s.ordinarySelectionAllowed()
	if errors.Is(err, errReserveModeActive) {
		return stepError(t, "RESERVE_MODE_ACTIVE", "Выбором сервера управляет резервирование VPN.")
	}
	if err != nil {
		return stepError(t, "RESERVE_SETTINGS_INVALID", "Не удалось подтвердить настройки резервирования VPN.")
	}
	return nil
}
