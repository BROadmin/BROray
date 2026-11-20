package keenetic

import (
	"context"
	"errors"

	"github.com/BROadmin/broray/internal/compat"
)

func registerReserveDescriptionServices(s *service) {
	s.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) {
		description, ok := ordinaryDescription(sanitizeServerName(name))
		if !ok {
			return "", errors.New("KEENETIC_DESCRIPTION_INVALID")
		}
		return description, nil
	}
	s.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
		description, ok := ordinaryDescription(s.activeServerName())
		if !ok {
			return "", errors.New("KEENETIC_DESCRIPTION_INVALID")
		}
		return description, nil
	}
	s.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
		if ctx.Err() != nil || !compat.FenceHeld(ctx) || !s.mu.TryLock() {
			return errors.New("KEENETIC_DESCRIPTION_AUTHORITY_REQUIRED")
		}
		defer s.mu.Unlock()
		// Do not call EnsureInterface: repairing/recreating a connection is
		// outside the metadata-only mode transition's authority.
		return s.syncDescription(ctx)
	}
}
