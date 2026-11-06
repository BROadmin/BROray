package compat

import (
	"bytes"
	"os"
	"testing"
)

func ambiguousLockFixture(t *testing.T, data []byte) (*Deps, string) {
	t.Helper()
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return d, path
}

func TestMalformedGlobalLockRecordIsPreserved(t *testing.T) {
	content := []byte("{broken")
	_, path := ambiguousLockFixture(t, content)
	_ = NewGlobalLock(path)
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("ambiguous ownership record changed: %q, %v", got, err)
	}
}

func TestMalformedGlobalLockBlocksNewOwner(t *testing.T) {
	_, path := ambiguousLockFixture(t, []byte("{broken"))
	g := NewGlobalLock(path)
	if !g.Busy() {
		t.Fatal("malformed ownership record did not fence mutations")
	}
	if release, ok := g.TryAcquire("routes", "apply", "new"); ok {
		release()
		t.Fatal("new owner acquired fence over malformed record")
	}
	if g.ClearPending() || !g.Busy() {
		t.Fatal("ambiguous ownership was clearable as a normal pending operation")
	}
}

func TestSymlinkGlobalLockRecordFailsClosed(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	target := d.RunPath("foreign-record.json")
	if err := os.WriteFile(target, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	g := NewGlobalLock(path)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("foreign symlink was removed or followed: %v", err)
	}
	if !g.Busy() {
		t.Fatal("symlink ownership record did not fence mutations")
	}
}

func TestNullGlobalLockRecordFailsClosed(t *testing.T) {
	_, path := ambiguousLockFixture(t, []byte("null"))
	g := NewGlobalLock(path)
	if !g.Busy() {
		t.Fatal("schema-invalid ownership record did not fence mutations")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "null" {
		t.Fatalf("schema-invalid ownership record changed: %q, %v", got, err)
	}
}

func TestAmbiguousGlobalLockStopsAreaStartup(t *testing.T) {
	d, path := ambiguousLockFixture(t, []byte("{broken"))
	d.Lock = NewGlobalLock(path)
	isolateAreaRegistry(t)
	started := false
	Area("ops", 0, func(*Deps) error { started = true; return nil })
	if err := StartAreas(d); err == nil || started {
		t.Fatalf("areas started over ambiguous global lock: started=%t err=%v", started, err)
	}
}

func TestMissingGlobalLockAllowsNewOwner(t *testing.T) {
	d := testDeps(t)
	path := d.RunPath("global-operation.json")
	g := NewGlobalLock(path)
	release, ok := g.TryAcquire("routes", "apply", "new")
	if !ok {
		t.Fatal("missing record unexpectedly blocked a new owner")
	}
	release()
}

func TestTrailingGlobalLockJSONFailsClosed(t *testing.T) {
	content := []byte(`{"scope":"routes","action":"apply","pending":false,"startedAt":"2026-11-04T00:00:00Z","updatedAt":"2026-11-04T00:00:01Z"}{"pending":true}`)
	_, path := ambiguousLockFixture(t, content)
	g := NewGlobalLock(path)
	if !g.Busy() {
		t.Fatal("trailing JSON allowed a new owner")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("trailing JSON record changed: %q, %v", got, err)
	}
}

func TestIncompletePendingGlobalLockCannotBeCleared(t *testing.T) {
	content := []byte(`{"pending":true}`)
	_, path := ambiguousLockFixture(t, content)
	g := NewGlobalLock(path)
	if g.ClearPending() || !g.Busy() {
		t.Fatal("incomplete pending holder was clearable")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("incomplete pending record changed: %q, %v", got, err)
	}
}

func TestAmbiguousGlobalLockJSONReportsActive(t *testing.T) {
	_, path := ambiguousLockFixture(t, []byte("{broken"))
	g := NewGlobalLock(path)
	status := g.JSON()
	if active, ok := status["active"].(bool); !ok || !active {
		t.Fatalf("ambiguous lock reported inactive: %+v", status)
	}
}
