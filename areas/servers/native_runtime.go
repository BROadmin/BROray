package servers

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

const localStorageMarker = "BROray XDRIVE local test storage v1\n"

// Local mode is confined to the installation's dedicated test namespace.
// Never adopt an existing unmarked directory or follow a link to another tree.
func prepareLocalStorage(root platform.Root, folder string) error {
	refuse := func() error {
		return errors.New("XDRIVE local: тестовый каталог не принадлежит этой установке BROray")
	}
	if folder != filepath.Join(root.Run(), "xdrive-local") {
		return refuse()
	}
	parent, err := platform.OpenDirNoFollow(root.Run())
	if err != nil {
		return refuse()
	}
	defer parent.Close()
	created := false
	dir, err := platform.OpenDirAtNoFollow(parent, "xdrive-local")
	if errors.Is(err, os.ErrNotExist) {
		if err := unix.Mkdirat(int(parent.Fd()), "xdrive-local", 0700); err != nil {
			return refuse()
		}
		created = true
		dir, err = platform.OpenDirAtNoFollow(parent, "xdrive-local")
	}
	if err != nil {
		return refuse()
	}
	defer dir.Close()
	st, err := dir.Stat()
	if err != nil || st.Mode().Perm() != 0700 {
		return refuse()
	}
	if created {
		if err := platform.WriteNewRegularAtNoFollow(dir, ".broray-owned", []byte(localStorageMarker), 0600); err != nil {
			return refuse()
		}
		if dir.Sync() != nil || parent.Sync() != nil {
			return refuse()
		}
	}
	marker, err := platform.ReadRegularAtNoFollow(dir, ".broray-owned")
	if err != nil || string(marker) != localStorageMarker {
		return refuse()
	}
	return nil
}

func (s *svc) prepareNativeConfig(cfg []byte) error {
	m, ok := decodeObject(cfg)
	if !ok {
		return nil
	} // the core reports malformed general JSON
	out, _ := m["outbounds"].([]any)
	for _, ob := range out {
		if get(ob, "streamSettings", "network") == "xdrive" && get(ob, "streamSettings", "xdriveSettings", "service") == "local" {
			if err := prepareLocalStorage(s.d.Root, asString(get(ob, "streamSettings", "xdriveSettings", "remoteFolder"))); err != nil {
				return err
			}
		}
	}
	return nil
}
