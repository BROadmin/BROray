package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"syscall"
)

// Download fetches artifact a (URL relative to manifestURL) into dir,
// verifies size and SHA-256 and returns the path of the installer binary
// (dir/broray, mode 0755). A tarball is never unpacked as a whole: only
// the regular member a.Entry is extracted, so absolute paths, ".." and
// links inside the archive have no effect.
func (f *Fetcher) Download(ctx context.Context, a Artifact, manifestURL, dir string, progress func(done, total int64)) (string, error) {
	u, err := resolve(manifestURL, a.URL)
	if err != nil {
		return "", fail(ErrArtifactInvalid, err, "Адрес установщика в манифесте недопустим.")
	}
	body, n, err := f.open(ctx, u)
	if err != nil {
		return "", fail(ErrDownloadFailed, err, "Не удалось скачать установщик новой версии.")
	}
	defer body.Close()
	if n >= 0 && n != a.Size {
		return "", fail(ErrDownloadSize, nil, "Размер установщика на сервере (%d байт) не совпадает с манифестом (%d байт).", n, a.Size)
	}
	part := filepath.Join(dir, "artifact.part")
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	pw := &progressWriter{total: a.Size, fn: progress}
	// One byte beyond the announced size is enough to detect an oversize body.
	got, err := io.Copy(io.MultiWriter(out, h, pw), io.LimitReader(body, a.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fail(ErrDownloadFailed, err, "Скачивание установщика прервано.")
	}
	if got != a.Size {
		kind := "короче"
		if got > a.Size {
			kind = "длиннее"
		}
		return "", fail(ErrDownloadSize, nil, "Скачанный установщик %s ожидаемого: %d байт вместо %d.", kind, got, a.Size)
	}
	if subtle.ConstantTimeCompare(h.Sum(nil), a.sha()) != 1 {
		return "", fail(ErrDownloadHash, nil, "SHA-256 скачанного установщика не совпадает с подписанным манифестом.")
	}
	bin := filepath.Join(dir, "broray")
	if a.Format == "tar.gz" {
		if err := extractEntry(part, a.Entry, a.UnpackedSize, bin); err != nil {
			return "", fail(ErrArtifactInvalid, err, "Архив установщика повреждён или не содержит %s.", a.Entry)
		}
		_ = os.Remove(part)
	} else if err := os.Rename(part, bin); err != nil {
		return "", err
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return "", err
	}
	return bin, nil
}

type progressWriter struct {
	done, total int64
	fn          func(done, total int64)
	last        int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil && (p.done-p.last >= 1<<20 || p.done >= p.total) {
		p.last = p.done
		p.fn(p.done, p.total)
	}
	return len(b), nil
}

func extractEntry(archive, entry string, size int64, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("entry %s not found", entry)
		}
		if err != nil {
			return err
		}
		if path.Clean(hdr.Name) != entry {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("entry %s is not a regular file", entry)
		}
		if hdr.Size != size {
			return fmt.Errorf("entry %s: %d bytes, manifest says %d", entry, hdr.Size, size)
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(tr, size+1))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if n != size {
			return fmt.Errorf("entry %s truncated", entry)
		}
		return nil
	}
}

// SpaceMargin is kept free on every filesystem after the download.
const SpaceMargin = 8 << 20

// CheckSpace verifies, before anything is written, that each filesystem
// holding one of the directories has room for the bytes requested there
// plus SpaceMargin. Directories on the same filesystem add up.
func CheckSpace(need map[string]int64) error {
	type fsNeed struct {
		dirs  []string
		bytes int64
		free  uint64
	}
	byDev := map[uint64]*fsNeed{}
	for dir, n := range need {
		var st syscall.Stat_t
		if err := syscall.Stat(dir, &st); err != nil {
			return err
		}
		var sf syscall.Statfs_t
		if err := syscall.Statfs(dir, &sf); err != nil {
			return err
		}
		dev := uint64(st.Dev)
		e := byDev[dev]
		if e == nil {
			e = &fsNeed{free: uint64(sf.Bavail) * uint64(sf.Bsize)}
			byDev[dev] = e
		}
		e.dirs = append(e.dirs, dir)
		e.bytes += n
	}
	for _, e := range byDev {
		want := uint64(e.bytes) + SpaceMargin
		if e.free < want {
			return fail(ErrSpaceRequired, nil, "Недостаточно места для обновления в %s: свободно %d МБ, нужно %d МБ. Освободите место (например, «Безопасная очистка») и повторите.",
				e.dirs[0], e.free>>20, (want+(1<<20)-1)>>20)
		}
	}
	return nil
}
