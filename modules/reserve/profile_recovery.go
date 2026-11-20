package reserve

import (
	"context"
	"errors"
	"os"

	"github.com/BROadmin/broray/internal/platform"
)

// Recover restores a prepared profile or preserves a committed decision while
// the actual daemon, child, installer and launcher exclusions are held. The
// caller must also hold the application recovery fence. Before returning nil,
// verify must validate the recovered images. Stopped active-profile intent is
// persisted before verification. It must not start the main child or reenter this
// journal. This primitive alone does not authorize daemon startup.
func (j *ProfileJournal) Recover(ctx context.Context, guard *platform.Lock, verify func(context.Context, ProfileTransactionRecord) error) error {
	return j.withProfileExclusion(ctx, guard, func(proof *profileExclusion) error {
		root := j.base.root
		bootParent := j.base.dirs[root.Config()]
		r, owner, held, err := j.load()
		if held != nil {
			defer held.Close()
		}
		if errors.Is(err, os.ErrNotExist) {
			return j.base.checkRoots()
		}
		if err != nil {
			return err
		}
		if verify == nil {
			return ErrTransactionUnconfirmed
		}
		bootImage, bootFile, err := openTransactionImage(bootParent, ProfileBootHoldFile, maxProfileBootHoldBytes)
		if bootFile != nil {
			defer bootFile.Close()
		}
		if err != nil {
			return err
		}
		if err = profileBootContext(r, bootImage, true); err != nil {
			return err
		}
		// Reopening an equal image cannot replace the original inode proof.
		initialBootImage, initialBootFile := bootImage, bootFile
		proof.checkHold = func() error {
			if initialBootImage.Exists {
				if err := checkProfileBootHold(bootParent, initialBootImage, initialBootFile); err != nil {
					return err
				}
			}
			return checkProfileBootHold(bootParent, bootImage, bootFile)
		}
		want := r.Next
		if r.Phase == "prepared" {
			if err := j.currentWithBoot(r, nil, true); err != nil {
				return err
			}
			want = r.Previous
			for _, item := range []struct {
				parent                *os.File
				name                  string
				image, previous, next TransactionImage
				limit                 int
			}{{j.records, r.ServerID + ".json", want.Record, r.Previous.Record, r.Next.Record, maxProfileRecordBytes}, {j.base.dirs[root.Config()], "config.json", want.Config, r.Previous.Config, r.Next.Config, maxTransactionConfigBytes}} {
				if err := j.sameOwner(owner, held); err != nil {
					return err
				}
				if err := j.currentWithBoot(r, nil, true); err != nil {
					return err
				}
				if err := replaceProfileImage(item.parent, item.name, item.image, item.previous, item.next, item.limit); err != nil {
					return err
				}
			}
		}
		if err := j.sameOwner(owner, held); err != nil {
			return err
		}
		if err := j.currentWithBoot(r, &want, true); err != nil {
			return err
		}
		bootImage, bootFile, err = j.preserveBootIntent(r, want, owner, held)
		if bootFile != nil {
			defer bootFile.Close()
		}
		if err != nil {
			return err
		}
		if err := j.sameOwner(owner, held); err != nil {
			return err
		}
		if err := j.currentWithBoot(r, &want, true); err != nil {
			return err
		}
		if err := verify(ctx, cloneProfileRecord(r)); err != nil {
			return err
		}
		if err := j.sameOwner(owner, held); err != nil {
			return err
		}
		if err := j.currentWithBoot(r, &want, true); err != nil {
			return err
		}
		if err := checkProfileBootHold(j.base.dirs[root.Config()], bootImage, bootFile); err != nil {
			return err
		}
		return j.clear(owner, held)
	})
}
