package compat

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/BROadmin/broray/internal/platform"
)

func daemonPolicyHolder(h *Holder) bool {
	if h.Scope != "routes" || h.BundleID != "policy" || h.Resumable || h.DaemonOwnedXray || h.DaemonOwnedReserve {
		return false
	}
	return h.Action == "routes:policy" || h.Action == "routes:geodata"
}

// ReconcileDaemonPolicy runs before any area starts. Unlike the Xray worker
// handoff, policy recovery must finish while its old fence is still retained.
// The callback must not call GlobalLock methods; it holds kernel exclusions
// and restores the journal's exact files before this function releases ownership.
func (g *GlobalLock) ReconcileDaemonPolicy(root platform.Root, guard *platform.Lock, pending bool, recoverPolicy func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !guard.Owns(root.DaemonLock()) || filepath.Clean(g.file) != filepath.Join(root.Run(), "global-operation.json") {
		return errors.New("policy daemon ownership not confirmed")
	}
	g.daemonOwned = true
	st, data, err := readGlobalLockRecord(g.file)
	if errors.Is(err, os.ErrNotExist) {
		if g.ambiguous || g.cur != nil {
			return errors.New("global owner disappeared before policy recovery")
		}
		if !pending {
			return nil
		}
		if recoverPolicy == nil || g.externalBusyLocked() {
			return errors.New("policy recovery blocked by external owner")
		}
		if err := recoverPolicy(); err != nil {
			return err
		}
		if !guard.Owns(root.DaemonLock()) {
			return errors.New("daemon owner changed during policy recovery")
		}
		if _, _, err := readGlobalLockRecord(g.file); !errors.Is(err, os.ErrNotExist) {
			return errors.New("global owner appeared during policy recovery")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if g.cur != nil {
		if err := g.ownerMatchesLocked(); err != nil {
			return err
		}
	} else if !g.ambiguous {
		return errors.New("unexpected global owner record")
	}
	var h Holder
	if json.Unmarshal(data, &h) != nil || h.Scope == "" || h.Action == "" || h.StartedAt.IsZero() || h.UpdatedAt.IsZero() || (!h.Pending && h.Resumable) {
		return errors.New("invalid policy recovery owner")
	}
	if !h.DaemonOwnedPolicy || !daemonPolicyHolder(&h) {
		if !pending && h.Pending && !g.ambiguous {
			return nil
		}
		return errors.New("global operation does not belong to policy recovery")
	}
	if recoverPolicy == nil || g.externalBusyLocked() {
		return errors.New("policy recovery unavailable or external owner active")
	}
	if err := recoverPolicy(); err != nil {
		return err
	}
	if !guard.Owns(root.DaemonLock()) {
		return errors.New("daemon owner changed during policy recovery")
	}
	g.cur = &h
	g.ownerInfo = st
	g.ownerHash = sha256.Sum256(data)
	g.ambiguous = false
	if !g.releaseLocked() {
		return errors.New("cannot release recovered policy fence")
	}
	return platform.SyncDir(root.Run())
}
