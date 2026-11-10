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

func stagingZip(t *testing.T, names ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte("#!/bin/sh\necho 'Xray 26.11.9'\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestStagingArchiveDoesNotOccupyPersistentRoot(t *testing.T) {
	member := "xray"
	if runtime.GOARCH == "mips" || runtime.GOARCH == "mipsle" {
		member = "xray_softfloat"
	}
	data := stagingZip(t, member)
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	old := []byte("original installed core")
	if err := os.WriteFile(root.XrayBinary(), old, 0700); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	defer srv.Close()
	i := NewInstaller(root, "XTLS/Xray-core", "v26.11.9")
	i.BaseURL, i.SHA256 = srv.URL, fmt.Sprintf("%x", sha256.Sum256(data))
	i.ValidateHeader = func(h []byte) error {
		if !bytes.HasPrefix(h, []byte("#!/bin/sh\n")) {
			return fmt.Errorf("invalid fixture")
		}
		return nil
	}
	observed := false
	i.Progress = func(stage string, _, _ int64) {
		if stage != "extract" {
			return
		}
		observed = true
		_ = filepath.WalkDir(root.Dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				t.Error(err)
				return err
			}
			if !entry.IsDir() && filepath.Ext(path) == ".zip" {
				t.Errorf("archive occupies persistent root during extraction: %s", path)
			}
			return nil
		})
	}
	path, version, err := i.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if !observed || version != "26.11.9" {
		t.Fatalf("extract=%v version=%q", observed, version)
	}
	if got, err := os.ReadFile(root.XrayBinary()); err != nil || !bytes.Equal(got, old) {
		t.Fatal("preparation modified installed core")
	}
}

func TestStagingDuplicateMemberRejectedBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.zip")
	dest := filepath.Join(dir, "candidate")
	if err := os.WriteFile(archive, stagingZip(t, "xray", "xray"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := extractMember(archive, "xray", dest); err == nil {
		t.Fatal("ambiguous duplicate xray member accepted")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("invalid archive produced a candidate")
	}
}

func TestStagingRejectsSymlinkDestination(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.zip")
	dest := filepath.Join(dir, "candidate")
	external := filepath.Join(dir, "external")
	if err := os.WriteFile(archive, stagingZip(t, "xray"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, dest); err != nil {
		t.Fatal(err)
	}
	if err := extractMember(archive, "xray", dest); err == nil {
		t.Error("symlink destination accepted")
	}
	if got, _ := os.ReadFile(external); string(got) != "preserve" {
		t.Fatal("external file overwritten")
	}
}
