package xray

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

// XTLS ships both members in MIPS archives. Check the installed bytes, not
// merely the version string (which is identical for both binaries).
func TestPrepareSelectsArchitectureBinary(t *testing.T) {
	want := "xray"
	if runtime.GOARCH == "mips" || runtime.GOARCH == "mipsle" {
		want = "xray_softfloat"
	}
	for _, missing := range []bool{false, true} {
		name := "both-members"
		if missing {
			name = "required-member-missing"
		}
		t.Run(name, func(t *testing.T) {
			root := platform.Root{Dir: t.TempDir()}
			if err := os.MkdirAll(filepath.Dir(root.XrayBinary()), 0o700); err != nil {
				t.Fatal(err)
			}
			old := []byte("existing user-selected core")
			if err := os.WriteFile(root.XrayBinary(), old, 0o700); err != nil {
				t.Fatal(err)
			}
			payloads := map[string][]byte{
				"xray":           []byte("#!/bin/sh\n# default member\necho 'Xray 26.11.9'\n"),
				"xray_softfloat": []byte("#!/bin/sh\n# soft-float member\necho 'Xray 26.11.9'\n"),
			}
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			for _, member := range []string{"xray", "xray_softfloat"} {
				if missing && member == want {
					continue
				}
				w, err := zw.Create(member)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write(payloads[member]); err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(archive.Bytes())
			}))
			defer srv.Close()
			i := NewInstaller(root, "XTLS/Xray-core", "v26.11.9")
			i.BaseURL, i.SHA256 = srv.URL, fmt.Sprintf("%x", sha256.Sum256(archive.Bytes()))
			i.ValidateHeader = func(h []byte) error {
				if !bytes.HasPrefix(h, []byte("#!/bin/sh\n")) {
					return fmt.Errorf("invalid fixture")
				}
				return nil
			}
			path, version, err := i.Prepare(context.Background())
			unchanged, readErr := os.ReadFile(root.XrayBinary())
			if readErr != nil || !bytes.Equal(unchanged, old) {
				t.Fatal("Prepare changed the installed core")
			}
			if missing {
				if err == nil {
					t.Fatal("accepted an archive without the required architecture member")
				}
				if _, err := os.Stat(i.CandidatePath()); !os.IsNotExist(err) {
					t.Fatal("failed preparation left a candidate")
				}
				return
			}
			if err != nil || version != "26.11.9" {
				t.Fatalf("Prepare: version=%q err=%v", version, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, payloads[want]) {
				t.Fatalf("wrong installed member for %s: want %s", runtime.GOARCH, want)
			}
		})
	}
}
