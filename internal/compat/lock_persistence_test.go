package compat

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func unwritableLockPath(d *Deps) string {
	return filepath.Join(d.Root.Dir, "missing-parent", "global-operation.json")
}

func nonemptyLockDirectory(t *testing.T, d *Deps) string {
	t.Helper()
	dir := filepath.Join(d.Root.Dir, "foreign-lock-directory")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "owner"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestActiveGlobalLockRecordBlocksRestart(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	t.Cleanup(release)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewGlobalLock(path)
	if !restarted.Busy() {
		t.Fatal("live active record was discarded on restart")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("active owner record changed: %q, %v", got, err)
	}
	if next, ok := restarted.TryAcquire("dot", "apply", "dns"); ok {
		next()
		t.Fatal("new operation acquired over active record")
	}
}

func TestFailedMarkPendingWriteKeepsRestartFenced(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	d.Lock.file = unwritableLockPath(d)
	d.Lock.MarkPending(true)
	d.Lock.file = path
	release()
	restarted := NewGlobalLock(path)
	if !restarted.Busy() {
		t.Fatal("failed pending write left restart free for new mutations")
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("prior active record was removed: %v", err)
	}
}

func TestFailedFirstLockWriteRefusesAcquire(t *testing.T) {
	d := testDeps(t)
	d.Lock.file = unwritableLockPath(d)
	if release, ok := d.Lock.TryAcquire("routes", "apply", "telegram"); ok {
		release()
		t.Fatal("operation was admitted without any durable owner record")
	}
	if !d.Lock.Busy() {
		t.Fatal("failed owner write did not leave a protective fence")
	}
}

func TestFailedReleaseRemovalRetainsFence(t *testing.T) {
	d := testDeps(t)
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	d.Lock.file = nonemptyLockDirectory(t, d)
	release()
	if !d.Lock.Busy() {
		t.Fatal("failed owner removal released the in-memory fence")
	}
}

func TestFailedClearPendingRemovalRetainsFence(t *testing.T) {
	d := testDeps(t)
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	d.Lock.MarkPending(true)
	release()
	d.Lock.file = nonemptyLockDirectory(t, d)
	if d.Lock.ClearPending() || !d.Lock.Busy() {
		t.Fatal("failed pending-record removal cleared the fence")
	}
}

func TestReplacedOwnerRecordIsNotRemovedOnRelease(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	if err := os.Rename(path, path+".owned"); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("foreign owner")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	release()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, foreign) || !d.Lock.Busy() {
		t.Fatalf("foreign record removed or fence freed: %q, %v", got, err)
	}
}

func TestReplacedOwnerRecordIsNotOverwrittenOnPending(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	_, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	if err := os.Rename(path, path+".owned"); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("foreign owner")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	d.Lock.MarkPending(true)
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, foreign) || !d.Lock.Busy() {
		t.Fatalf("foreign record overwritten or fence freed: %q, %v", got, err)
	}
}

func TestForeignRecordBeforeFirstAcquireBlocks(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	foreign := []byte("foreign owner")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if release, ok := d.Lock.TryAcquire("routes", "apply", "telegram"); ok {
		release()
		t.Fatal("first acquisition replaced a foreign record")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, foreign) || !d.Lock.Busy() {
		t.Fatalf("foreign record altered or fence freed: %q, %v", got, err)
	}
}

func TestFailedTakePendingWriteRetainsFence(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	d.Lock.MarkPending(true)
	release()
	d.Lock.file = unwritableLockPath(d)
	if resumed, ok := d.Lock.TakePending("routes", "telegram", "resume"); ok {
		resumed()
		t.Fatal("resume admitted without a durable owner record")
	}
	d.Lock.file = path
	if !d.Lock.Busy() || !NewGlobalLock(path).Busy() {
		t.Fatal("failed resume write lost the pending fence")
	}
}

func TestMissingOwnerRecordOnReleaseFailsClosed(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	release()
	if !d.Lock.Busy() {
		t.Fatal("missing owner record released the fence")
	}
}

func TestModifiedOwnerRecordIsNotRemovedOnRelease(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	release, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	foreign := []byte("foreign owner")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	release()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, foreign) || !d.Lock.Busy() {
		t.Fatalf("modified owner record removed or fence freed: %q, %v", got, err)
	}
}

func TestModifiedOwnerRecordIsNotOverwrittenOnPending(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	_, ok := d.Lock.TryAcquire("routes", "apply", "telegram")
	if !ok {
		t.Fatal("initial acquire")
	}
	foreign := []byte("foreign owner")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	d.Lock.MarkPending(true)
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, foreign) || !d.Lock.Busy() {
		t.Fatalf("modified owner record overwritten or fence freed: %q, %v", got, err)
	}
}
