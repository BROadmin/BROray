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
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

const ProfileTransactionJournalFile = "server-dpi-pending.v1.json"
const maxProfileJournalBytes = 12 << 20
const maxProfileRecordBytes = 1 << 20

var profileServerID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,250}$`)

type ProfileImages struct {
	Record TransactionImage `json:"record"`
	Config TransactionImage `json:"config"`
}
type ProfileContext struct {
	Active      TransactionImage `json:"active"`
	Preferences TransactionImage `json:"preferences"`
	Settings    TransactionImage `json:"settings"`
	Receipt     TransactionImage `json:"receipt"`
	// Nil identifies a legacy journal; new journals capture even an absent hold.
	BootHold *TransactionImage `json:"bootHold,omitempty"`
}
type ProfileTransactionRecord struct {
	SchemaVersion int            `json:"schemaVersion"`
	ID            string         `json:"id"`
	Phase         string         `json:"phase"`
	ServerID      string         `json:"serverId"`
	Authority     string         `json:"authority"`
	WasRunning    bool           `json:"wasRunning"`
	Context       ProfileContext `json:"context"`
	Previous      ProfileImages  `json:"previous"`
	Next          ProfileImages  `json:"next"`
}

// The caller owns the application fence and validates full server/core/runtime
// semantics. This journal changes only one existing live profile and config;
// active selection, settings, roster and router receipt are immutable context.
// Install and Restore must run under the supervisor's stopped-child guard for
// an active profile. They never invoke a process or router operation themselves.
type ProfileJournal struct {
	base    *TransactionJournal
	records *os.File
}

func OpenProfileJournal(root platform.Root) (*ProfileJournal, error) {
	base, err := OpenTransactionJournal(root)
	if err != nil {
		return nil, err
	}
	records, err := platform.OpenDirNoFollow(filepath.Join(root.Data(), "servers"))
	if err != nil {
		base.Close()
		return nil, err
	}
	j := &ProfileJournal{base: base, records: records}
	base.recoveryCheck = func() error {
		visible, e := platform.OpenDirNoFollow(filepath.Join(root.Data(), "servers"))
		if e != nil {
			return e
		}
		defer visible.Close()
		a, ea := records.Stat()
		b, eb := visible.Stat()
		if ea != nil || eb != nil || !os.SameFile(a, b) {
			return ErrTransactionUnconfirmed
		}
		return nil
	}
	return j, nil
}
func (j *ProfileJournal) Close() error { return errors.Join(j.base.Close(), j.records.Close()) }
func cloneProfileRecord(r ProfileTransactionRecord) ProfileTransactionRecord {
	clone := func(i TransactionImage) TransactionImage { i.Data = append([]byte(nil), i.Data...); return i }
	r.Context = ProfileContext{Active: clone(r.Context.Active), Preferences: clone(r.Context.Preferences), Settings: clone(r.Context.Settings), Receipt: clone(r.Context.Receipt), BootHold: r.Context.BootHold}
	if r.Context.BootHold != nil {
		hold := clone(*r.Context.BootHold)
		r.Context.BootHold = &hold
	}
	r.Previous = ProfileImages{clone(r.Previous.Record), clone(r.Previous.Config)}
	r.Next = ProfileImages{clone(r.Next.Record), clone(r.Next.Config)}
	return r
}
func (r ProfileTransactionRecord) validate() error {
	if r.SchemaVersion != 1 || !transactionHashPattern.MatchString(r.ID) || !transactionHashPattern.MatchString(r.Authority) || !profileServerID.MatchString(r.ServerID) || (r.Phase != "prepared" && r.Phase != "committed") {
		return ErrTransactionUnconfirmed
	}
	for _, state := range []ProfileImages{r.Previous, r.Next} {
		if !state.Record.Exists || !state.Record.valid(maxProfileRecordBytes) || !state.Config.valid(maxTransactionConfigBytes) {
			return ErrTransactionUnconfirmed
		}
		value, err := strictjson.Decode(state.Record.Data)
		m, ok := value.(map[string]any)
		if err != nil || !ok || m["id"] != r.ServerID {
			return ErrTransactionUnconfirmed
		}
		if state.Config.Exists && !transactionObject(state.Config.Data) {
			return ErrTransactionUnconfirmed
		}
	}
	for _, item := range []struct {
		image TransactionImage
		limit int
	}{{r.Context.Active, 4096}, {r.Context.Preferences, 4096}, {r.Context.Settings, 1 << 20}, {r.Context.Receipt, maxTransactionReceiptBytes}} {
		if !item.image.valid(item.limit) {
			return ErrTransactionUnconfirmed
		}
	}
	for _, image := range []TransactionImage{r.Context.Preferences, r.Context.Settings, r.Context.Receipt} {
		if image.Exists && !transactionObject(image.Data) {
			return ErrTransactionUnconfirmed
		}
	}
	active := ""
	if r.Context.Active.Exists {
		if !utf8.Valid(r.Context.Active.Data) {
			return ErrTransactionUnconfirmed
		}
		active = strings.TrimSpace(string(r.Context.Active.Data))
		if !profileServerID.MatchString(active) {
			return ErrTransactionUnconfirmed
		}
	}
	if hold := r.Context.BootHold; hold != nil {
		if !hold.valid(maxProfileBootHoldBytes) {
			return ErrTransactionUnconfirmed
		}
		if hold.Exists {
			if _, err := decodeProfileBootHold(hold.Data); err != nil {
				return err
			}
			if active == r.ServerID && r.WasRunning {
				return ErrTransactionUnconfirmed
			}
		}
	}
	if active == r.ServerID {
		if !r.Previous.Config.Exists || !r.Next.Config.Exists {
			return ErrTransactionUnconfirmed
		}
	} else if r.WasRunning || !r.Previous.Config.equal(r.Next.Config) {
		return ErrTransactionUnconfirmed
	}
	return nil
}
func decodeProfileRecord(raw []byte) (ProfileTransactionRecord, error) {
	var r ProfileTransactionRecord
	if len(raw) > maxProfileJournalBytes || !utf8.Valid(raw) {
		return r, ErrTransactionUnconfirmed
	}
	fields, err := transactionFields(raw, "schemaVersion", "id", "phase", "serverId", "authority", "wasRunning", "context", "previous", "next")
	if err != nil {
		return r, err
	}
	flag := strings.TrimSpace(string(fields["wasRunning"]))
	if flag != "true" && flag != "false" {
		return r, ErrTransactionUnconfirmed
	}
	for _, key := range []string{"id", "phase", "serverId", "authority"} {
		var s string
		if string(fields[key]) == "null" || json.Unmarshal(fields[key], &s) != nil {
			return r, ErrTransactionUnconfirmed
		}
	}
	for _, key := range []string{"previous", "next", "context"} {
		names := []string{"record", "config"}
		if key == "context" {
			names = []string{"active", "preferences", "settings", "receipt"}
		}
		images, e := transactionFields(fields[key], names...)
		if e != nil && key == "context" {
			images, e = transactionFields(fields[key], append(names, "bootHold")...)
		}
		if e != nil {
			return r, e
		}
		for _, image := range images {
			if e = transactionImageFields(image); e != nil {
				return r, e
			}
		}
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	return r, r.validate()
}
func (j *ProfileJournal) snapshot(server string) (ProfileImages, ProfileContext, error) {
	if !profileServerID.MatchString(server) {
		return ProfileImages{}, ProfileContext{}, ErrTransactionUnconfirmed
	}
	state, err := j.base.snapshot()
	if err != nil {
		return ProfileImages{}, ProfileContext{}, err
	}
	record, err := readTransactionImage(j.records, server+".json", maxProfileRecordBytes)
	if err != nil {
		return ProfileImages{}, ProfileContext{}, err
	}
	metadata := transactionMetadataDir(j.base.root)
	settings, err := readTransactionImage(j.base.dirs[filepath.Join(metadata, "system")], "settings.json", 1<<20)
	if err != nil {
		return ProfileImages{}, ProfileContext{}, err
	}
	receipt, err := readTransactionImage(j.base.dirs[metadata], "interface.json", maxTransactionReceiptBytes)
	if err != nil {
		return ProfileImages{}, ProfileContext{}, err
	}
	hold, err := readTransactionImage(j.base.dirs[j.base.root.Config()], ProfileBootHoldFile, maxProfileBootHoldBytes)
	if err != nil {
		return ProfileImages{}, ProfileContext{}, err
	}
	return ProfileImages{record, state.Config}, ProfileContext{Active: state.Active, Preferences: state.Preferences, Settings: settings, Receipt: receipt, BootHold: &hold}, j.base.checkRoots()
}
func profileContextEqual(a, b ProfileContext) bool {
	return a.Active.equal(b.Active) && a.Preferences.equal(b.Preferences) && a.Settings.equal(b.Settings) && a.Receipt.equal(b.Receipt)
}
func profileImagesEqual(a, b ProfileImages) bool {
	return a.Record.equal(b.Record) && a.Config.equal(b.Config)
}
func (j *ProfileJournal) current(r ProfileTransactionRecord, want *ProfileImages) error {
	return j.currentWithBoot(r, want, false)
}
func (j *ProfileJournal) currentWithBoot(r ProfileTransactionRecord, want *ProfileImages, recovery bool) error {
	ordinary, err := readTransactionImage(j.base.dirs[j.base.root.Config()], TransactionJournalFile, maxTransactionJournalBytes)
	if err != nil {
		return err
	}
	if ordinary.Exists {
		return ErrTransactionUnconfirmed
	}
	state, context, err := j.snapshot(r.ServerID)
	if err != nil {
		return err
	}
	if !profileContextEqual(context, r.Context) {
		return ErrTransactionUnconfirmed
	}
	if err := profileBootContext(r, *context.BootHold, recovery); err != nil {
		return err
	}
	if want != nil {
		if !profileImagesEqual(state, *want) {
			return ErrTransactionUnconfirmed
		}
		return nil
	}
	for _, images := range [][3]TransactionImage{{state.Record, r.Previous.Record, r.Next.Record}, {state.Config, r.Previous.Config, r.Next.Config}} {
		if !images[0].equal(images[1]) && !images[0].equal(images[2]) {
			return ErrTransactionUnconfirmed
		}
	}
	return nil
}
func (j *ProfileJournal) load() (ProfileTransactionRecord, TransactionImage, *os.File, error) {
	image, held, err := openTransactionImage(j.base.dirs[j.base.root.Config()], ProfileTransactionJournalFile, maxProfileJournalBytes)
	if err != nil {
		return ProfileTransactionRecord{}, image, held, err
	}
	if !image.Exists {
		return ProfileTransactionRecord{}, image, held, os.ErrNotExist
	}
	r, err := decodeProfileRecord(image.Data)
	return r, image, held, err
}
func (j *ProfileJournal) sameOwner(image TransactionImage, held *os.File) error {
	if err := j.base.checkRoots(); err != nil {
		return err
	}
	parent := j.base.dirs[j.base.root.Config()]
	if err := sameTransactionEntry(parent, ProfileTransactionJournalFile, held); err != nil {
		return err
	}
	current, err := readTransactionImage(parent, ProfileTransactionJournalFile, maxProfileJournalBytes)
	if err != nil {
		return err
	}
	if !current.equal(image) {
		return ErrTransactionUnconfirmed
	}
	return nil
}
func (j *ProfileJournal) persist(r ProfileTransactionRecord, previous TransactionImage, held *os.File) (TransactionImage, *os.File, error) {
	if err := r.validate(); err != nil {
		return TransactionImage{}, nil, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return TransactionImage{}, nil, err
	}
	next := transactionImage(data)
	if err = j.sameOwner(previous, held); err != nil {
		return next, nil, err
	}
	if err = replaceTransactionImage(j.base.dirs[j.base.root.Config()], ProfileTransactionJournalFile, next, previous, maxProfileJournalBytes); err != nil {
		return next, nil, err
	}
	got, file, err := openTransactionImage(j.base.dirs[j.base.root.Config()], ProfileTransactionJournalFile, maxProfileJournalBytes)
	if err == nil && !got.equal(next) {
		err = ErrTransactionUnconfirmed
	}
	if err == nil {
		err = j.sameOwner(next, file)
	}
	return next, file, err
}
func (j *ProfileJournal) clear(image TransactionImage, held *os.File) error {
	if err := j.sameOwner(image, held); err != nil {
		return err
	}
	if err := replaceTransactionImage(j.base.dirs[j.base.root.Config()], ProfileTransactionJournalFile, TransactionImage{}, image, maxProfileJournalBytes); err != nil {
		return err
	}
	return j.sameOwner(TransactionImage{}, nil)
}
func (j *ProfileJournal) Begin(ctx context.Context, server, expectedSHA string, nextRecord, nextConfig []byte, wasRunning bool, authority string) (ProfileTransactionRecord, error) {
	var r ProfileTransactionRecord
	err := j.base.locked(ctx, func() error {
		owner, held, e := openTransactionImage(j.base.dirs[j.base.root.Config()], ProfileTransactionJournalFile, maxProfileJournalBytes)
		if held != nil {
			defer held.Close()
		}
		if e != nil {
			return e
		}
		if owner.Exists {
			return ErrTransactionUnconfirmed
		}
		reserveOwner, e := readTransactionImage(j.base.dirs[j.base.root.Config()], TransactionJournalFile, maxTransactionJournalBytes)
		if e != nil {
			return e
		}
		if reserveOwner.Exists {
			return ErrTransactionUnconfirmed
		}
		previous, context, e := j.snapshot(server)
		if e != nil {
			return e
		}
		if !previous.Record.Exists || previous.Record.SHA256 != expectedSHA {
			return ErrTransactionUnconfirmed
		}
		var nonce [32]byte
		if _, e = rand.Read(nonce[:]); e != nil {
			return e
		}
		r = ProfileTransactionRecord{SchemaVersion: 1, ID: hex.EncodeToString(nonce[:]), Phase: "prepared", ServerID: server, Authority: authority, WasRunning: wasRunning, Context: context, Previous: previous, Next: ProfileImages{transactionImage(nextRecord), transactionImage(nextConfig)}}
		if e = r.validate(); e != nil {
			return e
		}
		_, file, e := j.persist(r, owner, held)
		if file != nil {
			defer file.Close()
		}
		if e != nil {
			return e
		}
		return j.current(r, &r.Previous)
	})
	return cloneProfileRecord(r), err
}
func (j *ProfileJournal) Read(ctx context.Context) (ProfileTransactionRecord, error) {
	var r ProfileTransactionRecord
	err := j.base.locked(ctx, func() error {
		var e error
		var held *os.File
		r, _, held, e = j.load()
		if held != nil {
			defer held.Close()
		}
		return e
	})
	return cloneProfileRecord(r), err
}
func (j *ProfileJournal) prepared(expected ProfileTransactionRecord) (ProfileTransactionRecord, TransactionImage, *os.File, error) {
	r, owner, held, err := j.load()
	if err == nil && (r.Phase != "prepared" || !reflect.DeepEqual(r, expected)) {
		err = ErrTransactionUnconfirmed
	}
	return r, owner, held, err
}

// Install and Restore perform only local CAS under retained directories. Partial
// record/config pairs are recoverable; a foreign image is never overwritten.
func (j *ProfileJournal) Install(ctx context.Context, expected ProfileTransactionRecord) error {
	return j.replace(ctx, expected, false)
}
func (j *ProfileJournal) Restore(ctx context.Context, expected ProfileTransactionRecord) error {
	return j.replace(ctx, expected, true)
}
func (j *ProfileJournal) replace(ctx context.Context, expected ProfileTransactionRecord, restore bool) error {
	expected = cloneProfileRecord(expected)
	return j.base.locked(ctx, func() error {
		r, owner, held, err := j.prepared(expected)
		if held != nil {
			defer held.Close()
		}
		if err != nil {
			return err
		}
		if restore {
			err = j.current(r, nil)
		} else {
			err = j.current(r, &r.Previous)
		}
		if err != nil {
			return err
		}
		want := r.Next
		if restore {
			want = r.Previous
		}
		for _, item := range []struct {
			parent                *os.File
			name                  string
			image, previous, next TransactionImage
			limit                 int
		}{{j.records, r.ServerID + ".json", want.Record, r.Previous.Record, r.Next.Record, maxProfileRecordBytes}, {j.base.dirs[j.base.root.Config()], "config.json", want.Config, r.Previous.Config, r.Next.Config, maxTransactionConfigBytes}} {
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = j.sameOwner(owner, held); err != nil {
				return err
			}
			if err = j.current(r, nil); err != nil {
				return err
			}
			if e := replaceProfileImage(item.parent, item.name, item.image, item.previous, item.next, item.limit); e != nil {
				return e
			}
		}
		if err = j.sameOwner(owner, held); err != nil {
			return err
		}
		return j.current(r, &want)
	})
}
func (j *ProfileJournal) Confirm(ctx context.Context, expected ProfileTransactionRecord, verify func(context.Context, ProfileTransactionRecord) error) error {
	return j.finish(ctx, expected, verify, true)
}
func (j *ProfileJournal) RollbackPrepared(ctx context.Context, expected ProfileTransactionRecord, verify func(context.Context, ProfileTransactionRecord) error) error {
	return j.finish(ctx, expected, verify, false)
}
func (j *ProfileJournal) finish(ctx context.Context, expected ProfileTransactionRecord, verify func(context.Context, ProfileTransactionRecord) error, commit bool) error {
	expected = cloneProfileRecord(expected)
	return j.base.locked(ctx, func() error {
		r, owner, held, err := j.prepared(expected)
		if held != nil {
			defer held.Close()
		}
		if err != nil {
			return err
		}
		if verify == nil || stoppedActiveProfile(r) {
			return ErrTransactionUnconfirmed
		}
		want := r.Previous
		if commit {
			want = r.Next
		}
		if err = j.current(r, &want); err != nil {
			return err
		}
		if err = verify(ctx, cloneProfileRecord(r)); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = j.sameOwner(owner, held); err != nil {
			return err
		}
		if err = j.current(r, &want); err != nil {
			return err
		}
		if commit {
			r.Phase = "committed"
			next, file, e := j.persist(r, owner, held)
			if file != nil {
				defer file.Close()
			}
			if e != nil {
				return e
			}
			return j.clear(next, file)
		}
		return j.clear(owner, held)
	})
}

// CleanupCommitted is only for guarded recovery of a durable accepted decision.
// It cannot be used as a live prepared rollback or clear a partial next image.
func (j *ProfileJournal) CleanupCommitted(ctx context.Context) error {
	return j.base.locked(ctx, func() error {
		r, owner, held, err := j.load()
		if held != nil {
			defer held.Close()
		}
		if err != nil {
			return err
		}
		if r.Phase != "committed" || stoppedActiveProfile(r) {
			return ErrTransactionUnconfirmed
		}
		if err = j.current(r, &r.Next); err != nil {
			return err
		}
		return j.clear(owner, held)
	})
}

// This final boundary must retain the journal's allowed images through CAS.
func replaceProfileImage(parent *os.File, name string, want, previous, next TransactionImage, limit int) error {
	before, err := readTransactionImage(parent, name, limit)
	if err != nil {
		return err
	}
	if !previous.valid(limit) || !next.valid(limit) || !want.valid(limit) ||
		(!want.equal(previous) && !want.equal(next)) || (!before.equal(previous) && !before.equal(next)) {
		return ErrTransactionUnconfirmed
	}
	return replaceTransactionImage(parent, name, want, before, limit)
}
