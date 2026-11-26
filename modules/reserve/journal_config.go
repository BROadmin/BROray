package reserve

import (
	"context"
	"reflect"
)

// InstallConfig is suitable for the supervisor's stopped-child installation
// callback: only bounded local file operations, no runtime/service callbacks.
// Selection remains previous until the replacement has actually launched.
func (j *TransactionJournal) InstallConfig(ctx context.Context, id string) error {
	return j.locked(ctx, func() error {
		r, owner, err := j.loadID(id)
		if err != nil {
			return err
		}
		if r.Phase != "prepared" {
			return ErrTransactionUnconfirmed
		}
		if err := j.current(r.Previous); err != nil {
			return err
		}
		parent := j.dirs[j.root.Config()]
		receipt, err := readTransactionImage(j.dirs[transactionMetadataDir(j.root)], "interface.json", maxTransactionReceiptBytes)
		if err != nil {
			return err
		}
		if !receipt.equal(r.Receipt) {
			return ErrTransactionUnconfirmed
		}
		if err := j.sameOwner(owner); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := replaceTransactionImage(parent, "config.json", r.Next.Config, r.Previous.Config, maxTransactionConfigBytes); err != nil {
			return err
		}
		want := r.Previous
		want.Config = r.Next.Config
		if err := j.current(want); err != nil {
			return err
		}
		return j.sameOwner(owner)
	})
}

// RestoreConfig installs only the previous config image for a guarded live
// rollback. Call it inside the supervisor's stopped-child installation callback.
// It does not change selection, acknowledge runtime recovery or clear evidence:
// the caller must subsequently prove the restored runtime through Rollback.
// A committed decision and a changed interface receipt are never rolled back.
func (j *TransactionJournal) RestoreConfig(ctx context.Context, id string) error {
	return j.restoreConfig(ctx, id, nil, nil)
}

// RestoreConfigAfterMetadataProof is the enable rollback's config-only installer.
// The caller must first prove the exact current receipt against the owned router
// configuration and retain the successful replacement snapshot/fence. This
// method repeats only bounded local CAS checks, suitable under Process.mu.
// It never changes the historical receipt, selection or journal, nor proves
// runtime recovery. Ordinary RestoreConfig continues to require the old receipt.
func (j *TransactionJournal) RestoreConfigAfterMetadataProof(ctx context.Context, expected TransactionRecord, receipt []byte) error {
	expected = cloneTransactionRecord(expected)
	proof := append([]byte(nil), receipt...)
	return j.restoreConfig(ctx, expected.ID, &expected, proof)
}

func (j *TransactionJournal) restoreConfig(ctx context.Context, id string, expected *TransactionRecord, proof []byte) error {
	return j.locked(ctx, func() error {
		r, owner, err := j.loadID(id)
		if err != nil {
			return err
		}
		if r.Phase != "prepared" {
			return ErrTransactionUnconfirmed
		}
		wantReceipt := r.Receipt
		if expected != nil {
			if (r.Intent.Action != "enable" && r.Intent.Action != "exit-switch") || !reflect.DeepEqual(r, *expected) || len(proof) == 0 || len(proof) > maxTransactionReceiptBytes {
				return ErrTransactionUnconfirmed
			}
			wantReceipt = transactionImage(proof)
		}
		before, err := j.snapshot()
		if err != nil {
			return err
		}
		for _, pair := range [][3]TransactionImage{{before.Config, r.Previous.Config, r.Next.Config}, {before.Preferences, r.Previous.Preferences, r.Next.Preferences}, {before.Active, r.Previous.Active, r.Next.Active}} {
			if !pair[0].equal(pair[1]) && !pair[0].equal(pair[2]) {
				return ErrTransactionUnconfirmed
			}
		}
		receipt, err := readTransactionImage(j.dirs[transactionMetadataDir(j.root)], "interface.json", maxTransactionReceiptBytes)
		if err != nil {
			return err
		}
		if !receipt.equal(wantReceipt) {
			return ErrTransactionUnconfirmed
		}
		if err := j.sameOwner(owner); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.current(before); err != nil {
			return err
		}
		if err := replaceTransactionImage(j.dirs[j.root.Config()], "config.json", r.Previous.Config, before.Config, maxTransactionConfigBytes); err != nil {
			return err
		}
		want := before
		want.Config = r.Previous.Config
		if err := j.current(want); err != nil {
			return err
		}
		afterReceipt, err := readTransactionImage(j.dirs[transactionMetadataDir(j.root)], "interface.json", maxTransactionReceiptBytes)
		if err != nil {
			return err
		}
		if !afterReceipt.equal(wantReceipt) {
			return ErrTransactionUnconfirmed
		}
		return j.sameOwner(owner)
	})
}
