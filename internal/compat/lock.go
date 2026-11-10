package compat

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// GlobalLock is the 3.x global operation fence
// (/opt/var/lock/broray/global-operation.lock): at most one conflicting
// mutation (routes, custom routes, DoT, server activation, subscription
// apply, Xray install, Keenetic writes) runs at a time. A routes operation
// that stopped resumably keeps the fence as "pending" until resumed or
// abandoned, exactly like 3.x.
//
// The holder is persisted to a JSON file. An active record found after a
// restart has uncertain ownership and blocks startup until investigated.
// Detached launcher and installer processes can outlive the daemon and keep
// their kernel locks; these also block mutations. A pending holder is restored.
type GlobalLock struct {
	mu          sync.Mutex
	cur         *Holder
	file        string
	ambiguous   bool          // unreadable or invalid ownership record; never clear as pending
	daemonOwned bool          // enabled only while holding this root's daemon kernel lock
	free        chan struct{} // closed and replaced on every release
	external    []string      // kernel locks held by a detached updater/installer
	ownerInfo   os.FileInfo
	ownerHash   [sha256.Size]byte
}

// Holder describes the current owner (3.x globalOperation fields).
type Holder struct {
	Scope           string    `json:"scope"`  // routes | custom | dot | servers | subscriptions | xray | keenetic | system
	Action          string    `json:"action"` // e.g. "export", "dot:apply"
	BundleID        string    `json:"bundleId,omitempty"`
	Pending         bool      `json:"pending"`   // stopped, waiting for resume
	Resumable       bool      `json:"resumable"` // pending and resumable
	StartedAt       time.Time `json:"startedAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	DaemonOwnedXray bool      `json:"daemonOwnedXray,omitempty"`
	token           uint64
}

var lockSeq uint64

const maxGlobalLockRecord = 64 << 10

func readGlobalLockRecord(path string) (os.FileInfo, []byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxGlobalLockRecord {
		return nil, nil, errors.New("global lock record is not a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(st, opened) {
		return nil, nil, errors.New("global lock record changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxGlobalLockRecord+1))
	if err != nil || len(data) > maxGlobalLockRecord || int64(len(data)) != st.Size() {
		return nil, nil, errors.New("global lock record changed while reading")
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(st, current) || current.Size() != st.Size() || !current.ModTime().Equal(st.ModTime()) {
		return nil, nil, errors.New("global lock record changed after reading")
	}
	return st, data, nil
}

func NewGlobalLock(file string, external ...string) *GlobalLock {
	g := &GlobalLock{file: file, free: make(chan struct{}), external: append([]string(nil), external...)}
	st, data, err := readGlobalLockRecord(file)
	if errors.Is(err, os.ErrNotExist) {
		return g
	}
	if err != nil {
		g.ambiguous = true
		return g
	}
	var h Holder
	if json.Unmarshal(data, &h) != nil ||
		h.Scope == "" || h.Action == "" || h.StartedAt.IsZero() || h.UpdatedAt.IsZero() ||
		(!h.Pending && h.Resumable) {
		g.ambiguous = true
		return g
	}
	if h.Pending {
		h.token = 0
		g.cur = &h
		g.ownerInfo, g.ownerHash = st, sha256.Sum256(data)
		return g
	}
	// A complete active record may belong to a process that outlived the
	// daemon. Its age is not proof of stale ownership.
	g.ambiguous = true
	return g
}

// Release is returned by Acquire; calling it more than once is harmless.
type Release func()

// TryAcquire takes the fence or returns ok=false if anyone holds it
// (including a pending holder).
func (g *GlobalLock) TryAcquire(scope, action, bundleID string) (Release, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ambiguous || g.cur != nil || g.externalBusyLocked() {
		return nil, false
	}
	r, err := g.takeLocked(scope, action, bundleID)
	return r, err == nil
}

// Acquire waits until the fence is free or ctx ends.
func (g *GlobalLock) Acquire(ctx context.Context, scope, action, bundleID string) (Release, error) {
	for {
		g.mu.Lock()
		if g.ambiguous {
			g.mu.Unlock()
			return nil, errors.New("global lock ownership is ambiguous")
		}
		if g.cur == nil && !g.externalBusyLocked() {
			r, err := g.takeLocked(scope, action, bundleID)
			g.mu.Unlock()
			return r, err
		}
		ch := g.free
		g.mu.Unlock()
		// A detached process releases its kernel lock without touching g.free.
		// Poll that lock while retaining the usual release notification.
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-ch:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// externalBusyLocked treats both a held kernel lock and an uncertain probe
// as busy. An I/O error must not permit a conflicting Keenetic mutation.
func (g *GlobalLock) externalBusyLocked() bool {
	for _, path := range g.external {
		st, err := os.Lstat(path)
		if err == nil && !st.Mode().IsRegular() {
			return true // never follow a symlink or accept another file type
		}
		if err != nil && !os.IsNotExist(err) {
			return true
		}
		lock, err := platform.TryLock(path)
		if err != nil {
			return true
		}
		if err := lock.Release(); err != nil {
			return true
		}
	}
	return false
}

func (g *GlobalLock) takeLocked(scope, action, bundleID string) (Release, error) {
	lockSeq++
	tok := lockSeq
	now := time.Now()
	g.cur = &Holder{Scope: scope, Action: action, BundleID: bundleID, StartedAt: now, UpdatedAt: now, token: tok}
	g.cur.DaemonOwnedXray = g.daemonOwned && daemonXrayHolder(g.cur)
	if err := g.persistLocked(); err != nil {
		g.ambiguous = true
		return nil, fmt.Errorf("persist global lock owner: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.cur != nil && g.cur.token == tok && !g.cur.Pending {
				g.releaseLocked()
			}
		})
	}, nil
}

func (g *GlobalLock) ownerMatchesLocked() error {
	if g.ownerInfo == nil {
		return errors.New("global lock owner record was never persisted")
	}
	st, data, err := readGlobalLockRecord(g.file)
	if err != nil {
		return err
	}
	if !os.SameFile(g.ownerInfo, st) || sha256.Sum256(data) != g.ownerHash {
		return errors.New("global lock owner record changed")
	}
	return nil
}

func (g *GlobalLock) releaseLocked() bool {
	if g.ambiguous || g.ownerMatchesLocked() != nil {
		g.ambiguous = true
		return false
	}
	if err := os.Remove(g.file); err != nil {
		g.ambiguous = true
		return false
	}
	if _, err := os.Lstat(g.file); !errors.Is(err, os.ErrNotExist) {
		g.ambiguous = true
		return false
	}
	g.cur = nil
	g.ownerInfo = nil
	g.ownerHash = [sha256.Size]byte{}
	close(g.free)
	g.free = make(chan struct{})
	return true
}

// MarkPending converts the current holder (taken by the caller) into a
// pending fence that survives Release and daemon restarts. Used by the
// routes engine when an operation stops resumably.
func (g *GlobalLock) MarkPending(resumable bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cur == nil || g.ambiguous {
		return errors.New("global lock owner is unavailable or ambiguous")
	}
	g.cur.Pending, g.cur.Resumable, g.cur.UpdatedAt = true, resumable, time.Now()
	if err := g.persistLocked(); err != nil {
		g.ambiguous = true
		return fmt.Errorf("persist pending global lock: %w", err)
	}
	return nil
}

// TakePending lets the owner of a pending fence (same scope and bundle)
// continue it: the fence becomes active again under a new release func.
func (g *GlobalLock) TakePending(scope, bundleID, action string) (Release, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ambiguous || g.cur == nil || !g.cur.Pending || g.cur.Scope != scope || g.cur.BundleID != bundleID {
		return nil, false
	}
	prior := g.cur
	g.cur = nil
	r, err := g.takeLocked(scope, action, bundleID)
	if err != nil {
		g.cur = prior
		return nil, false
	}
	return r, true
}

// ClearPending drops a pending fence (abandon / recovery).
func (g *GlobalLock) ClearPending() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ambiguous || g.cur == nil || !g.cur.Pending {
		return false
	}
	return g.releaseLocked()
}

// Touch updates UpdatedAt of the active holder.
func (g *GlobalLock) Touch() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cur != nil {
		g.cur.UpdatedAt = time.Now()
	}
}

// Current returns a copy of the holder.
func (g *GlobalLock) Current() (Holder, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cur == nil {
		return Holder{}, false
	}
	return *g.cur, true
}

// Ambiguous reports an ownership record that cannot be trusted or cleared
// through the normal pending-operation API.
func (g *GlobalLock) Ambiguous() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ambiguous
}

// Busy reports in-process, ambiguous and detached updater owners.
func (g *GlobalLock) Busy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ambiguous || g.cur != nil || g.externalBusyLocked()
}

// JSON is the 3.x `globalOperation` object (broray_routes_api_lock_read_json).
func (g *GlobalLock) JSON() map[string]any {
	if g.Ambiguous() {
		return map[string]any{"active": true, "pending": true, "resumable": false, "pid": nil,
			"scope": "system", "action": "ambiguous-record", "bundleId": nil,
			"startedAt": nil, "updatedAt": nil, "stale": true}
	}
	h, ok := g.Current()
	if !ok {
		return map[string]any{"active": false, "pending": false, "resumable": false, "pid": nil, "scope": nil, "action": nil,
			"bundleId": nil, "startedAt": nil, "updatedAt": nil, "stale": false}
	}
	var bundle any
	if h.BundleID != "" {
		bundle = h.BundleID
	}
	return map[string]any{"active": true, "pending": h.Pending, "resumable": h.Resumable, "pid": os.Getpid(),
		"scope": h.Scope, "action": h.Action, "bundleId": bundle,
		"startedAt": Stamp(h.StartedAt), "updatedAt": Stamp(h.UpdatedAt), "stale": false}
}

func (g *GlobalLock) persistLocked() error {
	if g.cur == nil {
		return errors.New("global lock has no owner")
	}
	if g.ownerInfo == nil {
		if _, err := os.Lstat(g.file); !errors.Is(err, os.ErrNotExist) {
			return errors.New("global lock record already exists or cannot be checked")
		}
	} else if err := g.ownerMatchesLocked(); err != nil {
		return err
	}
	want, err := Marshal(g.cur)
	if err != nil {
		return err
	}
	if err := WriteJSON(g.file, g.cur); err != nil {
		return err
	}
	st, got, err := readGlobalLockRecord(g.file)
	if err != nil {
		return err
	}
	if sha256.Sum256(got) != sha256.Sum256(want) {
		return errors.New("persisted global lock record differs from owner")
	}
	g.ownerInfo, g.ownerHash = st, sha256.Sum256(got)
	return nil
}

type fenceKey struct{}

// WithFence marks ctx as running under a global fence its caller already
// holds (the uninstall). Code called with it must not try to take d.Lock.
func WithFence(ctx context.Context) context.Context { return context.WithValue(ctx, fenceKey{}, true) }

// FenceHeld reports whether the caller already holds the global fence.
func FenceHeld(ctx context.Context) bool { v, _ := ctx.Value(fenceKey{}).(bool); return v }
