package update

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

func TestInstallerOversizedOutputFailsWithoutHanging(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "installer")
	script := `#!/bin/sh
printf '%s\n' '{"event":"result","ok":true,"state":"committed"}'
i=0
while [ "$i" -lt 300 ]; do
  printf '%s' '` + strings.Repeat("x", 4096) + `'
  i=$((i + 1))
done
printf '\n'
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	outcome := make(chan error, 1)
	go func() {
		_, err := runInstaller(ApplyOptions{
			Root: platform.Root{Dir: dir}, Source: Source{Channel: "test"},
			Current: "3.5.0-dev5", Stderr: io.Discard,
		}, NewEmitter(io.Discard, "test"), bin, "update")
		outcome <- err
	}()
	select {
	case err := <-outcome:
		if Code(err) != ErrInstallerFailed {
			t.Fatalf("oversized installer output was accepted: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("launcher blocked after installer stdout exceeded scanner limit")
	}
}
