package xray

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const MemoryReserve = int64(32 << 20)
const ArchiveLimit = int64(128 << 20)
const BinaryLimit = int64(256 << 20)

// Archive is a digest-verified official ZIP in a private RAM directory. Its
// sizes refer to the selected architecture member, never a compression ratio.
type Archive struct {
	Path        string
	Member      string
	BinarySize  int64
	ArchiveSize int64
}

func (a *Archive) Close() error {
	if a == nil {
		return nil
	}
	err := os.Remove(a.Path)
	if os.IsNotExist(err) {
		err = nil
	}
	// Never recursively delete: a retained recovery copy may share the directory.
	_ = os.Remove(filepath.Dir(a.Path))
	return err
}

// RAMAvailable measures both the actual filesystem limit and currently
// available memory. /tmp is not assumed to be a RAM filesystem by its name.
func RAMAvailable(path string) (int64, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return 0, err
	}
	if uint32(fs.Type) != uint32(unix.TMPFS_MAGIC) && uint32(fs.Type) != uint32(unix.RAMFS_MAGIC) {
		return 0, errors.New("XRAY_RAM_FILESYSTEM_REQUIRED")
	}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var available, free int64
	var hasAvailable bool
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || n < 0 || n > 1<<50 {
			continue
		}
		switch fields[0] {
		case "MemAvailable:":
			available, hasAvailable = n*1024, true
		case "MemFree:":
			free = n * 1024
		}
	}
	if err := scan.Err(); err != nil {
		return 0, err
	}
	// Old kernels without MemAvailable: use free pages only, not an optimistic
	// cache estimate that could include unreclaimable tmpfs pages.
	if !hasAvailable {
		available = free
	}
	if fs.Type == unix.TMPFS_MAGIC {
		limit := uint64(fs.Bavail) * uint64(fs.Bsize)
		if limit < uint64(available) {
			available = int64(limit)
		}
	}
	return available, nil
}

func selectedMember() string {
	if runtime.GOARCH == "mips" || runtime.GOARCH == "mipsle" {
		return "xray_softfloat"
	}
	return "xray"
}

func archiveMember(zr *zip.ReadCloser, member string) (*zip.File, error) {
	var found *zip.File
	for _, f := range zr.File {
		if f.Name != member {
			continue
		}
		if found != nil {
			return nil, errors.New("XRAY_ARCHIVE_MEMBER_DUPLICATE")
		}
		if !f.Mode().IsRegular() || f.UncompressedSize64 == 0 || f.UncompressedSize64 > uint64(BinaryLimit) {
			return nil, errors.New("XRAY_ARCHIVE_MEMBER_INVALID")
		}
		found = f
	}
	if found == nil {
		return nil, fmt.Errorf("XRAY_ARCHIVE_MEMBER_MISSING: %s", member)
	}
	return found, nil
}

func (i *Installer) DownloadArchive(ctx context.Context) (*Archive, error) {
	asset, want, err := i.Expected()
	if err != nil {
		return nil, err
	}
	// Fixed parent; do not accept TMPDIR from an inherited shell environment.
	st, err := os.Lstat("/tmp")
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("XRAY_RAM_DIRECTORY_INVALID")
	}
	available, err := RAMAvailable("/tmp")
	if err != nil {
		return nil, err
	}
	if available <= MemoryReserve {
		return nil, errors.New("XRAY_RAM_INSUFFICIENT")
	}
	dir, err := os.MkdirTemp("/tmp", "broray-xray-")
	if err != nil {
		return nil, err
	}
	a := &Archive{Path: filepath.Join(dir, "archive.zip"), Member: selectedMember()}
	success := false
	defer func() {
		if !success {
			_ = a.Close()
		}
	}()
	if i.StagingCreated != nil {
		if err := i.StagingCreated(a); err != nil {
			return nil, err
		}
	}
	base := i.BaseURL
	if base == "" {
		base = "https://github.com"
	}
	url := base + "/" + i.Repo + "/releases/download/" + i.Tag + "/" + asset.Zip
	if err := i.download(ctx, url, a.Path, want); err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(a.Path)
	if err != nil {
		return nil, fmt.Errorf("XRAY_ARCHIVE_INVALID: %w", err)
	}
	f, err := archiveMember(zr, a.Member)
	if err == nil {
		err = i.validateMember(ctx, f)
		if err == nil {
			a.BinarySize = int64(f.UncompressedSize64)
		}
	}
	_ = zr.Close()
	if err != nil {
		return nil, err
	}
	st, err = os.Stat(a.Path)
	if err != nil {
		return nil, err
	}
	a.ArchiveSize = st.Size()
	success = true
	return a, nil
}

func (i *Installer) validateMember(ctx context.Context, f *zip.File) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	buf := make([]byte, 64<<10)
	header := make([]byte, 0, 64)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, err := r.Read(buf)
		n += int64(read)
		if n > BinaryLimit || n > int64(f.UncompressedSize64) {
			return errors.New("XRAY_ARCHIVE_SIZE_MISMATCH")
		}
		if len(header) < 64 {
			count := min(64-len(header), read)
			header = append(header, buf[:count]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("XRAY_ARCHIVE_STREAM_INVALID: %w", err)
		}
	}
	if n != int64(f.UncompressedSize64) {
		return errors.New("XRAY_ARCHIVE_SIZE_MISMATCH")
	}
	validate := i.ValidateHeader
	if validate == nil {
		validate = validateCoreELF
	}
	return validate(header)
}

func validateCoreELF(h []byte) error {
	if len(h) < 64 || string(h[:4]) != "\x7fELF" || h[6] != 1 {
		return errors.New("XRAY_EXECUTABLE_FORMAT_INVALID")
	}
	var order binary.ByteOrder = binary.LittleEndian
	if h[5] == 2 {
		order = binary.BigEndian
	} else if h[5] != 1 {
		return errors.New("XRAY_EXECUTABLE_FORMAT_INVALID")
	}
	machine := order.Uint16(h[18:20])
	kind := order.Uint16(h[16:18])
	ok := false
	switch runtime.GOARCH {
	case "arm64":
		ok = h[4] == 2 && h[5] == 1 && machine == 183
	case "arm":
		ok = h[4] == 1 && h[5] == 1 && machine == 40
	case "mips":
		ok = h[4] == 1 && h[5] == 2 && machine == 8
	case "mipsle":
		ok = h[4] == 1 && h[5] == 1 && machine == 8
	}
	if !ok || kind != 2 && kind != 3 {
		return errors.New("XRAY_EXECUTABLE_ARCHITECTURE_MISMATCH")
	}
	return nil
}

func (i *Installer) ExtractArchive(ctx context.Context, a *Archive) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if a == nil || a.Member != selectedMember() {
		return "", "", errors.New("XRAY_ARCHIVE_INVALID")
	}
	if err := os.MkdirAll(i.Root.Runtime(), 0755); err != nil {
		return "", "", err
	}
	i.report("extract", 0, a.BinarySize)
	path := i.CandidatePath()
	if err := extractMember(a.Path, a.Member, path); err != nil {
		return "", "", err
	}
	i.report("verify", 0, 0)
	v, err := Version(ctx, path)
	if err != nil {
		_ = os.Remove(path)
		return "", "", err
	}
	return path, v, nil
}
