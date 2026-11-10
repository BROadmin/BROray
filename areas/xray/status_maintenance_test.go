package xray

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceChangeInvalidatesStatusCache(t *testing.T) {
	h := newHarness(t, true)
	if err := h.s.writeMaintenance(maintenanceState{Status: "installing"}); err != nil {
		t.Fatal(err)
	}
	if got := h.s.statusCached(context.Background())["maintenanceStatus"]; got != "installing" {
		t.Fatal(got)
	}
	if err := h.s.writeMaintenance(maintenanceState{Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	if got := h.s.statusCached(context.Background())["maintenanceStatus"]; got != "ready" {
		t.Fatalf("completed maintenance cached as %v", got)
	}
}

func TestStatusInvalidationDiscardsInflightMaintenanceSnapshot(t *testing.T) {
	h := newHarness(t, true)
	if err := h.s.writeMaintenance(maintenanceState{Status: "installing"}); err != nil {
		t.Fatal(err)
	}
	marker, release := filepath.Join(t.TempDir(), "entered"), filepath.Join(t.TempDir(), "release")
	script := strings.Replace(fakeXray("26.11.9"), "version) echo", "version) touch '"+marker+"'; while [ ! -f '"+release+"' ]; do sleep 0.01; done; echo", 1)
	if err := os.WriteFile(h.d.Root.XrayBinary(), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan map[string]any, 1)
	go func() { done <- h.s.statusCached(ctx) }()
	defer os.WriteFile(release, nil, 0600)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("status did not reach version barrier")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := h.s.writeMaintenance(maintenanceState{Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	h.s.invalidateStatus() // the completed operation also invalidates the cache
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-done:
		if status["maintenanceStatus"] != "ready" {
			t.Fatalf("in-flight snapshot survived invalidation: %v", status["maintenanceStatus"])
		}
	case <-ctx.Done():
		t.Fatal("status did not finish")
	}
	if got := h.s.statusCached(ctx)["maintenanceStatus"]; got != "ready" {
		t.Fatalf("stale result repopulated cache: %v", got)
	}
}
