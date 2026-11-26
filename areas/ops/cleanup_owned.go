package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"golang.org/x/sys/unix"
)

const cleanupGeodataPrefix = "runtime/" + geodata.AssetDirectory + "/"

var cleanupVersionPattern = regexp.MustCompile("^[0-9]+\\.[0-9]+\\.[0-9]+$")

func cleanupDiskAvailable(path string) (int64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}

func (a *area) addCleanupOwned(ctx context.Context, lines *[]cleanupLine) {
	a.addObsoleteVersions(lines)
	*lines = append(*lines, a.preImportCleanup("")...)
	store, err := geodata.Open(filepath.Join(a.d.Root.Runtime(), geodata.AssetDirectory))
	if err != nil {
		return
	}
	defer store.Close()
	duplicates, err := store.DuplicatePlan(ctx)
	if err != nil {
		// A missing, damaged or busy geodata store does not authorize changes.
		return
	}
	for _, item := range duplicates {
		*lines = append(*lines, cleanupLine{"geodata", "consolidate", item.Bytes, cleanupGeodataPrefix + item.Path, item.Identity})
	}
}

func (a *area) consolidateCleanupGeodata(ctx context.Context, expected map[string]string) (int, error) {
	store, err := geodata.Open(filepath.Join(a.d.Root.Runtime(), geodata.AssetDirectory))
	if err != nil {
		return 0, err
	}
	defer store.Close()
	return store.Consolidate(ctx, expected)
}

func (a *area) addObsoleteVersions(lines *[]cleanupLine) {
	raw, err := platform.ReadRegularNoFollow(a.d.Root.UpdateJournal())
	if err != nil {
		return
	}
	var journal installer.Journal
	if json.Unmarshal(raw, &journal) != nil || journal.SchemaVersion != 1 || journal.State != installer.JCommitted {
		return
	}
	active, err := os.Readlink(a.d.Root.Current())
	if err != nil || !cleanupVersionPattern.MatchString(journal.To) || active != filepath.Join("versions", journal.To) || active != journal.NewTarget {
		return
	}
	for _, record := range journal.OwnedVersions {
		if !cleanupVersionPattern.MatchString(record.Version) {
			continue
		}
		rel := filepath.Join("versions", record.Version)
		if rel == active || rel == journal.PreviousTarget || record.Version == journal.From {
			continue
		}
		var pinned []cleanupLine
		path := filepath.Join(a.d.Root.Dir, rel)
		a.candidate(&pinned, "versions", path)
		if len(pinned) == 1 && cleanupUnmounted(path) && a.verifiedObsoleteVersion(rel, record.SHA256) {
			a.addPinnedCleanup(lines, pinned[0])
		}
	}
}

func (a *area) addPinnedCleanup(lines *[]cleanupLine, pinned cleanupLine) {
	var current []cleanupLine
	a.candidate(&current, pinned.category, filepath.Join(a.d.Root.Dir, pinned.rel))
	if len(current) == 1 && current[0] == pinned {
		*lines = append(*lines, pinned)
	}
}

func cleanupUnmounted(path string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	unescape := strings.NewReplacer("\\040", " ", "\\011", "\t", "\\012", "\n", "\\134", "\\")
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mount := unescape.Replace(fields[4])
		if mount == path || strings.HasPrefix(mount, path+"/") {
			return false
		}
	}
	return true
}

func (a *area) verifiedObsoleteVersion(rel, digest string) bool {
	handle, err := a.openCleanup(rel)
	if err != nil {
		return false
	}
	defer handle.close()
	entries, err := handle.file.ReadDir(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	if len(entries) != 1 || entries[0].Name() != "broray" || !entries[0].Type().IsRegular() {
		return false
	}
	data, err := platform.ReadRegularAtNoFollow(handle.file, "broray")
	if err != nil || compat.SHA256Hex(data) != digest {
		return false
	}
	var binary unix.Stat_t
	if err := unix.Fstatat(int(handle.file.Fd()), "broray", &binary, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		binary.Mode&unix.S_IFMT != unix.S_IFREG {
		return false
	}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, process := range processes {
		if !process.IsDir() {
			continue
		}
		var executable unix.Stat_t
		if err := unix.Stat(filepath.Join("/proc", process.Name(), "exe"), &executable); err == nil &&
			binary.Dev == executable.Dev && binary.Ino == executable.Ino {
			return false
		}
	}
	return true
}

func (a *area) preImportCleanup(newestID string) []cleanupLine {
	dir, err := platform.OpenDirNoFollow(a.backupDir())
	if err != nil {
		return nil
	}
	defer dir.Close()
	if incomplete, err := a.incompleteBackupCopies(); err != nil || len(incomplete) != 0 {
		return nil
	}
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil
	}
	type verifiedCopy struct {
		stamp time.Time
		entry cleanupLine
	}
	verified := make(map[string]verifiedCopy)
	var newest time.Time
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || !reBackupCopyID.MatchString(id) {
			continue
		}
		var pinned []cleanupLine
		path := a.backupDir(id)
		if !cleanupUnmounted(path) {
			continue
		}
		a.candidate(&pinned, "preImport", path)
		if len(pinned) != 1 {
			continue
		}
		if stamp, ok := a.verifiedPreImportCopy(id); ok {
			var current []cleanupLine
			a.addPinnedCleanup(&current, pinned[0])
			if len(current) != 1 {
				continue
			}
			verified[id] = verifiedCopy{stamp, pinned[0]}
			if stamp.After(newest) {
				newest = stamp
			}
		}
	}
	if newestID != "" {
		copy, ok := verified[newestID]
		if !ok {
			return nil
		}
		newest = copy.stamp
	}
	var lines []cleanupLine
	for id, copy := range verified {
		if copy.stamp.Before(newest) || (newestID != "" && id != newestID && copy.stamp.Equal(newest)) {
			lines = append(lines, copy.entry)
		}
	}
	return lines
}

func (a *area) verifiedPreImportCopy(id string) (time.Time, bool) {
	raw, err := platform.ReadRegularNoFollow(a.backupDir(id, "manifest.json"))
	var man copyManifest
	if err != nil || json.Unmarshal(raw, &man) != nil || man.SchemaVersion != 1 ||
		man.Kind != "pre-import" || !man.Complete || man.Recovered || len(man.Paths) == 0 {
		return time.Time{}, false
	}
	stamp, err := time.Parse(time.RFC3339, man.CreatedAt)
	if err != nil {
		return time.Time{}, false
	}
	copyRoot := a.backupDir(id, backupCopyRoot)
	paths := make(map[string]bool, len(man.Paths))
	for _, entry := range man.Paths {
		if !copyPathAllowed(entry.Path) || entry.SHA256 == "" || paths[entry.Path] {
			return time.Time{}, false
		}
		paths[entry.Path] = true
	}
	copyDir := a.backupDir(id)
	err = filepath.WalkDir(copyDir, func(path string, file fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == copyDir || path == copyRoot || path == filepath.Join(copyDir, "manifest.json") {
			if file.IsDir() || file.Type().IsRegular() {
				return nil
			}
			return fs.ErrInvalid
		}
		rel, err := filepath.Rel(copyRoot, path)
		if err != nil || (!file.IsDir() && !file.Type().IsRegular()) {
			return fs.ErrInvalid
		}
		for owned := range paths {
			if rel == owned || strings.HasPrefix(rel, owned+"/") || (file.IsDir() && strings.HasPrefix(owned, rel+"/")) {
				return nil
			}
		}
		return fs.ErrInvalid
	})
	if err != nil {
		return time.Time{}, false
	}
	for _, entry := range man.Paths {
		path := filepath.Join(copyRoot, entry.Path)
		if !entry.Existed {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return time.Time{}, false
			}
			continue
		}
		dir, err := platform.OpenDirNoFollow(filepath.Dir(path))
		if err != nil {
			return time.Time{}, false
		}
		sum, err := compat.BackupDigest(copyRoot, entry.Path, nil)
		dir.Close()
		if err != nil || entry.OriginalSHA256 == "" || sum != entry.OriginalSHA256 {
			return time.Time{}, false
		}
	}
	return stamp, true
}
