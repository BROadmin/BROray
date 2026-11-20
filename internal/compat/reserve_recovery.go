package compat

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

const ReserveModeAction = "servers:reserve-mode"
const ReserveSwitchAction = "servers:reserve-switch"
const DPIProfileAction = "servers:dpi-profile"

// Source removal shares the durable selection journal and its recovery owner.
const SourceRemovalAction = "subscriptions:remove-source"
const SourceRefreshAction = "subscriptions:refresh"
const ReserveOperationTarget = "reserve"

func daemonReserveHolder(h *Holder) bool {
	return h.Scope == "operations" && h.BundleID == ReserveOperationTarget &&
		!h.Resumable && !h.DaemonOwnedXray && !h.DaemonOwnedPolicy &&
		reserveQueueAction(h.Action)
}

// ReconcileDaemonReserve retains the old fence throughout recovery, before any
// area or child starts. The callback must not call GlobalLock methods.
func (g *GlobalLock) ReconcileDaemonReserve(root platform.Root, guard *platform.Lock, pending bool, recoverReserve func() error) error {
	return g.reconcileDaemonReserve(root, guard, pending, false, recoverReserve)
}

// ReconcileDaemonDPI uses the same retained owner protocol, but only a DPI
// operation may authorize a profile recovery. Its callback must not call
// GlobalLock methods, start the main child, or recover a selection journal.
func (g *GlobalLock) ReconcileDaemonDPI(root platform.Root, guard *platform.Lock, pending bool, recoverProfile func() error) error {
	return g.reconcileDaemonReserve(root, guard, pending, true, recoverProfile)
}

func (g *GlobalLock) reconcileDaemonReserve(root platform.Root, guard *platform.Lock, pending, profile bool, recoverReserve func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	fail := errors.New("reserve recovery ownership not confirmed")
	if !guard.Owns(root.DaemonLock()) || filepath.Clean(g.file) != filepath.Join(root.Run(), "global-operation.json") {
		return fail
	}
	if err := CheckSourceSyncWorkspaces(root); err != nil {
		return err
	}
	g.daemonOwned = true
	st, data, err := readGlobalLockRecord(g.file)
	if errors.Is(err, os.ErrNotExist) {
		if g.ambiguous || g.cur != nil {
			return fail
		}
		if !pending {
			return nil
		}
		if recoverReserve == nil || g.externalBusyLocked() {
			return fail
		}
		if err := recoverReserve(); err != nil {
			return err
		}
		if err := CheckSourceSyncWorkspaces(root); err != nil {
			return err
		}
		if !guard.Owns(root.DaemonLock()) || g.externalBusyLocked() {
			return fail
		}
		if _, _, err := readGlobalLockRecord(g.file); !errors.Is(err, os.ErrNotExist) {
			return fail
		}
		return nil
	}
	if err != nil {
		return err
	}
	var h Holder
	if json.Unmarshal(data, &h) != nil {
		return fail
	}
	// Other recovery classes retain their handling when no matching journal
	// exists. Profile and selection journals must never borrow each other's owner.
	if !pending && (!h.DaemonOwnedReserve || (h.Action == DPIProfileAction) != profile) {
		return nil
	}
	v, err := strictjson.Decode(data)
	fields, ok := v.(map[string]any)
	if err != nil || !ok {
		return fail
	}
	for key, value := range fields {
		if value == nil {
			return fail
		}
		switch key {
		case "scope", "action", "bundleId", "pending", "resumable", "startedAt", "updatedAt", "daemonOwnedXray", "daemonOwnedPolicy", "daemonOwnedReserve":
		default:
			return fail
		}
	}
	if !h.DaemonOwnedReserve || !daemonReserveHolder(&h) || (h.Action == DPIProfileAction) != profile || h.StartedAt.IsZero() || h.UpdatedAt.IsZero() {
		return fail
	}
	if g.cur != nil {
		if err := g.ownerMatchesLocked(); err != nil {
			return err
		}
	} else if !g.ambiguous {
		return fail
	}
	if recoverReserve == nil || g.externalBusyLocked() {
		return fail
	}
	if err := recoverReserve(); err != nil {
		return err
	}
	if err := CheckSourceSyncWorkspaces(root); err != nil {
		return err
	}
	if !guard.Owns(root.DaemonLock()) || g.externalBusyLocked() {
		return fail
	}
	g.cur, g.ownerInfo, g.ownerHash, g.ambiguous = &h, st, sha256.Sum256(data), false
	if !g.releaseLocked() {
		return fail
	}
	return platform.SyncDir(root.Run())
}
