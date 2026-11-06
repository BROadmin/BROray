package xray

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// Installer downloads the pinned Xray release for this architecture,
// verifies the archive SHA-256, extracts the binary and swaps it in only after
// "xray version" succeeds. geoip/geosite are not extracted (unused, like 3.x).
type Installer struct {
	Root     platform.Root
	Repo     string // XTLS/Xray-core
	Tag      string // v26.10.31
	Client   *http.Client
	Progress func(stage string, done, total int64)
	// BaseURL is the release host ("https://github.com" when empty; tests).
	BaseURL string
	// SHA256 is the confirmed official archive digest of Tag (from the GitHub
	// API or the official .dgst). Empty → the digest pinned in this build.
	SHA256 string
}

func NewInstaller(root platform.Root, repo, tag string) *Installer {
	tr := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 60 * time.Second}
	return &Installer{Root: root, Repo: repo, Tag: tag, Client: &http.Client{Transport: tr, Timeout: 30 * time.Minute}}
}

func (i *Installer) report(stage string, done, total int64) {
	if i.Progress != nil {
		i.Progress(stage, done, total)
	}
}

// Expected returns the asset name and pinned digest for this build.
func (i *Installer) Expected() (Asset, string, error) {
	a, err := CurrentAsset()
	if err != nil {
		return a, "", err
	}
	if i.SHA256 != "" {
		return a, i.SHA256, nil
	}
	digests, ok := Pinned[i.Tag]
	if !ok {
		return a, "", fmt.Errorf("XRAY_VERSION_NOT_PINNED: %s", i.Tag)
	}
	d, ok := digests[a.Zip]
	if !ok {
		return a, "", fmt.Errorf("XRAY_ASSET_NOT_PINNED: %s %s", i.Tag, a.Zip)
	}
	return a, d, nil
}

// Install performs the full download → verify → extract → test → swap.
func (i *Installer) Install(ctx context.Context) (string, error) {
	newBin, v, err := i.Prepare(ctx)
	if err != nil {
		return "", err
	}
	defer os.Remove(newBin)
	if err := os.Rename(newBin, i.Root.XrayBinary()); err != nil {
		return "", err
	}
	_ = platform.SyncDir(i.Root.Runtime())
	i.report("done", 0, 0)
	return v, nil
}

// CandidatePath is where Prepare leaves the verified binary (runtime/xray.new).
func (i *Installer) CandidatePath() string { return i.Root.XrayBinary() + ".new" }

// Prepare downloads the pinned archive, verifies its SHA-256, extracts the
// binary to CandidatePath() and checks that "xray version" runs. It never
// touches the installed binary; the caller swaps (and removes the
// candidate on failure).
func (i *Installer) Prepare(ctx context.Context) (string, string, error) {
	asset, want, err := i.Expected()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(i.Root.Tmp(), 0o700); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(i.Root.Runtime(), 0o755); err != nil {
		return "", "", err
	}
	zipPath := filepath.Join(i.Root.Tmp(), "xray-"+i.Tag+".zip")
	defer os.Remove(zipPath)
	base := i.BaseURL
	if base == "" {
		base = "https://github.com"
	}
	url := base + "/" + i.Repo + "/releases/download/" + i.Tag + "/" + asset.Zip
	if err := i.download(ctx, url, zipPath, want); err != nil {
		return "", "", err
	}
	i.report("extract", 0, 0)
	newBin := i.CandidatePath()
	if err := extractMember(zipPath, "xray", newBin); err != nil {
		_ = os.Remove(newBin)
		return "", "", err
	}
	i.report("verify", 0, 0)
	v, err := Version(ctx, newBin)
	if err != nil {
		_ = os.Remove(newBin)
		return "", "", err
	}
	return newBin, v, nil
}

func (i *Installer) download(ctx context.Context, url, dest, wantSHA string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "BROray/3.5.0")
	resp, err := i.Client.Do(req)
	if err != nil {
		return fmt.Errorf("XRAY_DOWNLOAD_FAILED: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("XRAY_DOWNLOAD_FAILED: HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	total := resp.ContentLength
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			h.Write(buf[:n])
			done += int64(n)
			i.report("download", done, total)
			if done > 128<<20 {
				f.Close()
				return errors.New("XRAY_DOWNLOAD_TOO_LARGE")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return fmt.Errorf("XRAY_DOWNLOAD_FAILED: %v", rerr)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		return fmt.Errorf("XRAY_DIGEST_MISMATCH: got %s want %s", got, wantSHA)
	}
	return nil
}

func extractMember(zipPath, member, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("XRAY_ARCHIVE_INVALID: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != member {
			continue
		}
		if f.UncompressedSize64 > 256<<20 {
			return errors.New("XRAY_ARCHIVE_MEMBER_TOO_LARGE")
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(rc, 256<<20)); err != nil {
			out.Close()
			return err
		}
		if err := out.Sync(); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("XRAY_ARCHIVE_MEMBER_MISSING: %s", member)
}
