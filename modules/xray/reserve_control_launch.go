package xray

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

// prepareReserveControlLaunch runs with the inherited child guard held. Only
// the reserved API configuration can reclaim a path in its private namespace.
func prepareReserveControlLaunch(root platform.Root, config string) error {
	const reservedTag = "broray-reserve-control"
	path := ReserveControlSocket(root)
	fd, err := unix.Open(config, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil // The existing Xray launch handles unreadable ordinary configs.
	}
	f := os.NewFile(uintptr(fd), config)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil
	}
	const maxConfig = 2 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxConfig+1))
	_ = f.Close()
	var layout struct {
		API struct {
			Tag      string   `json:"tag"`
			Listen   string   `json:"listen"`
			Services []string `json:"services"`
		} `json:"api"`
	}
	if err != nil || len(data) > maxConfig || json.Unmarshal(data, &layout) != nil {
		return nil
	}
	api := layout.API
	address, _ := strings.CutSuffix(api.Listen, ",0600")
	if api.Tag != reservedTag && address != path {
		return nil
	}
	if api.Tag != reservedTag || address != path || len(api.Services) != 1 || api.Services[0] != "RoutingService" ||
		!filepath.IsAbs(path) || len(path) > 107 || strings.ContainsAny(path, "\x00\t\r\n ,") {
		return ErrReserveSelectorUnconfirmed
	}
	return preparePrivateReserveSocket(root, path)
}

func preparePrivateReserveSocket(root platform.Root, path string) error {
	parent, _, err := reserveControlDirectory(root.Run())
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := unix.Mkdirat(int(parent.Fd()), "reserve-control", 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return ErrReserveSelectorUnconfirmed
	}
	dir, info, err := reserveControlDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	fd, name := int(dir.Fd()), filepath.Base(path)
	var initial unix.Stat_t
	if err := unix.Fstatat(fd, name, &initial, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return ErrReserveSelectorUnconfirmed
	}
	if initial.Mode&unix.S_IFMT != unix.S_IFSOCK || initial.Uid != uint32(os.Geteuid()) {
		return ErrReserveSelectorUnconfirmed
	}
	conn, err := net.DialTimeout("unix", path, 75*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return ErrReserveSelectorUnconfirmed
	}
	if !errors.Is(err, unix.ECONNREFUSED) || !reserveSocketAbsentFromKernel(path) {
		return ErrReserveSelectorUnconfirmed
	}
	currentDir, currentInfo, err := reserveControlDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer currentDir.Close()
	var current unix.Stat_t
	if !os.SameFile(info, currentInfo) || unix.Fstatat(fd, name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		initial.Dev != current.Dev || initial.Ino != current.Ino || initial.Mode != current.Mode ||
		initial.Uid != current.Uid || initial.Ctim != current.Ctim {
		return ErrReserveSelectorUnconfirmed
	}
	if err := unix.Unlinkat(fd, name, 0); err != nil {
		return ErrReserveSelectorUnconfirmed
	}
	return nil
}

func reserveSocketAbsentFromKernel(path string) bool {
	f, err := os.Open("/proc/net/unix")
	if err != nil {
		return false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() || !strings.HasPrefix(s.Text(), "Num ") || !strings.Contains(s.Text(), "Inode Path") {
		return false
	}
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 7 || len(fields) >= 8 && fields[7] == path {
			return false
		}
	}
	return s.Err() == nil
}
