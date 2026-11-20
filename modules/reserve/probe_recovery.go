package reserve

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
)

var ErrProbeWorkspaceUnconfirmed = errors.New("RESERVE_PROBE_CLEANUP_UNCONFIRMED")

// A leftover workspace may still contain a live child. Neither its age nor a
// recorded PID authorizes deletion or recovery. Stop before other mutations.
func CheckProbeWorkspaces(root platform.Root) error {
	dir, err := platform.OpenDirNoFollow(root.Tmp())
	if err != nil {
		return err
	}
	defer dir.Close()
	for count := 0; count < 4096; count += 128 {
		names, err := dir.Readdirnames(128)
		for _, name := range names {
			if strings.HasPrefix(name, "reserve-probe-") {
				return ErrProbeWorkspaceUnconfirmed
			}
		}
		if err == io.EOF {
			visible, e := platform.OpenDirNoFollow(root.Tmp())
			if e != nil {
				return e
			}
			defer visible.Close()
			a, ea := dir.Stat()
			b, eb := visible.Stat()
			if ea != nil || eb != nil || !os.SameFile(a, b) {
				return ErrProbeWorkspaceUnconfirmed
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
	return ErrProbeWorkspaceUnconfirmed
}
