package servers

import (
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
)

// Never log wrapped error text: it may contain a source URL or private profile.
func (s *svc) logSourceFailure(phase string, err error) {
	if err == nil || s.d == nil || s.d.Log == nil {
		return
	}
	class := "operation"
	switch {
	case errors.Is(err, context.Canceled):
		class = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		class = "deadline"
	case errors.Is(err, compat.ErrXrayCandidateRejected):
		class = "candidate-rejected"
	case errors.Is(err, errReserveTransactionPending):
		class = "recovery-required"
	case errors.Is(err, errReserveRuntime):
		class = "runtime-proof"
	case errors.Is(err, errSourceRemovalState):
		class = "state-proof"
	}
	s.d.Log.Printf("servers: source lifecycle failed phase=%s class=%s", phase, class)
}
