package compat

import (
	"errors"
	"testing"
)

func isolateAreaRegistry(t *testing.T) {
	t.Helper()
	areasMu.Lock()
	saved := append([]area(nil), areas...)
	areas = nil
	areasMu.Unlock()
	t.Cleanup(func() {
		areasMu.Lock()
		areas = saved
		areasMu.Unlock()
	})
}

func TestOpsRecoveryFailureStopsLaterAreas(t *testing.T) {
	isolateAreaRegistry(t)
	started := false
	Area("routes", 50, func(*Deps) error {
		started = true
		return nil
	})
	Area("ops", 0, func(*Deps) error { return errors.New("ambiguous backup copy") })
	if err := StartAreas(testDeps(t)); err == nil {
		t.Fatal("critical ops failure was not returned to caller")
	}
	if started {
		t.Fatal("later area started despite failed backup recovery")
	}
}

func TestNoncriticalSetupFailureKeepsOtherAreas(t *testing.T) {
	isolateAreaRegistry(t)
	started := false
	Area("routes", 0, func(*Deps) error { return errors.New("routes unavailable") })
	Area("servers", 50, func(*Deps) error {
		started = true
		return nil
	})
	if err := StartAreas(testDeps(t)); err != nil {
		t.Fatalf("noncritical failure returned as fatal: %v", err)
	}
	if !started {
		t.Fatal("noncritical area failure stopped other areas")
	}
}
