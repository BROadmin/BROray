package keenetic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dev3 KN-1811: routes export failed "save before install" because Save
// compared RCI running-config with ndmc startup-config (RCI has no
// "more startup-config" and renders non-ASCII descriptions differently).
// With /bin/ndmc present, both are read from ndmc.
func TestAutoReadsConfigFromNDMC(t *testing.T) {
	rci := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":["interface Proxy0","    description \"BROray - Сервер\"","!"]}`))
	}))
	defer rci.Close()
	dir := t.TempDir()
	script := filepath.Join(dir, "ndmc")
	body := "#!/bin/sh\nprintf 'interface Proxy0\\n    description \"BROray - \\\\xd0\\\\xa1\"\\n!\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &Auto{Primary: NewRCI(rci.URL), Fallback: &NDMC{Binary: script, Timeout: 5 * time.Second}}
	ctx := context.Background()
	run, err := a.RunningConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start, err := a.StartupConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(run) != 3 || run[1] != `    description "BROray - \xd0\xa1"` || !equalConfig(run, start) {
		t.Fatalf("running %q startup %q", run, start)
	}
	// without ndmc the RCI form is used
	b := &Auto{Primary: NewRCI(rci.URL), Fallback: &NDMC{Binary: filepath.Join(dir, "missing"), Timeout: time.Second}}
	if run, err := b.RunningConfig(ctx); err != nil || run[1] != `    description "BROray - Сервер"` {
		t.Fatalf("rci: %q %v", run, err)
	}
}
