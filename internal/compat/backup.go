package compat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/BROadmin/broray/internal/platform"
)

// Settings backup (BROray page «Резервная копия настроек»).
//
// Every area that owns settings registers a BackupPart: it exports its share
// of the file and, on import, validates that share and writes the merged
// result into a staging root that mirrors the installation root, with its
// own store functions on a Deps whose Root is the staging root. areas/ops
// then swaps the staged paths with the live ones (SwapBackupPaths); the
// replaced originals form the pre-import copy, so an import can be undone
// by swapping them back. Parts never write the live tree themselves; their
// After hook may run normal follow-up work (re-activating the active server)
// while the caller still holds the global fence.

// BackupChange is one previewed change of an import.
type BackupChange struct {
	Kind   string `json:"kind"` // server | subscription | customRoutes | setting
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Action string `json:"action"` // add | replace | unchanged | remove | keep
	Detail string `json:"detail,omitempty"`
}

// BackupPlan is what a part's import would change.
type BackupPlan struct {
	Changes   []BackupChange `json:"changes"`
	FollowUps []string       `json:"followUps"` // owner actions after the import (Keenetic changes are never automatic)
	Notes     []string       `json:"notes"`
}

// Add appends a change.
func (p *BackupPlan) Add(kind, id, name, action, detail string) {
	p.Changes = append(p.Changes, BackupChange{Kind: kind, ID: id, Name: name, Action: action, Detail: detail})
}

// BackupPart is one area's share of the settings backup.
type BackupPart struct {
	Name  string // key in the file's "parts" object
	Order int    // stage order (servers before subscriptions: the latter edit the staged servers dir)
	// Export returns the part's JSON value (nil: the part is omitted).
	Export func(ctx context.Context) (any, error)
	// Stage validates the part's value from a backup and writes the merged
	// result into st. Errors are "CODE: message" texts shown to the owner.
	Stage func(ctx context.Context, raw json.RawMessage, st *BackupStage) (BackupPlan, error)
	// Before captures live state before the swap; After runs after it
	// (global fence held) and reports notes and follow-ups. Both optional.
	Before func() any
	After  func(ctx context.Context, before any) BackupPlan
}

var (
	backupMu    sync.Mutex
	backupParts = map[string]BackupPart{}
)

// RegisterBackupPart registers (or replaces) an area's backup part.
func RegisterBackupPart(p BackupPart) {
	backupMu.Lock()
	defer backupMu.Unlock()
	backupParts[p.Name] = p
}

// BackupParts returns the registered parts in stage order.
func BackupParts() []BackupPart {
	backupMu.Lock()
	defer backupMu.Unlock()
	out := make([]BackupPart, 0, len(backupParts))
	for _, p := range backupParts {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ---------- staging ----------

// BackupStage is a staging root for one import. Paths are relative to the
// installation root ("data/servers", "config/config.v1.json").
type BackupStage struct {
	Live *Deps
	St   *Deps // Root = staging dir; no App, Queue or loops: store functions only
	Dir  string

	copied map[string]bool
	swap   map[string]bool
	fields map[string][]string
}

// NewBackupStage creates an empty staging root in dir.
func NewBackupStage(live *Deps, dir string) (*BackupStage, error) {
	root := platform.Root{Dir: dir}
	if err := os.MkdirAll(root.Data(), 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root.Run(), 0o700); err != nil {
		return nil, err
	}
	st := &Deps{Root: root, Log: live.Log, Ctx: live.Ctx, Svc: &Services{}, Maintenance: true}
	st.Lock = NewGlobalLock(st.RunPath("global-operation.json"))
	return &BackupStage{Live: live, St: st, Dir: dir, copied: map[string]bool{}, swap: map[string]bool{}, fields: map[string][]string{}}, nil
}

// DataRel is the root-relative form of a data path ("servers" → "data/servers").
func DataRel(parts ...string) string {
	return filepath.ToSlash(filepath.Join(append([]string{"data"}, parts...)...))
}

func cleanRel(rel string) (string, error) {
	c := filepath.Clean(filepath.FromSlash(rel))
	if rel == "" || filepath.IsAbs(c) || c == "." || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("BACKUP_PATH_INVALID: недопустимый путь %q", rel)
	}
	return c, nil
}

// Copy copies the live path into the stage once (a missing live path is
// fine), without marking it for the swap: read-only input of a stage writer.
func (s *BackupStage) Copy(rel string) error {
	c, err := cleanRel(rel)
	if err != nil {
		return err
	}
	if s.copied[c] {
		return nil
	}
	src, dst := filepath.Join(s.Live.Root.Dir, c), filepath.Join(s.Dir, c)
	if _, err := os.Lstat(src); err == nil {
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := copyTree(src, dst); err != nil {
			return fmt.Errorf("BACKUP_STAGE_FAILED: не удалось подготовить %s: %v", c, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	s.copied[c] = true
	return nil
}

// Take copies the live path into the stage (once) and marks it for the swap.
func (s *BackupStage) Take(rel string) error {
	if err := s.Copy(rel); err != nil {
		return err
	}
	c, _ := cleanRel(rel)
	s.swap[c] = true
	return nil
}

// TakeFields is Take for a JSON object file of which the backup owns only
// the given top-level fields: at swap time the staged file is rebuilt from
// the live one with just those fields taken from the stage, so whatever
// else the file holds (Keenetic receipts) is never replaced by a stale copy.
func (s *BackupStage) TakeFields(rel string, fields ...string) error {
	if len(fields) == 0 {
		return fmt.Errorf("BACKUP_PATH_INVALID: нет полей для %q", rel)
	}
	if err := s.Take(rel); err != nil {
		return err
	}
	c, _ := cleanRel(rel)
	s.fields[filepath.ToSlash(c)] = append([]string(nil), fields...)
	return nil
}

// Fields returns the field-merged paths (slash form) and their fields.
func (s *BackupStage) Fields() map[string][]string {
	out := make(map[string][]string, len(s.fields))
	for p, f := range s.fields {
		out[p] = append([]string(nil), f...)
	}
	return out
}

// Paths lists the paths marked for the swap (sorted, slash form).
func (s *BackupStage) Paths() []string {
	out := make([]string, 0, len(s.swap))
	for p := range s.swap {
		out = append(out, filepath.ToSlash(p))
	}
	sort.Strings(out)
	return out
}

// copyTree copies regular files and directories; anything else (symlinks,
// devices) is refused, so a staged tree can never point outside the root.
func copyTree(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode().IsRegular():
		return copyFile(src, dst)
	case st.IsDir():
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%s: не обычный файл", src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// CopyTree is copyTree for callers outside the package (pre-import copies).
func CopyTree(src, dst string) error { return copyTree(src, dst) }

// ---------- swap ----------

// SwapEntry records one swapped path of an import (manifest of the
// pre-import copy): Existed=false means the live path did not exist, so an
// undo removes it.
type SwapEntry struct {
	Path           string   `json:"path"`
	Existed        bool     `json:"existed"`
	Staged         bool     `json:"staged,omitempty"`         // staged path existed before the swap
	StageSHA256    string   `json:"stageSha256,omitempty"`    // digest of the staged path before the swap
	OriginalSHA256 string   `json:"originalSha256,omitempty"` // digest of the original live path before the swap
	Fields         []string `json:"fields,omitempty"`         // field-merged file (BackupStage.TakeFields)
	SHA256         string   `json:"sha256,omitempty"`         // BackupDigest of the live path right after the swap
}

func readObjectFile(p string) (map[string]any, bool, error) {
	st, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !st.Mode().IsRegular() {
		return nil, true, fmt.Errorf("%s: не обычный файл", p)
	}
	var m map[string]any
	if err := ReadJSON(p, &m); err != nil || m == nil {
		return nil, true, nil // present but not a JSON object
	}
	return m, true, nil
}

// MergeBackupFields rebuilds srcRoot/rel (the version about to be swapped
// in) as the live JSON object with only fields taken from the source
// version; a field missing there is removed. A live file that is missing or
// not a JSON object leaves the source version as it is.
func MergeBackupFields(liveRoot, srcRoot, rel string, fields []string) error {
	c, err := cleanRel(rel)
	if err != nil {
		return err
	}
	live, _, err := readObjectFile(filepath.Join(liveRoot, c))
	if err != nil {
		return err
	}
	if live == nil {
		return nil
	}
	srcPath := filepath.Join(srcRoot, c)
	src, srcExists, err := readObjectFile(srcPath)
	if err != nil {
		return err
	}
	if srcExists && src == nil {
		return fmt.Errorf("BACKUP_STAGE_FAILED: %s повреждён", rel)
	}
	for _, f := range fields {
		if v, ok := src[f]; ok {
			live[f] = v
		} else {
			delete(live, f)
		}
	}
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o700); err != nil {
		return err
	}
	return WriteJSON(srcPath, live)
}

// BackupDigest is the SHA-256 of a path below root as the import left it:
// file content, a directory tree (names, kinds, contents), "absent" for a
// missing path; for a field-merged file only the given fields count.
func BackupDigest(root, rel string, fields []string) (string, error) {
	c, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, c)
	if len(fields) > 0 {
		m, exists, err := readObjectFile(p)
		if err != nil {
			return "", err
		}
		if !exists {
			return SHA256Hex([]byte("absent")), nil
		}
		proj := map[string]any{}
		for _, f := range fields {
			if v, ok := m[f]; ok {
				proj[f] = v
			}
		}
		return CanonicalSHA256(map[string]any{"object": m != nil, "fields": proj})
	}
	h := sha256.New()
	var walk func(p, name string) error
	walk = func(p, name string) error {
		st, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case st.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "f\x00%s\x00%s\n", name, SHA256Hex(b))
		case st.IsDir():
			fmt.Fprintf(h, "d\x00%s\n", name)
			entries, err := os.ReadDir(p)
			if err != nil {
				return err
			}
			for _, e := range entries { // sorted by name
				if err := walk(filepath.Join(p, e.Name()), name+"/"+e.Name()); err != nil {
					return err
				}
			}
		default:
			fmt.Fprintf(h, "o\x00%s\x00%s\n", name, st.Mode().Type())
		}
		return nil
	}
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		return SHA256Hex([]byte("absent")), nil
	}
	if err := walk(p, "."); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// UndoBackupExchange reverses a verified renameat2 exchange that was
// interrupted before the original path could be moved into the copy.
func UndoBackupExchange(stage, live string) error {
	if err := exchange(stage, live); err != nil {
		return err
	}
	if err := platform.SyncDir(filepath.Dir(stage)); err != nil {
		return err
	}
	if filepath.Dir(stage) != filepath.Dir(live) {
		return platform.SyncDir(filepath.Dir(live))
	}
	return nil
}

// RenameBackupNoReplace moves a backup path only if its destination is still
// absent. On unsupported kernels or filesystems it fails closed, so a late
// foreign destination can never be overwritten by a fallback os.Rename.
// The caller must sync affected parent directories after a successful move.
func RenameBackupNoReplace(src, dst string) error {
	return renameBackupNoReplace(src, dst)
}

type backupSwapHooks struct {
	exchange func(string, string) error
	syncDir  func(string) error
	lstat    func(string) (os.FileInfo, error)
}

func (h backupSwapHooks) pathExists(path string) (bool, error) {
	st, err := h.lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.IsDir() && !st.Mode().IsRegular() {
		return false, fmt.Errorf("BACKUP_PATH_INVALID: %s не обычный файл или каталог", path)
	}
	return true, nil
}

func unsupportedBackupExchange(err error) bool {
	return errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EOPNOTSUPP)
}

func (h backupSwapHooks) syncParents(src, dst string, srcFD, dstFD *os.File) error {
	srcParent, dstParent := filepath.Dir(src), filepath.Dir(dst)
	if err := srcFD.Sync(); err != nil {
		return fmt.Errorf("backup directory sync %s: %w", srcParent, err)
	}
	if dstParent != srcParent {
		if err := dstFD.Sync(); err != nil {
			return fmt.Errorf("backup directory sync %s: %w", dstParent, err)
		}
	}
	if h.syncDir != nil {
		if err := h.syncDir(srcParent); err != nil {
			return fmt.Errorf("backup directory sync %s: %w", srcParent, err)
		}
		if dstParent != srcParent {
			if err := h.syncDir(dstParent); err != nil {
				return fmt.Errorf("backup directory sync %s: %w", dstParent, err)
			}
		}
	}
	if err := backupSwapParentUnchanged(srcParent, srcFD); err != nil {
		return err
	}
	return backupSwapParentUnchanged(dstParent, dstFD)
}

func backupSwapParentUnchanged(path string, held *os.File) error {
	current, err := platform.OpenDirNoFollow(path)
	if err != nil {
		return err
	}
	defer current.Close()
	before, err := held.Stat()
	if err != nil {
		return err
	}
	after, err := current.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return fmt.Errorf("BACKUP_PARENT_CHANGED: %s", path)
	}
	return nil
}

// SwapBackupPaths replaces each live path (below liveRoot) with its staged
// version (below stageRoot); the replaced original moves to the same path
// below preRoot. A path missing in the stage means "remove the live path"
// (used by undo). Directories are exchanged atomically where the kernel
// supports renameat2(RENAME_EXCHANGE), else by two renames. Each metadata
// phase syncs both parent directories before the next phase. The caller must
// persist a recovery plan before invoking this function: any failure after a
// metadata phase may leave a partial swap, including a later path failure.
// Only the owner of that plan may validate and recover the affected paths.
func SwapBackupPaths(liveRoot, stageRoot, preRoot string, paths []string) ([]SwapEntry, error) {
	return swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, paths, backupSwapHooks{})
}

func swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot string, paths []string, hooks backupSwapHooks) ([]SwapEntry, error) {
	if hooks.lstat == nil {
		hooks.lstat = os.Lstat
	}
	type swapPath struct {
		rel, live, stage, pre     string
		liveExists, stageExists   bool
		liveDir, stageDir, preDir *os.File
	}
	var selected []swapPath
	defer func() {
		for _, p := range selected {
			_ = p.liveDir.Close()
			_ = p.stageDir.Close()
			_ = p.preDir.Close()
		}
	}()
	checkParents := func(p swapPath) error {
		for _, parent := range []struct {
			path string
			held *os.File
		}{
			{filepath.Dir(p.live), p.liveDir},
			{filepath.Dir(p.stage), p.stageDir},
			{filepath.Dir(p.pre), p.preDir},
		} {
			if err := backupSwapParentUnchanged(parent.path, parent.held); err != nil {
				return err
			}
		}
		return nil
	}
	seen := make([]string, 0, len(paths))
	// Resolve every path and destination before the first live-path rename.
	for _, rel := range paths {
		c, err := cleanRel(rel)
		if err != nil {
			return nil, err
		}
		for _, prior := range seen {
			if c == prior || strings.HasPrefix(c, prior+string(filepath.Separator)) || strings.HasPrefix(prior, c+string(filepath.Separator)) {
				return nil, fmt.Errorf("BACKUP_PATH_INVALID: пересекающиеся пути %q и %q", prior, c)
			}
		}
		seen = append(seen, c)
		live, stage, pre := filepath.Join(liveRoot, c), filepath.Join(stageRoot, c), filepath.Join(preRoot, c)
		if err := platform.MkdirAllDurable(filepath.Dir(pre), 0o700); err != nil {
			return nil, err
		}
		if err := platform.MkdirAllDurable(filepath.Dir(live), 0o700); err != nil {
			return nil, err
		}
		if err := platform.MkdirAllDurable(filepath.Dir(stage), 0o700); err != nil {
			return nil, err
		}
		liveDir, err := platform.OpenDirNoFollow(filepath.Dir(live))
		if err != nil {
			return nil, err
		}
		stageDir, err := platform.OpenDirNoFollow(filepath.Dir(stage))
		if err != nil {
			_ = liveDir.Close()
			return nil, err
		}
		preDir, err := platform.OpenDirNoFollow(filepath.Dir(pre))
		if err != nil {
			_ = liveDir.Close()
			_ = stageDir.Close()
			return nil, err
		}
		selected = append(selected, swapPath{rel: c, live: live, stage: stage, pre: pre, liveDir: liveDir, stageDir: stageDir, preDir: preDir})
		if occupied, err := hooks.pathExists(pre); err != nil {
			return nil, err
		} else if occupied {
			return nil, fmt.Errorf("BACKUP_PATH_OCCUPIED: копия %s уже существует", pre)
		}
		liveExists, err := hooks.pathExists(live)
		if err != nil {
			return nil, err
		}
		stageExists, err := hooks.pathExists(stage)
		if err != nil {
			return nil, err
		}
		selected[len(selected)-1].liveExists = liveExists
		selected[len(selected)-1].stageExists = stageExists
		if err := checkParents(selected[len(selected)-1]); err != nil {
			return nil, err
		}
	}
	var done []SwapEntry
	for _, p := range selected {
		// Revalidate immediately before use: another actor may have changed a
		// later path or parent since preflight. The durable plan handles any
		// earlier paths that were already swapped.
		if err := checkParents(p); err != nil {
			return nil, err
		}
		if occupied, err := hooks.pathExists(p.pre); err != nil {
			return nil, err
		} else if occupied {
			return nil, fmt.Errorf("BACKUP_PATH_OCCUPIED: копия %s уже существует", p.pre)
		}
		liveExists, err := hooks.pathExists(p.live)
		if err != nil {
			return nil, err
		}
		stageExists, err := hooks.pathExists(p.stage)
		if err != nil {
			return nil, err
		}
		if liveExists != p.liveExists || stageExists != p.stageExists {
			return nil, fmt.Errorf("BACKUP_PATH_CHANGED: %s изменился после проверки", p.rel)
		}
		if err := checkParents(p); err != nil {
			return nil, err
		}
		if !liveExists && !stageExists {
			continue
		}
		switch {
		case liveExists && stageExists:
			var exchangeErr error
			if hooks.exchange != nil {
				exchangeErr = hooks.exchange(p.stage, p.live)
			} else {
				exchangeErr = exchangeBackupAt(p.stageDir, filepath.Base(p.stage), p.liveDir, filepath.Base(p.live))
			}
			if exchangeErr == nil {
				// live now holds the staged version, stage the original
				if err := hooks.syncParents(p.stage, p.live, p.stageDir, p.liveDir); err != nil {
					return nil, err // the durable plan drives recovery
				}
				if err := checkParents(p); err != nil {
					return nil, err
				}
				if err := renameBackupNoReplaceAt(p.stageDir, filepath.Base(p.stage), p.preDir, filepath.Base(p.pre)); err != nil {
					return nil, err
				}
				if err := hooks.syncParents(p.stage, p.pre, p.stageDir, p.preDir); err != nil {
					return nil, err
				}
			} else {
				if !unsupportedBackupExchange(exchangeErr) {
					return nil, exchangeErr
				}
				if err := checkParents(p); err != nil {
					return nil, err
				}
				if err := renameBackupNoReplaceAt(p.liveDir, filepath.Base(p.live), p.preDir, filepath.Base(p.pre)); err != nil {
					return nil, err
				}
				if err := hooks.syncParents(p.live, p.pre, p.liveDir, p.preDir); err != nil {
					return nil, err
				}
				if err := checkParents(p); err != nil {
					return nil, err
				}
				if err := renameBackupNoReplaceAt(p.stageDir, filepath.Base(p.stage), p.liveDir, filepath.Base(p.live)); err != nil {
					return nil, err
				}
				if err := hooks.syncParents(p.stage, p.live, p.stageDir, p.liveDir); err != nil {
					return nil, err
				}
			}
		case liveExists:
			if err := renameBackupNoReplaceAt(p.liveDir, filepath.Base(p.live), p.preDir, filepath.Base(p.pre)); err != nil {
				return nil, err
			}
			if err := hooks.syncParents(p.live, p.pre, p.liveDir, p.preDir); err != nil {
				return nil, err
			}
		default:
			if err := renameBackupNoReplaceAt(p.stageDir, filepath.Base(p.stage), p.liveDir, filepath.Base(p.live)); err != nil {
				return nil, err
			}
			if err := hooks.syncParents(p.stage, p.live, p.stageDir, p.liveDir); err != nil {
				return nil, err
			}
		}
		if err := checkParents(p); err != nil {
			return nil, err
		}
		done = append(done, SwapEntry{Path: filepath.ToSlash(p.rel), Existed: liveExists})
	}
	return done, nil
}
