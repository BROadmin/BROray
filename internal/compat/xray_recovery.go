package compat

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/BROadmin/broray/internal/platform"
	"path/filepath"
)

func daemonXrayHolder(h *Holder) bool {
	if h.Scope != "xray" || h.BundleID != "xray" || h.Pending || h.Resumable {
		return false
	}
	switch h.Action {
	case "xray:install", "xray:update", "xray:reinstall", "xray:bootstrap", "xray:uninstall":
		return true
	}
	return false
}

// ReconcileDaemonXray runs before area startup, after acquiring the daemon
// kernel lock. Only the new, explicitly in-process Xray worker contract can
// be reclaimed. Detached, legacy, malformed and foreign records stay fenced.
func (g *GlobalLock) ReconcileDaemonXray(root platform.Root, guard *platform.Lock) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !guard.Owns(root.DaemonLock()) || filepath.Clean(g.file) != filepath.Join(root.Run(), "global-operation.json") {
		return errors.New("daemon ownership not confirmed")
	}
	g.daemonOwned = true
	if !g.ambiguous {
		return nil
	}
	st, data, err := readGlobalLockRecord(g.file)
	if err != nil {
		return err
	}
	var h Holder
	if json.Unmarshal(data, &h) != nil || !h.DaemonOwnedXray || !daemonXrayHolder(&h) || h.StartedAt.IsZero() || h.UpdatedAt.IsZero() || g.externalBusyLocked() {
		return errors.New("global operation ownership remains uncertain")
	}
	g.cur = &h
	g.ownerInfo = st
	g.ownerHash = sha256.Sum256(data)
	g.ambiguous = false
	if !g.releaseLocked() {
		return errors.New("cannot release terminated Xray worker fence")
	}
	return platform.SyncDir(root.Run())
}
