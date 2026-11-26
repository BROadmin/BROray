package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/installstats"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/web"
)

func TestInstallStatisticsObservesExecutingWebUI(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	rci := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rci/show/version":
			_, _ = io.WriteString(w, `{"hw_id":"KN-2710","unrelated":"never-include"}`)
		case "/rci/show/identification":
			_, _ = io.WriteString(w, `{"serial":"SYNTHETIC-880179","unrelated":"never-include"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer rci.Close()
	cfg := platform.DefaultConfig()
	cfg.Keenetic.RCIURL = rci.URL
	cfg.Keenetic.WriteTransport = "rci"
	a := app.New(root, cfg, log.New(io.Discard, "", 0))
	ui := httptest.NewServer(web.NewServer(a, nil))
	defer ui.Close()
	hash, err := installstats.ExecutableDigest("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	live, ok := observeInstallStatisticsBuild(context.Background(), a, hash)
	if !ok || live.SHA256 != hash || live.Version != app.Version || live.Revision != app.Revision {
		t.Fatal("the updater could not observe the exact executing build")
	}
	if _, ok := observeInstallStatisticsBuild(context.Background(), a, "wrong"); ok {
		t.Fatal("the updater accepted an unrelated executable")
	}
	if err := installstats.NewQueue(root).ArmFirstInstall(app.Version, hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	intent := filepath.Join(root.State(), "install-statistics", "first-install.json")
	waitForCancel, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runInstallStatistics(waitForCancel, a, ui.Listener.Addr().String())
	}()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(intent); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("local UI did not confirm the executing build")
		}
		time.Sleep(20 * time.Millisecond)
	}
	events, err := filepath.Glob(filepath.Join(filepath.Dir(intent), "event-*.json"))
	if err != nil || len(events) != 1 {
		t.Fatalf("expected one durable event, found %d: %v", len(events), err)
	}
	data, err := os.ReadFile(events[0])
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Event json.RawMessage }
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	event, err := installstats.ParseEvent(envelope.Event)
	want := installstats.RouterIdentity("KN-2710", "SYNTHETIC-880179")
	if err != nil || event.Type != "install" || event.Version != app.Version ||
		event.Revision != app.Revision || event.Model != want.Model || event.DeviceKey != want.DeviceKey {
		t.Fatalf("unexpected event from running WebUI: %v", err)
	}
}
