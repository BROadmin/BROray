package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func TestCtlStopRejectsAmbiguousDaemonLock(t *testing.T) {
	root := platform.Root{Dir: filepath.Join(t.TempDir(), "b")}
	if err := os.MkdirAll(root.Run(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root.DaemonLock(), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", root.DaemonSocket())
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	if code := runCtl(root, []string{"stop"}); code == 0 {
		t.Fatal("ctl stop reported success without verifying the daemon lock")
	}
	select {
	case request := <-requests:
		if request != "POST /api/v1/daemon/stop" {
			t.Fatalf("unexpected control request: %s", request)
		}
	default:
		t.Fatal("daemon stop request was not accepted by the mock")
	}
}
