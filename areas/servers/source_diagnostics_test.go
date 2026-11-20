package servers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/BROadmin/broray/internal/compat"
	"log"
	"strings"
	"testing"
)

func TestSourceFailureDiagnosticsNoPrivateError(t *testing.T) {
	var out bytes.Buffer
	s := &svc{d: &compat.Deps{Log: log.New(&out, "", 0)}}
	for _, tt := range []struct {
		err  error
		want string
	}{{context.DeadlineExceeded, "deadline"}, {compat.ErrXrayCandidateRejected, "candidate-rejected"}, {errReserveRuntime, "runtime-proof"}, {errSourceRemovalState, "state-proof"}, {errors.New("other"), "operation"}} {
		out.Reset()
		s.logSourceFailure("replacement/core-validation", fmt.Errorf("private://password@secret: %w", tt.err))
		got := out.String()
		if !strings.Contains(got, "phase=replacement/core-validation class="+tt.want) || strings.Contains(got, "private") || strings.Contains(got, "secret") {
			t.Fatal("unsafe diagnostic", got)
		}
	}
	out.Reset()
	s.logSourceFailure("replacement/core-validation", nil)
	if out.Len() != 0 {
		t.Fatal("successful operation logged as failure")
	}
}
