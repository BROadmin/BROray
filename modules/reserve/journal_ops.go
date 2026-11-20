package reserve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

func OpenTransactionJournal(root platform.Root) (*TransactionJournal, error) {
	j := &TransactionJournal{root: root, dirs: map[string]*os.File{}}
	for _, path := range []string{root.Config(), transactionMetadataDir(root), filepath.Join(transactionMetadataDir(root), "system")} {
		dir, err := platform.OpenDirNoFollow(path)
		if err != nil {
			_ = j.Close()
			return nil, err
		}
		j.dirs[path] = dir
	}
	return j, nil
}
func (j *TransactionJournal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	var err error
	for _, dir := range j.dirs {
		err = errors.Join(err, dir.Close())
	}
	j.dirs = nil
	return err
}

// TransactionPending reads only the bounded journal entry. It also works on
// legacy installations without a reserve preferences directory. Bad files
// fail closed; this check does not authorize recovery or clear any ownership.
func TransactionPending(ctx context.Context, root platform.Root) (bool, error) {
	selection, profile, err := PendingTransactions(ctx, root)
	return selection || profile, err
}

// PendingTransactions distinguishes selection and profile journals using the
// same bounded nofollow snapshot. Presence does not authorize recovery; callers
// must reject mixed journals and retain the matching operation owner.
func PendingTransactions(ctx context.Context, root platform.Root) (selection, profilePending bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	dir, err := platform.OpenDirNoFollow(root.Config())
	if err != nil {
		return false, false, err
	}
	defer dir.Close()
	image, err := readTransactionImage(dir, TransactionJournalFile, maxTransactionJournalBytes)
	if err != nil {
		return false, false, err
	}
	profile, err := readTransactionImage(dir, ProfileTransactionJournalFile, maxProfileJournalBytes)
	if err != nil {
		return false, false, err
	}
	visible, err := platform.OpenDirNoFollow(root.Config())
	if err != nil {
		return false, false, err
	}
	defer visible.Close()
	a, ea := dir.Stat()
	b, eb := visible.Stat()
	if ea != nil || eb != nil || !os.SameFile(a, b) {
		return false, false, ErrTransactionUnconfirmed
	}
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	return image.Exists, profile.Exists, nil
}

func (j *TransactionJournal) Begin(ctx context.Context, next TransactionFiles, intent TransactionIntent) (string, error) {
	var id string
	err := j.locked(ctx, func() error {
		owner, err := readTransactionImage(j.dirs[j.root.Config()], TransactionJournalFile, maxTransactionJournalBytes)
		if err != nil {
			return err
		}
		if owner.Exists {
			return ErrTransactionUnconfirmed
		}
		profile, err := readTransactionImage(j.dirs[j.root.Config()], ProfileTransactionJournalFile, maxProfileJournalBytes)
		if err != nil {
			return err
		}
		if profile.Exists {
			return ErrTransactionUnconfirmed
		}
		previous, err := j.snapshot()
		if err != nil {
			return err
		}
		receipt, err := readTransactionImage(j.dirs[transactionMetadataDir(j.root)], "interface.json", maxTransactionReceiptBytes)
		if err != nil {
			return err
		}
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		r := TransactionRecord{SchemaVersion: 1, ID: hex.EncodeToString(token[:]), Phase: "prepared", Intent: intent, Receipt: receipt, Previous: previous,
			Next: TransactionState{Config: transactionImage(next.Config), Preferences: transactionImage(next.Preferences), Active: transactionImage(next.Active)}}
		if err := r.validate(); err != nil {
			return err
		}
		id = r.ID
		if _, err := j.persist(r, owner); err != nil {
			return err
		}
		after, err := readTransactionImage(j.dirs[transactionMetadataDir(j.root)], "interface.json", maxTransactionReceiptBytes)
		if err != nil {
			return err
		}
		if !after.equal(receipt) {
			return ErrTransactionUnconfirmed
		}
		return j.current(r.Previous)
	})
	return id, err
}
func (j *TransactionJournal) Read(ctx context.Context) (TransactionRecord, error) {
	var r TransactionRecord
	err := j.locked(ctx, func() error { var err error; r, _, err = j.load(); return err })
	return r, err
}
func (j *TransactionJournal) InstallSelection(ctx context.Context, id string) error {
	return j.locked(ctx, func() error {
		r, owner, err := j.loadID(id)
		if err != nil {
			return err
		}
		if r.Phase != "prepared" {
			return ErrTransactionUnconfirmed
		}
		if err := j.allowed(r); err != nil {
			return err
		}
		current, err := j.snapshot()
		if err != nil {
			return err
		}
		if !current.Config.equal(r.Next.Config) {
			return ErrTransactionUnconfirmed
		}
		if err := j.selection(ctx, r.Next, r.Previous, owner); err != nil {
			return err
		}
		return j.current(r.Next)
	})
}
func (j *TransactionJournal) Confirm(ctx context.Context, id string, verify func(context.Context, TransactionRecord) error) error {
	return j.locked(ctx, func() error {
		r, owner, err := j.loadID(id)
		if err != nil {
			return err
		}
		if r.Phase != "prepared" || verify == nil {
			return ErrTransactionUnconfirmed
		}
		if err := j.current(r.Next); err != nil {
			return err
		}
		if err := verify(ctx, cloneTransactionRecord(r)); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.sameOwner(owner); err != nil {
			return err
		}
		if err := j.current(r.Next); err != nil {
			return err
		}
		r.Phase = "committed"
		committed, err := j.persist(r, owner)
		if err != nil {
			return err
		}
		return j.clear(committed)
	})
}
func (j *TransactionJournal) Rollback(ctx context.Context, id string, restore func(context.Context, TransactionRecord) error) error {
	return j.rollback(ctx, id, nil, restore)
}

// RollbackPrepared is for a live operation retaining an exact prepared record.
// Unlike cold recovery, it must not clean a later committed decision or accept
// a missing journal without invoking the old runtime verifier.
func (j *TransactionJournal) RollbackPrepared(ctx context.Context, expected TransactionRecord, restore func(context.Context, TransactionRecord) error) error {
	expected = cloneTransactionRecord(expected)
	return j.rollback(ctx, expected.ID, &expected, restore)
}

func (j *TransactionJournal) rollback(ctx context.Context, id string, expected *TransactionRecord, restore func(context.Context, TransactionRecord) error) error {
	return j.locked(ctx, func() error {
		r, owner, err := j.loadID(id)
		if errors.Is(err, os.ErrNotExist) && expected == nil {
			return nil
		}
		if err != nil {
			return err
		}
		if expected != nil && (r.Phase != "prepared" || !reflect.DeepEqual(r, *expected)) {
			return ErrTransactionUnconfirmed
		}
		if r.Phase == "committed" {
			// The durable commit point already followed live proof. A crash
			// during cleanup must not undo an accepted switching decision.
			if err := j.current(r.Next); err != nil {
				return err
			}
			return j.clear(owner)
		}
		if restore == nil {
			return ErrTransactionUnconfirmed
		}
		if err := j.allowed(r); err != nil {
			return err
		}
		if err := j.selection(ctx, r.Previous, r.Next, owner); err != nil {
			return err
		}
		if err := restore(ctx, cloneTransactionRecord(r)); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.sameOwner(owner); err != nil {
			return err
		}
		if err := j.current(r.Previous); err != nil {
			return err
		}
		return j.clear(owner)
	})
}

func (j *TransactionJournal) checkRoots() error {
	if j.recoveryCheck != nil {
		if err := j.recoveryCheck(); err != nil {
			return err
		}
	}
	if len(j.dirs) != 3 {
		return ErrTransactionUnconfirmed
	}
	for path, held := range j.dirs {
		visible, err := platform.OpenDirNoFollow(path)
		if err != nil {
			return err
		}
		a, ea := held.Stat()
		b, eb := visible.Stat()
		_ = visible.Close()
		if ea != nil || eb != nil || !os.SameFile(a, b) {
			return ErrTransactionUnconfirmed
		}
	}
	return nil
}
func (j *TransactionJournal) locked(ctx context.Context, action func() error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := j.checkRoots(); err != nil {
		return err
	}
	fd := int(j.dirs[j.root.Config()].Fd())
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return action()
}
func (j *TransactionJournal) snapshot() (TransactionState, error) {
	if err := j.checkRoots(); err != nil {
		return TransactionState{}, err
	}
	config, err := readTransactionImage(j.dirs[j.root.Config()], "config.json", maxTransactionConfigBytes)
	if err != nil {
		return TransactionState{}, err
	}
	preferences, err := readTransactionImage(j.dirs[filepath.Join(transactionMetadataDir(j.root), "system")], "server-reserve.json", 4096)
	if err != nil {
		return TransactionState{}, err
	}
	active, err := readTransactionImage(j.dirs[transactionMetadataDir(j.root)], "active-server", 4096)
	if err != nil {
		return TransactionState{}, err
	}
	return TransactionState{config, preferences, active}, j.checkRoots()
}
func statesEqual(a, b TransactionState) bool {
	return a.Config.equal(b.Config) && a.Preferences.equal(b.Preferences) && a.Active.equal(b.Active)
}
func (j *TransactionJournal) current(want TransactionState) error {
	got, err := j.snapshot()
	if err != nil {
		return err
	}
	if !statesEqual(got, want) {
		return ErrTransactionUnconfirmed
	}
	return nil
}
func (j *TransactionJournal) allowed(r TransactionRecord) error {
	got, err := j.snapshot()
	if err != nil {
		return err
	}
	for _, pair := range [][3]TransactionImage{{got.Config, r.Previous.Config, r.Next.Config}, {got.Preferences, r.Previous.Preferences, r.Next.Preferences}, {got.Active, r.Previous.Active, r.Next.Active}} {
		if !pair[0].equal(pair[1]) && !pair[0].equal(pair[2]) {
			return ErrTransactionUnconfirmed
		}
	}
	return nil
}
func (j *TransactionJournal) selection(ctx context.Context, want, other TransactionState, owner TransactionImage) error {
	for _, item := range []struct {
		parent, name string
		image, other TransactionImage
	}{{filepath.Join(transactionMetadataDir(j.root), "system"), "server-reserve.json", want.Preferences, other.Preferences}, {transactionMetadataDir(j.root), "active-server", want.Active, other.Active}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.sameOwner(owner); err != nil {
			return err
		}
		before, err := readTransactionImage(j.dirs[item.parent], item.name, 4096)
		if err != nil {
			return err
		}
		if !before.equal(item.image) && !before.equal(item.other) {
			return ErrTransactionUnconfirmed
		}
		if err := replaceTransactionImage(j.dirs[item.parent], item.name, item.image, before, 4096); err != nil {
			return err
		}
		after, err := readTransactionImage(j.dirs[item.parent], item.name, 4096)
		if err != nil {
			return err
		}
		if !after.equal(item.image) {
			return ErrTransactionUnconfirmed
		}
	}
	return j.sameOwner(owner)
}
func (j *TransactionJournal) load() (TransactionRecord, TransactionImage, error) {
	image, err := readTransactionImage(j.dirs[j.root.Config()], TransactionJournalFile, maxTransactionJournalBytes)
	if err != nil {
		return TransactionRecord{}, image, err
	}
	if !image.Exists {
		return TransactionRecord{}, image, os.ErrNotExist
	}
	r, err := decodeTransactionRecord(image.Data)
	return r, image, err
}
func (j *TransactionJournal) loadID(id string) (TransactionRecord, TransactionImage, error) {
	r, image, err := j.load()
	if err != nil {
		return r, image, err
	}
	if r.ID != id {
		return r, image, ErrTransactionUnconfirmed
	}
	return r, image, nil
}
func (j *TransactionJournal) sameOwner(owner TransactionImage) error {
	if err := j.checkRoots(); err != nil {
		return err
	}
	current, err := readTransactionImage(j.dirs[j.root.Config()], TransactionJournalFile, maxTransactionJournalBytes)
	if err != nil {
		return err
	}
	if !current.equal(owner) {
		return ErrTransactionUnconfirmed
	}
	return nil
}
func (j *TransactionJournal) persist(r TransactionRecord, previous TransactionImage) (TransactionImage, error) {
	if err := r.validate(); err != nil {
		return TransactionImage{}, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return TransactionImage{}, err
	}
	next := transactionImage(data)
	if err := j.checkRoots(); err != nil {
		return next, err
	}
	if err := replaceTransactionImage(j.dirs[j.root.Config()], TransactionJournalFile, next, previous, maxTransactionJournalBytes); err != nil {
		return next, err
	}
	return next, j.sameOwner(next)
}
func (j *TransactionJournal) clear(owner TransactionImage) error {
	if err := j.checkRoots(); err != nil {
		return err
	}
	if err := replaceTransactionImage(j.dirs[j.root.Config()], TransactionJournalFile, TransactionImage{}, owner, maxTransactionJournalBytes); err != nil {
		return err
	}
	after, err := readTransactionImage(j.dirs[j.root.Config()], TransactionJournalFile, maxTransactionJournalBytes)
	if err != nil {
		return err
	}
	if after.Exists {
		return ErrTransactionUnconfirmed
	}
	return j.checkRoots()
}
func cloneTransactionRecord(r TransactionRecord) TransactionRecord {
	clone := func(f TransactionImage) TransactionImage { f.Data = append([]byte(nil), f.Data...); return f }
	r.Receipt = clone(r.Receipt)
	r.Previous = TransactionState{clone(r.Previous.Config), clone(r.Previous.Preferences), clone(r.Previous.Active)}
	r.Next = TransactionState{clone(r.Next.Config), clone(r.Next.Preferences), clone(r.Next.Active)}
	return r
}

// Compatibility-area selection and ownership records live under data/config.
// The core's generated config and this journal live under the root config dir.
func transactionMetadataDir(root platform.Root) string {
	return filepath.Join(root.Data(), "config")
}
