package reserve

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/BROadmin/broray/internal/platform"
)

const ProfileBootHoldFile = "server-dpi-boot-hold.v1.json"
const profileBootLockFile = "server-dpi-boot.lock"
const maxProfileBootHoldBytes = 4096

var ErrProfileBootHeld = errors.New("DPI_RECOVERED_PROFILE_STOPPED")

type profileBootHold struct {
	SchemaVersion int    `json:"schemaVersion"`
	Operation     string `json:"operation"`
	ServerID      string `json:"serverId"`
	RecordSHA256  string `json:"recordSHA256"`
	ConfigSHA256  string `json:"configSHA256"`
}

func decodeProfileBootHold(raw []byte) (profileBootHold, error) {
	var h profileBootHold
	if len(raw) > maxProfileBootHoldBytes || !utf8.Valid(raw) {
		return h, ErrTransactionUnconfirmed
	}
	if _, err := transactionFields(raw, "schemaVersion", "operation", "serverId", "recordSHA256", "configSHA256"); err != nil {
		return h, err
	}
	if json.Unmarshal(raw, &h) != nil || h.SchemaVersion != 1 || !profileServerID.MatchString(h.ServerID) || !transactionHashPattern.MatchString(h.Operation) || !transactionHashPattern.MatchString(h.RecordSHA256) || !transactionHashPattern.MatchString(h.ConfigSHA256) {
		return h, ErrTransactionUnconfirmed
	}
	return h, nil
}

func checkProfileBootHold(parent *os.File, image TransactionImage, held *os.File) error {
	if err := sameTransactionEntry(parent, ProfileBootHoldFile, held); err != nil {
		return err
	}
	current, err := readTransactionImage(parent, ProfileBootHoldFile, maxProfileBootHoldBytes)
	if err != nil {
		return err
	}
	if !current.equal(image) {
		return ErrTransactionUnconfirmed
	}
	return nil
}

// A prior stopped decision is immutable evidence, not authority to adopt any
// syntactically valid marker. Only guarded recovery may recognize its own
// interrupted marker, and only when no prior hold was captured.
func profileBootContext(r ProfileTransactionRecord, image TransactionImage, recovery bool) error {
	if !image.valid(maxProfileBootHoldBytes) {
		return ErrTransactionUnconfirmed
	}
	if image.Exists {
		if _, err := decodeProfileBootHold(image.Data); err != nil {
			return err
		}
	}
	active := r.Context.Active.Exists && strings.TrimSpace(string(r.Context.Active.Data)) == r.ServerID
	if !active {
		return nil
	}
	if r.WasRunning {
		if image.Exists {
			return ErrTransactionUnconfirmed
		}
		return nil
	}
	prior := TransactionImage{}
	if r.Context.BootHold != nil {
		prior = *r.Context.BootHold
	}
	if image.equal(prior) {
		return nil
	}
	if recovery && !prior.Exists {
		want := r.Previous
		if r.Phase == "committed" {
			want = r.Next
		}
		raw, err := json.Marshal(profileBootHold{1, r.ID, r.ServerID, want.Record.SHA256, want.Config.SHA256})
		if err == nil && image.equal(transactionImage(raw)) {
			return nil
		}
	}
	return ErrTransactionUnconfirmed
}

// Called only inside the journal's retained-parent, kernel-excluded recovery.
// Repeating interrupted recovery must find exactly the same durable decision.
func (j *ProfileJournal) preserveBootIntent(r ProfileTransactionRecord, want ProfileImages, journalOwner TransactionImage, journalFile *os.File) (TransactionImage, *os.File, error) {
	if err := j.sameOwner(journalOwner, journalFile); err != nil {
		return TransactionImage{}, nil, err
	}
	parent := j.base.dirs[j.base.root.Config()]
	image, held, err := openTransactionImage(parent, ProfileBootHoldFile, maxProfileBootHoldBytes)
	if err != nil {
		return image, held, err
	}
	if image.Exists {
		if _, err = decodeProfileBootHold(image.Data); err != nil {
			return image, held, err
		}
	}
	active := r.Context.Active.Exists && strings.TrimSpace(string(r.Context.Active.Data)) == r.ServerID
	if !active {
		return image, held, nil
	}
	if r.WasRunning {
		if image.Exists {
			return image, held, ErrTransactionUnconfirmed
		}
		return image, held, nil
	}
	if prior := r.Context.BootHold; prior != nil && prior.Exists {
		if !image.equal(*prior) {
			return image, held, ErrTransactionUnconfirmed
		}
		return image, held, nil
	}
	h := profileBootHold{1, r.ID, r.ServerID, want.Record.SHA256, want.Config.SHA256}
	raw, err := json.Marshal(h)
	if err != nil {
		return image, held, err
	}
	if _, err = decodeProfileBootHold(raw); err != nil {
		return image, held, err
	}
	next := transactionImage(raw)
	if image.Exists {
		if !image.equal(next) {
			return image, held, ErrTransactionUnconfirmed
		}
		return image, held, nil
	}
	if err = j.sameOwner(journalOwner, journalFile); err != nil {
		return image, held, err
	}
	if err = j.currentWithBoot(r, &want, true); err != nil {
		return image, held, err
	}
	if err = replaceTransactionImage(parent, ProfileBootHoldFile, next, image, maxProfileBootHoldBytes); err != nil {
		return image, held, err
	}
	image, held, err = openTransactionImage(parent, ProfileBootHoldFile, maxProfileBootHoldBytes)
	if err == nil && !image.equal(next) {
		err = ErrTransactionUnconfirmed
	}
	return image, held, err
}

// WithProfileStart serializes a launch decision against durable stopped intent.
// An automatic launch never consumes a hold. An intentional launch supersedes
// the saved stop before its callback, including when the launch later fails:
// the supervisor's existing semantics retain the requested running intent.
// The callback must not reenter this helper or the profile journal.
func WithProfileStart(ctx context.Context, root platform.Root, automatic bool, start func() error) error {
	if start == nil {
		return ErrTransactionUnconfirmed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := platform.OpenDirNoFollow(root.Config())
	if os.IsNotExist(err) {
		return start()
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	// Ordinary selection transactions may verify a concurrent new generation
	// while holding the config-directory flock. Only profile recovery shares
	// this intent exclusion; do not block unrelated launches on that flock.
	bootLock, err := reserveRecoveryGuard(parent, profileBootLockFile)
	if err != nil {
		return err
	}
	defer bootLock.Close()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		visible, err := platform.OpenDirNoFollow(root.Config())
		if err != nil {
			return err
		}
		defer visible.Close()
		a, ea := parent.Stat()
		b, eb := visible.Stat()
		if ea != nil || eb != nil || !os.SameFile(a, b) {
			return ErrTransactionUnconfirmed
		}
		return sameTransactionEntry(parent, profileBootLockFile, bootLock)
	}
	if err = check(); err != nil {
		return err
	}
	image, held, err := openTransactionImage(parent, ProfileBootHoldFile, maxProfileBootHoldBytes)
	if held != nil {
		defer held.Close()
	}
	if err != nil {
		return err
	}
	if image.Exists {
		if _, err = decodeProfileBootHold(image.Data); err != nil {
			return err
		}
		if automatic {
			return ErrProfileBootHeld
		}
		if err = check(); err != nil {
			return err
		}
		if err = checkProfileBootHold(parent, image, held); err != nil {
			return err
		}
		if err = replaceTransactionImage(parent, ProfileBootHoldFile, TransactionImage{}, image, maxProfileBootHoldBytes); err != nil {
			return err
		}
		image = TransactionImage{}
		held = nil
	}
	if err = check(); err != nil {
		return err
	}
	if err = checkProfileBootHold(parent, image, held); err != nil {
		return err
	}
	return start()
}
