package servers

import (
	"context"
	"errors"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

var ErrSourceRemovalQueueRequired = errors.New("SOURCE_REMOVAL_QUEUE_REQUIRED")

// CheckSourceRemovalLegacy is a read-only compatibility guard. A legacy
// inactive deletion cannot alter reserve policy or enter a durable transition
// under its ordinary job fence; affected sources require the protected queue.
func CheckSourceRemovalLegacy(ctx context.Context, d *compat.Deps, ids []string) error {
	pending, err := reserve.TransactionPending(ctx, d.Root)
	if err != nil {
		return err
	}
	if pending {
		return ErrSourceRemovalQueueRequired
	}
	if len(ids) == 0 {
		return nil
	}
	prefs, err := readReservePreferences(d)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !ValidID(id) {
			return errReserveRuntime
		}
		if reserveRosterContains(prefs, id) {
			return ErrSourceRemovalQueueRequired
		}
	}
	return nil
}
