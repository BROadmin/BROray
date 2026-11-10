package xray

import (
	"errors"
	"github.com/BROadmin/broray/internal/platform"
	mxray "github.com/BROadmin/broray/modules/xray"
	"os"
	"path/filepath"
	"strings"
)

func (s *service) downloadPath() string { return filepath.Join(s.d.Root.State(), "xray-download.json") }
func (s *service) recordDownload(a *mxray.Archive) error {
	if _, err := os.Lstat(s.downloadPath()); !os.IsNotExist(err) {
		return errors.New("XRAY_DOWNLOAD_RECOVERY_REQUIRED")
	}
	return platform.WriteJSONAtomic(s.downloadPath(), map[string]string{"archive": a.Path}, 0600)
}
func (s *service) recoverDownload() {
	st, err := os.Lstat(s.downloadPath())
	if err != nil || !st.Mode().IsRegular() || st.Size() > 4096 {
		return
	}
	var m map[string]string
	if platform.ReadJSON(s.downloadPath(), &m) != nil {
		return
	}
	p := m["archive"]
	dir := filepath.Dir(p)
	if filepath.Base(p) != "archive.zip" || filepath.Dir(dir) != "/tmp" || !strings.HasPrefix(filepath.Base(dir), "broray-xray-") {
		return
	}
	st, err = os.Lstat(dir)
	if os.IsNotExist(err) {
		_ = os.Remove(s.downloadPath())
		return
	}
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return
	}
	if _, err := mxray.RAMAvailable(dir); err != nil {
		return
	}
	if st, err := os.Lstat(p); err == nil {
		if !st.Mode().IsRegular() || os.Remove(p) != nil {
			return
		}
	} else if !os.IsNotExist(err) {
		return
	}
	_ = os.Remove(dir) // a RAM rollback may still exist here; never recursive
	_ = os.Remove(s.downloadPath())
}
