package routes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestJobEndReportsPendingFencePersistenceError(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "lock")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock := compat.NewGlobalLock(filepath.Join(dir, "global-operation.json"))
	release, ok := lock.TryAcquire("routes", "export", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	j := &job{e: &engine{lock: lock}, rel: release, keepPending: true, pendingResumable: true}
	finisher, ok := any(j).(interface{ end(error) error })
	if !ok {
		t.Fatal("route job completion cannot return a pending-fence persistence error to its caller")
	}
	moved := dir + ".moved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(moved, dir)
	if err := finisher.end(nil); err == nil {
		t.Fatal("failed pending-fence persistence was reported as successful completion")
	}
	if !lock.Busy() {
		t.Fatal("failed pending-fence persistence released the fence")
	}
}
