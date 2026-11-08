package installer

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
)

// Model the published daemon exiting before its detached watcher releases the
// persisted fence. The real control protocol and kernel daemon lock are used.
func TestUpdateStopHandsOffOnlyBoundFence(t *testing.T) {
	for _, variant := range []string{"update", "reinstall", "already_released", "foreign_scope", "wrong_operation", "pending", "changed_inode", "changed_bytes", "symlink", "no_launcher", "no_installer", "not_running"} {
		t.Run(variant, func(t *testing.T) {
			root, _ := fixture(t)
			if err := os.MkdirAll(filepath.Join(root.Data(), "state", "update"), 0700); err != nil {
				t.Fatal(err)
			}
			op := "update"
			if variant == "reinstall" {
				op = variant
			}
			id := op + "-20261108160000-601-574e257e2884b2db"
			j := &Journal{OperationID: id, Operation: op, From: "3.5.1", To: "3.5.2"}
			if err := j.set(root, JStopping); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{root.LauncherLock(), root.InstallerLock()} {
				if variant == "no_launcher" && p == root.LauncherLock() || variant == "no_installer" && p == root.InstallerLock() {
					continue
				}
				l, err := platform.TryLock(p)
				if err != nil {
					t.Fatal(err)
				}
				defer l.Release()
			}
			now := time.Now().UTC()
			fence := map[string]any{"scope": "system", "action": op, "pending": false, "resumable": false, "startedAt": now, "updatedAt": now}
			if variant == "foreign_scope" {
				fence["scope"] = "routes"
			}
			if variant == "pending" {
				fence["pending"] = true
			}
			path := filepath.Join(root.Run(), "global-operation.json")
			if err := platform.WriteJSONAtomic(path, fence, 0600); err != nil {
				t.Fatal(err)
			}
			original, _ := os.ReadFile(path)
			if variant == "symlink" {
				foreign := path + ".foreign"
				if err := os.Rename(path, foreign); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, path); err != nil {
					t.Fatal(err)
				}
			}
			page := map[string]any{"operationId": id, "operation": op, "state": "running", "running": true, "watchLock": root.LauncherLock(), "progressFile": filepath.Join(root.Data(), "state", "update", id+".jsonl"), "updatedAt": now}
			if variant == "wrong_operation" {
				page["operationId"] = "unrelated"
			}
			if err := platform.WriteJSONAtomic(filepath.Join(root.Data(), "state", "broray-operation.json"), page, 0600); err != nil {
				t.Fatal(err)
			}
			if variant != "not_running" {
				daemon, err := platform.TryLock(root.DaemonLock())
				if err != nil {
					t.Fatal(err)
				}
				defer daemon.Release()
				token, err := control.EnsureToken(root)
				if err != nil {
					t.Fatal(err)
				}
				ln, err := net.Listen("unix", root.DaemonSocket())
				if err != nil {
					t.Fatal(err)
				}
				mux := http.NewServeMux()
				control.Mount(mux, "3.5.1", token, func() {
					switch variant {
					case "already_released":
						_ = os.Remove(path)
					case "changed_inode":
						_ = platform.WriteFileAtomic(path, original, 0600)
					case "changed_bytes":
						fence["action"] = "other"
						b, _ := json.Marshal(fence)
						_ = os.WriteFile(path, b, 0600)
					}
					_ = daemon.Release()
				})
				srv := &http.Server{Handler: mux}
				go srv.Serve(ln)
				defer srv.Close()
			}
			dl, _, err := stopUpdateDaemon(root, 3*time.Second)
			if dl != nil {
				defer dl.Release()
			}
			if variant == "changed_inode" || variant == "changed_bytes" {
				if err == nil {
					t.Error("changed fence was not reported")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Lstat(path)
			wantAbsent := variant == "update" || variant == "reinstall" || variant == "already_released"
			if wantAbsent && !os.IsNotExist(statErr) {
				t.Error("owned updater fence survived confirmed daemon exit; next daemon cannot start")
			}
			if !wantAbsent && statErr != nil {
				t.Error("unproven fence removed")
			}
		})
	}
}
