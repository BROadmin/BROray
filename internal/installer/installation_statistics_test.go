package installer

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/installstats"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

func TestUpdateStatisticsRequiresCommittedExactReceipt(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	old := installstats.Build{Version: "3.5.9", Revision: "r9"}
	current := installstats.Build{Version: "3.5.10", Revision: "r14"}
	event, err := installstats.NewEvent("update", current, old, "arm64", installstats.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{OperationID: "update-20261124132200-1", Mode: "switch", From: old.Version, To: current.Version,
		PreviousTarget: filepath.Join("versions", old.Version), NewTarget: filepath.Join("versions", current.Version),
		PreviousSHA256: strings.Repeat("a", 64), StagedSHA256: strings.Repeat("b", 64),
		Statistics: &event, StatisticsReady: true}
	path := filepath.Join(root.State(), "install-statistics", "event-"+event.EventID+".json")
	if err := j.set(root, JStarted); err != nil {
		t.Fatal(err)
	}
	drainCommittedStatistics(root, j)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uncommitted installation was counted")
	}
	if err := j.set(root, JCommitted); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	held, err := platform.TryLockNoFollow(filepath.Join(filepath.Dir(path), ".queue.lock"))
	if err != nil {
		t.Fatal(err)
	}
	drainCommittedStatistics(root, j)
	held.Release()
	unchanged, err := LoadJournal(root)
	if err != nil || unchanged == nil || unchanged.Statistics == nil || *unchanged.Statistics != event {
		t.Fatal("locked queue lost the exact transaction receipt")
	}
	drainCommittedStatistics(root, unchanged)
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), event.EventID) || !strings.Contains(string(data), old.Revision) {
		t.Fatalf("committed receipt missing from the outbox: %v", err)
	}
	finished, err := LoadJournal(root)
	if err != nil || finished == nil || finished.State != JCommitted || finished.Statistics != nil || finished.StatisticsReady {
		t.Fatal("event enqueue did not retire the journal's copy")
	}
}

func TestUpdateStatisticsSurvivesSuccessorWithHeldQueue(t *testing.T) {
	root, _ := fixture(t)
	const installed = "3.5.10"
	const candidate = "3.5.11"
	event, err := installstats.NewEvent("update", installstats.Build{Version: installed, Revision: "r14"},
		installstats.Build{Version: "3.5.9", Revision: "r9"}, "arm64", installstats.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(root.Versions(), installed)
	if err := os.Mkdir(current, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "broray"), []byte("#!/bin/sh\nprintf '%s\\n' 'BROray 3.5.10'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("versions", installed), root.Current()); err != nil {
		t.Fatal(err)
	}
	previous := &Journal{Mode: "switch", From: "3.5.9", To: installed, OperationID: "old-operation",
		PreviousTarget: filepath.Join("versions", "3.5.9"), NewTarget: filepath.Join("versions", installed),
		Statistics: &event, StatisticsReady: true}
	if err := previous.set(root, JCommitted); err != nil {
		t.Fatal(err)
	}
	queueDir := filepath.Join(root.State(), "install-statistics")
	if err := os.Mkdir(queueDir, 0700); err != nil {
		t.Fatal(err)
	}
	heldQueue, err := platform.TryLockNoFollow(filepath.Join(queueDir, ".queue.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer heldQueue.Release()
	heldDaemon, err := platform.TryLockNoFollow(root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer heldDaemon.Release()
	self := filepath.Join(root.Dir, "fixture-updater")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nprintf '%s\\n' 'BROray 3.5.11'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	result := Apply(ApplyOptions{Root: root, ABI: update.ABI, Self: self, Version: candidate,
		OperationID: "new-operation", StopTimeout: time.Second})
	if result.OK || result.State != JFailed {
		t.Fatalf("successor did not replace the journal and fail before switch: %+v", result)
	}
	failed, err := LoadJournal(root)
	if err != nil || failed == nil || failed.OperationID != "new-operation" || failed.State != JFailed {
		t.Fatalf("successor journal was not durably recorded: %v", err)
	}
	if err := heldQueue.Release(); err != nil {
		t.Fatal(err)
	}
	RetryCommittedStatistics(root)
	raw, err := os.ReadFile(filepath.Join(queueDir, "event-"+event.EventID+".json"))
	if err != nil || !strings.Contains(string(raw), event.EventID) || !strings.Contains(string(raw), event.Revision) {
		t.Fatalf("new native transaction lost the previous committed event: %v", err)
	}
}

func TestUpdateStatisticsConfirmsExactControlBuild(t *testing.T) {
	for _, test := range []struct {
		name, liveRevision string
		want               bool
	}{{"stale", "r13", false}, {"exact", "r14", true}} {
		t.Run(test.name, func(t *testing.T) {
			root := platform.Root{Dir: t.TempDir()}
			if err := root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			event, err := installstats.NewEvent("update", installstats.Build{Version: "3.5.10", Revision: "r14"},
				installstats.Build{Version: "3.5.9", Revision: "r9"}, "arm64", installstats.Identity{})
			if err != nil {
				t.Fatal(err)
			}
			digest := strings.Repeat("b", 64)
			j := &Journal{Mode: "switch", From: "3.5.9", To: "3.5.10", Statistics: &event, StagedSHA256: digest}
			if err := j.set(root, JStarted); err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("unix", root.DaemonSocket())
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			control.MountBuildObservation(mux, func(_ context.Context, expected string) (control.BuildObservation, bool) {
				return control.BuildObservation{Version: "3.5.10", Revision: test.liveRevision,
					Architecture: "arm64", SHA256: digest, Model: "KN-2710"}, expected == digest
			})
			server := &http.Server{Handler: mux}
			go func() { _ = server.Serve(ln) }()
			defer server.Close()
			if err := commit(root, j); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join(root.State(), "install-statistics", "event-"+event.EventID+".json"))
			if test.want && (err != nil || !strings.Contains(string(body), "KN-2710")) ||
				!test.want && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected readiness verdict and queue entry: %v", err)
			}
		})
	}
}
