package xray

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/BROadmin/broray/internal/platform"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStagingRejectsWrongArchitectureAndCRCBeforeExtraction(t *testing.T) {
	for _, kind := range []string{"wrong-architecture", "crc"} {
		t.Run(kind, func(t *testing.T) {
			var data bytes.Buffer
			zw := zip.NewWriter(&data)
			f, err := zw.CreateHeader(&zip.FileHeader{Name: selectedMember(), Method: zip.Store})
			if err != nil {
				t.Fatal(err)
			}
			hdr := make([]byte, 128)
			copy(hdr, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
			binary.LittleEndian.PutUint16(hdr[16:], 2)
			binary.LittleEndian.PutUint16(hdr[18:], 62) // x86-64, never a supported router.
			if _, err := f.Write(hdr); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			archive := data.Bytes()
			if kind == "crc" {
				at := bytes.Index(archive, []byte{0x7f, 'E', 'L', 'F'})
				archive[at+80] ^= 0x80
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) }))
			defer srv.Close()
			inst := NewInstaller(platform.Root{Dir: t.TempDir()}, "XTLS/Xray-core", "v26.11.9")
			inst.BaseURL = srv.URL
			inst.SHA256 = fmt.Sprintf("%x", sha256.Sum256(archive))
			a, err := inst.DownloadArchive(context.Background())
			if a != nil {
				defer a.Close()
			}
			if err == nil {
				t.Fatalf("approved archive with %s accepted before destructive step", kind)
			}
		})
	}
}
