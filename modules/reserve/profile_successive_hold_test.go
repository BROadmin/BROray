package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func successiveHoldFixture(t *testing.T, prior bool) (profileFixture, []byte) {
	t.Helper()
	f := newProfileFixture(t, true)
	ctx := context.Background()
	if err := f.j.RollbackPrepared(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { return nil }); err != nil {
		t.Fatal(err)
	}
	hold := bootHoldBytes(t)
	if prior {
		transactionWrite(t, filepath.Join(f.f.root.Config(), ProfileBootHoldFile), hold)
	}
	var err error
	f.r, err = f.j.Begin(ctx, f.r.ServerID, transactionHash(f.old), f.next, f.r.Next.Config.Data, false, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return f, hold
}

func TestDPISuccessiveStoppedHold(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		t.Run(phase, func(t *testing.T) {
			f, hold := successiveHoldFixture(t, true)
			ctx := context.Background()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			if phase == "committed" {
				f.r.Phase = phase
				writeProfileRecoveryRecord(t, f, f.r)
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			calls := 0
			err = f.j.Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error { calls++; return nil })
			if err != nil || calls != 1 {
				t.Fatal("prior stopped hold rejected", err, calls)
			}
			f.assertState(t, phase == "committed")
			f.assertPending(t, false)
			after, err := os.ReadFile(filepath.Join(f.f.root.Config(), ProfileBootHoldFile))
			if err != nil || !bytes.Equal(hold, after) {
				t.Fatal("prior stopped decision changed", err)
			}
			if err = WithProfileStart(ctx, f.f.root, true, func() error { t.Fatal("unexpected boot"); return nil }); err != ErrProfileBootHeld {
				t.Fatal(err)
			}
		})
	}
}

func TestDPILateHoldCannotAuthorizeRestore(t *testing.T) {
	f, hold := successiveHoldFixture(t, false)
	ctx := context.Background()
	if err := f.j.Install(ctx, f.r); err != nil {
		t.Fatal(err)
	}
	transactionWrite(t, filepath.Join(f.f.root.Config(), ProfileBootHoldFile), hold)
	guard, err := platform.TryLock(f.f.root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	if err = f.j.Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error {
		t.Fatal("foreign hold reached verifier")
		return nil
	}); err == nil {
		t.Fatal("foreign hold accepted")
	}
	f.assertState(t, true)
	f.assertPending(t, true)
	after, err := os.ReadFile(filepath.Join(f.f.root.Config(), ProfileBootHoldFile))
	if err != nil || !bytes.Equal(hold, after) {
		t.Fatal("foreign hold changed", err)
	}
}

func TestDPICapturedHoldMutationBoundaries(t *testing.T) {
	for _, boundary := range []string{"install", "restore", "confirm", "rollback", "recover", "verify-bytes", "verify-inode", "verify-remove"} {
		t.Run(boundary, func(t *testing.T) {
			f, hold := successiveHoldFixture(t, true)
			ctx := context.Background()
			path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
			if boundary != "install" && boundary != "rollback" {
				if err := f.j.Install(ctx, f.r); err != nil {
					t.Fatal(err)
				}
			}
			foreign := bytes.Replace(hold, []byte(strings.Repeat("a", 64)), []byte(strings.Repeat("d", 64)), 1)
			if !strings.HasPrefix(boundary, "verify-") {
				transactionWrite(t, path, foreign)
			}
			calls := 0
			verify := func(context.Context, ProfileTransactionRecord) error { calls++; return nil }
			var err error
			switch boundary {
			case "install":
				err = f.j.Install(ctx, f.r)
			case "restore":
				err = f.j.Restore(ctx, f.r)
			case "confirm":
				err = f.j.Confirm(ctx, f.r, verify)
			case "rollback":
				err = f.j.RollbackPrepared(ctx, f.r, verify)
			default:
				guard, e := platform.TryLock(f.f.root.DaemonLock())
				if e != nil {
					t.Fatal(e)
				}
				defer guard.Release()
				err = f.j.Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error {
					calls++
					switch boundary {
					case "verify-bytes":
						return os.WriteFile(path, foreign, 0600)
					case "verify-inode":
						// Rename keeps the old inode alive and rules out inode number reuse.
						if e := os.Rename(path, path+".retained"); e != nil {
							return e
						}
						return os.WriteFile(path, hold, 0600)
					case "verify-remove":
						return os.Remove(path)
					}
					t.Fatal("changed hold reached verifier")
					return nil
				})
			}
			if err == nil {
				t.Fatal("changed hold accepted")
			}
			f.assertPending(t, true)
			if strings.HasPrefix(boundary, "verify-") {
				if calls != 1 {
					t.Fatal("verifier not reached", calls)
				}
				f.assertState(t, false)
			} else {
				if calls != 0 {
					t.Fatal("changed hold reached verifier", calls)
				}
				f.assertState(t, boundary != "install" && boundary != "rollback")
				after, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(after, foreign) {
					t.Fatal("foreign hold overwritten", e)
				}
			}
		})
	}
}

func TestDPICapturedHoldSchemaAndClone(t *testing.T) {
	f, hold := successiveHoldFixture(t, true)
	if f.r.Context.BootHold == nil || !bytes.Equal(f.r.Context.BootHold.Data, hold) {
		t.Fatal("prior hold missing")
	}
	raw, err := json.Marshal(f.r)
	if err != nil {
		t.Fatal(err)
	}
	clone := cloneProfileRecord(f.r)
	clone.Context.BootHold.Data[0] = ' '
	if !bytes.Equal(f.r.Context.BootHold.Data, hold) {
		t.Fatal("clone aliases hold bytes")
	}
	clone.Context.BootHold.Exists = false
	if !f.r.Context.BootHold.Exists {
		t.Fatal("clone aliases hold object")
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	var ctx map[string]json.RawMessage
	if err = json.Unmarshal(obj["context"], &ctx); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]json.RawMessage{
		"null": []byte(`null`), "missing": []byte(`{}`), "wrong-hash": []byte(`{"exists":true,"data":"e30=","sha256":"bad"}`),
		"malformed-hold": func() []byte { b, _ := json.Marshal(transactionImage([]byte(`{}`))); return b }(),
		"duplicate":      bytes.Replace(ctx["bootHold"], []byte(`"exists":true`), []byte(`"exists":true,"exists":true`), 1),
		"unknown":        bytes.Replace(ctx["bootHold"], []byte(`"exists":true`), []byte(`"extra":true,"exists":true`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			original := ctx["bootHold"]
			ctx["bootHold"] = bad
			obj["context"], _ = json.Marshal(ctx)
			invalid, _ := json.Marshal(obj)
			if _, e := decodeProfileRecord(invalid); e == nil {
				t.Fatal("invalid hold evidence accepted")
			}
			ctx["bootHold"] = original
		})
	}
	delete(ctx, "bootHold")
	obj["context"], _ = json.Marshal(ctx)
	legacy, _ := json.Marshal(obj)
	decoded, err := decodeProfileRecord(legacy)
	if err != nil || decoded.Context.BootHold != nil {
		t.Fatal("legacy decode", err)
	}
	// A schema-valid running record may not claim a captured stopped hold.
	contradiction := cloneProfileRecord(f.r)
	contradiction.WasRunning = true
	if contradiction.validate() == nil {
		t.Fatal("contradictory captured intent accepted")
	}
}

func TestDPILegacyHoldRecoveryAndRetry(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		t.Run(phase, func(t *testing.T) {
			f, _ := successiveHoldFixture(t, false)
			f.r.Context.BootHold = nil
			writeProfileRecoveryRecord(t, f, f.r)
			ctx := context.Background()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			if phase == "committed" {
				f.r.Phase = phase
				writeProfileRecoveryRecord(t, f, f.r)
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			sentinel := errors.New("native validation interrupted")
			if err = f.j.Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error { return sentinel }); !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			f.assertPending(t, true)
			path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.j.Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error { return nil }); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("retry lost own hold", err)
			}
			f.assertPending(t, false)
			f.assertState(t, phase == "committed")
		})
	}
}

func TestDPIPriorHoldRejectsRunningBegin(t *testing.T) {
	f := newProfileFixture(t, true)
	ctx := context.Background()
	if err := f.j.RollbackPrepared(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { return nil }); err != nil {
		t.Fatal(err)
	}
	hold := bootHoldBytes(t)
	path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
	transactionWrite(t, path, hold)
	if _, err := f.j.Begin(ctx, f.r.ServerID, transactionHash(f.old), f.next, f.r.Next.Config.Data, true, strings.Repeat("a", 64)); err == nil {
		t.Fatal("running intent adopted stopped hold")
	}
	f.assertPending(t, false)
	f.assertState(t, false)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, hold) {
		t.Fatal(err)
	}
}

func TestDPIExistingHoldCannotBeReadopted(t *testing.T) {
	for boundary := 1; boundary <= 16; boundary++ {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			f, hold := successiveHoldFixture(t, true)
			ctx := context.Background()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
			previousCheck := f.j.base.recoveryCheck
			calls := 0
			injected := false
			f.j.base.recoveryCheck = func() error {
				if err := previousCheck(); err != nil {
					return err
				}
				record, e := os.ReadFile(f.recordPath)
				if e != nil {
					return e
				}
				config, e := os.ReadFile(f.f.root.XrayConfig())
				if e != nil {
					return e
				}
				if bytes.Equal(record, f.r.Previous.Record.Data) && bytes.Equal(config, f.r.Previous.Config.Data) {
					calls++
					if calls == boundary {
						if e := os.Rename(path, path+".original"); e != nil {
							return e
						}
						if e := os.WriteFile(path, hold, 0600); e != nil {
							return e
						}
						injected = true
					}
				}
				return nil
			}
			err = f.j.Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error { return nil })
			if injected && err == nil {
				t.Fatal("recovery adopted replaced existing hold", boundary, calls)
			}
			if !injected && err != nil {
				t.Fatal("unmodified recovery failed", err)
			}
			after, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(after, hold) {
				t.Fatal("foreign held bytes changed", e)
			}
		})
	}
}
