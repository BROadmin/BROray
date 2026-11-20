package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDPIProfilePendingBlocksExistingGate(t *testing.T) {
	f := newTransactionFixture(t)
	path := filepath.Join(f.root.Config(), "server-dpi-pending.v1.json")
	raw := []byte(`{"unfinished":true}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	pending, err := TransactionPending(context.Background(), f.root)
	if err != nil || !pending {
		t.Fatal("DPI profile pending image ignored", pending, err)
	}
	if _, err = f.j.Begin(context.Background(), f.next, f.intent); err == nil {
		t.Fatal("reserve transaction borrowed profile owner")
	}
}

type profileFixture struct {
	f          transactionFixture
	j          *ProfileJournal
	r          ProfileTransactionRecord
	recordPath string
	old, next  []byte
}

func newProfileFixture(t *testing.T, active bool) profileFixture {
	t.Helper()
	f := newTransactionFixture(t)
	dir := filepath.Join(f.root.Data(), "servers")
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		t.Fatal("fixture server directory absent", err)
	}
	server := "old-server"
	if !active {
		server = "idle-server"
	}
	old := []byte(`{"id":"` + server + `","password":"old"}`)
	next := []byte(`{"id":"` + server + `","password":"new"}`)
	path := filepath.Join(dir, server+".json")
	transactionWrite(t, path, old)
	j, err := OpenProfileJournal(f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	config := f.next.Config
	if !active {
		config = f.before.Config
	}
	r, err := j.Begin(context.Background(), server, transactionHash(old), next, config, active, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return profileFixture{f, j, r, path, old, next}
}
func (f profileFixture) assertState(t *testing.T, next bool) {
	t.Helper()
	want := f.r.Previous
	if next {
		want = f.r.Next
	}
	for path, data := range map[string][]byte{f.recordPath: want.Record.Data, f.f.root.XrayConfig(): want.Config.Data} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("wrong state", path, err)
		}
	}
}
func (f profileFixture) assertPending(t *testing.T, want bool) {
	t.Helper()
	f.f.assertPending(t, want)
}
func TestDPIProfileJournalCommit(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "inactive", true: "active"}[active], func(t *testing.T) {
			f := newProfileFixture(t, active)
			ctx := context.Background()
			f.assertState(t, false)
			f.assertPending(t, true)
			read, err := f.j.Read(ctx)
			if err != nil || read.ID != f.r.ID || read.WasRunning != active {
				t.Fatal(read, err)
			}
			read.Next.Record.Data[0] = ' '
			again, err := f.j.Read(ctx)
			if err != nil || again.Next.Record.Data[0] != '{' {
				t.Fatal("read aliases state", err)
			}
			if err = f.j.Confirm(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { return nil }); err == nil {
				t.Fatal("uninstalled profile confirmed")
			}
			if err = f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			f.assertState(t, true)
			f.assertPending(t, true)
			if err = f.j.Confirm(ctx, f.r, nil); err == nil {
				t.Fatal("no runtime verifier")
			}
			if err = f.j.Confirm(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { return errors.New("runtime not proven") }); err == nil {
				t.Fatal("failed runtime confirmed")
			}
			f.assertPending(t, true)
			calls := 0
			err = f.j.Confirm(ctx, f.r, func(_ context.Context, r ProfileTransactionRecord) error {
				calls++
				if r.Authority != f.r.Authority || r.WasRunning != active {
					t.Fatal("wrong intent")
				}
				r.Next.Record.Data[0] = ' '
				return nil
			})
			if err != nil || calls != 1 {
				t.Fatal(err, calls)
			}
			f.assertState(t, true)
			f.assertPending(t, false)
			if err = f.j.RollbackPrepared(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { t.Fatal("missing journal callback"); return nil }); err == nil {
				t.Fatal("missing decision treated as rollback")
			}
		})
	}
}
func TestDPIProfileJournalPartialRestore(t *testing.T) {
	for _, partial := range []string{"prepared", "record", "config", "both"} {
		t.Run(partial, func(t *testing.T) {
			f := newProfileFixture(t, true)
			ctx := context.Background()
			if partial == "record" || partial == "both" {
				transactionWrite(t, f.recordPath, f.r.Next.Record.Data)
			}
			if partial == "config" || partial == "both" {
				transactionWrite(t, f.f.root.XrayConfig(), f.r.Next.Config.Data)
			}
			if partial != "prepared" && f.j.RollbackPrepared(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { return nil }) == nil {
				t.Fatal("unrestored state accepted")
			}
			if err := f.j.Restore(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			f.assertState(t, false)
			f.assertPending(t, true)
			if err := f.j.RollbackPrepared(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { return errors.New("unproven runtime") }); err == nil {
				t.Fatal("failed rollback proof")
			}
			f.assertPending(t, true)
			if err := f.j.RollbackPrepared(ctx, f.r, func(_ context.Context, r ProfileTransactionRecord) error { f.assertState(t, false); return nil }); err != nil {
				t.Fatal(err)
			}
			f.assertPending(t, false)
		})
	}
}
func TestDPIProfileJournalContextDrift(t *testing.T) {
	for _, kind := range []string{"record", "config", "active", "preferences", "settings", "receipt", "ordinary-owner", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newProfileFixture(t, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			paths := map[string]string{"record": f.recordPath, "config": f.f.root.XrayConfig(), "active": filepath.Join(f.f.root.Data(), "config", "active-server"), "preferences": filepath.Join(f.f.root.Data(), "config", "system", "server-reserve.json"), "settings": filepath.Join(f.f.root.Data(), "config", "system", "settings.json"), "receipt": filepath.Join(f.f.root.Data(), "config", "interface.json"), "ordinary-owner": filepath.Join(f.f.root.Config(), TransactionJournalFile)}
			if kind == "cancel" {
				cancel()
			} else {
				transactionWrite(t, paths[kind], []byte(`{"foreign":true}`))
			}
			calls := 0
			verify := func(context.Context, ProfileTransactionRecord) error { calls++; return nil }
			if err := f.j.Confirm(ctx, f.r, verify); err == nil || calls != 0 {
				t.Fatal("changed input confirmed", kind, err, calls)
			}
			if err := f.j.Restore(ctx, f.r); err == nil {
				t.Fatal("foreign context restored", kind)
			}
			f.assertPending(t, true)
			if kind != "cancel" {
				got, err := os.ReadFile(paths[kind])
				if err != nil || string(got) != `{"foreign":true}` {
					t.Fatal("foreign data erased", err)
				}
			}
		})
	}
}
func TestDPIProfileJournalVerifierCannotReplaceOwner(t *testing.T) {
	for _, kind := range []string{"owner-bytes", "owner-inode", "owner-symlink", "record", "settings", "records-parent", "config-parent", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newProfileFixture(t, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile)
			original, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = f.j.Confirm(ctx, f.r, func(context.Context, ProfileTransactionRecord) error {
				calls++
				switch kind {
				case "owner-bytes":
					transactionWrite(t, journal, append(original, ' '))
				case "owner-inode", "owner-symlink":
					if e := os.Rename(journal, journal+".retained"); e != nil {
						t.Fatal(e)
					}
					if kind == "owner-inode" {
						transactionWrite(t, journal, original)
					} else if e := os.Symlink(journal+".retained", journal); e != nil {
						t.Fatal(e)
					}
				case "record":
					transactionWrite(t, f.recordPath, []byte(`{"foreign":true}`))
				case "settings":
					transactionWrite(t, filepath.Join(f.f.root.Data(), "config", "system", "settings.json"), []byte(`{"foreign":true}`))
				case "records-parent", "config-parent":
					dir := filepath.Dir(f.recordPath)
					if kind == "config-parent" {
						dir = f.f.root.Config()
					}
					if e := os.Rename(dir, dir+".retained"); e != nil {
						t.Fatal(e)
					}
					if e := os.Mkdir(dir, 0700); e != nil {
						t.Fatal(e)
					}
					transactionWrite(t, filepath.Join(dir, "foreign"), []byte("keep"))
				case "cancel":
					cancel()
				}
				return nil
			})
			if err == nil || calls != 1 {
				t.Fatal("callback mutation accepted", kind, err, calls)
			}
			if kind != "config-parent" {
				if _, e := os.Lstat(journal); e != nil {
					t.Fatal("owner evidence removed", e)
				}
			} else if got, e := os.ReadFile(filepath.Join(f.f.root.Config(), "foreign")); e != nil || string(got) != "keep" {
				t.Fatal("foreign directory cleaned", e)
			}
		})
	}
}
func TestDPIProfileJournalCommittedDecision(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "foreign"}[drift], func(t *testing.T) {
			f := newProfileFixture(t, true)
			ctx := context.Background()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			err := f.j.base.locked(ctx, func() error {
				r, owner, held, e := f.j.load()
				if held != nil {
					defer held.Close()
				}
				if e != nil {
					return e
				}
				r.Phase = "committed"
				_, next, e := f.j.persist(r, owner, held)
				if next != nil {
					defer next.Close()
				}
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.j.Restore(ctx, f.r); err == nil {
				t.Fatal("committed decision rolled back")
			}
			calls := 0
			if err = f.j.RollbackPrepared(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { calls++; return nil }); err == nil || calls != 0 {
				t.Fatal("committed borrowed as live prepared")
			}
			if drift {
				transactionWrite(t, f.recordPath, []byte(`{"foreign":true}`))
			}
			err = f.j.CleanupCommitted(ctx)
			if drift {
				if err == nil {
					t.Fatal("foreign committed cleanup")
				}
				f.assertPending(t, true)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				f.assertState(t, true)
				f.assertPending(t, false)
			}
		})
	}
}
func TestDPIProfileJournalStrictDecoder(t *testing.T) {
	f := newProfileFixture(t, true)
	raw, err := json.Marshal(f.r)
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{"duplicate", "unknown", "null-running", "wrong-id", "path", "hash", "missing-old", "inactive-config", "inactive-running", "bad-phase", "missing-context", "missing-exists", "null-context", "null-id"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			var m map[string]any
			if e := json.Unmarshal(raw, &m); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "unknown":
				m["extra"] = true
			case "null-running":
				m["wasRunning"] = nil
			case "wrong-id":
				m["serverId"] = "another"
			case "path":
				m["serverId"] = "../outside"
			case "hash":
				m["next"].(map[string]any)["record"].(map[string]any)["sha256"] = strings.Repeat("0", 64)
			case "missing-old":
				delete(m, "previous")
			case "inactive-config", "inactive-running":
				m["context"].(map[string]any)["active"] = transactionImage([]byte("other\n"))
				if kind == "inactive-config" {
					m["wasRunning"] = false
				} else {
					m["next"].(map[string]any)["config"] = f.r.Previous.Config
				}
			case "bad-phase":
				m["phase"] = "done"
			case "missing-context":
				delete(m["context"].(map[string]any), "settings")
			case "missing-exists":
				delete(m["previous"].(map[string]any)["record"].(map[string]any), "exists")
			case "null-context":
				m["context"] = nil
			case "null-id":
				m["id"] = nil
			}
			bad, e := json.Marshal(m)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "duplicate" {
				bad = append([]byte(`{"phase":"prepared",`), raw[1:]...)
			}
			if _, e = decodeProfileRecord(bad); e == nil {
				t.Fatal("malformed journal accepted", kind)
			}
		})
	}
}
func TestDPIProfileJournalRejectsOtherOwnerAndSymlinks(t *testing.T) {
	f := newProfileFixture(t, false)
	ctx := context.Background()
	if _, err := f.j.Begin(ctx, f.r.ServerID, transactionHash(f.old), f.next, f.f.before.Config, false, strings.Repeat("b", 64)); err == nil {
		t.Fatal("second owner admitted")
	}
	if _, err := f.f.j.Begin(ctx, f.f.next, f.f.intent); err == nil {
		t.Fatal("reserve borrowed profile owner")
	}
	if err := f.j.Restore(ctx, f.r); err != nil {
		t.Fatal(err)
	}
	if err := f.j.RollbackPrepared(ctx, f.r, func(context.Context, ProfileTransactionRecord) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.f.j.Begin(ctx, f.f.next, f.f.intent); err != nil {
		t.Fatal(err)
	}
	if _, err := f.j.Begin(ctx, f.r.ServerID, transactionHash(f.old), f.next, f.f.before.Config, false, strings.Repeat("b", 64)); err == nil {
		t.Fatal("profile borrowed reserve owner")
	}
}

func TestDPIProfileFinalCASRejectsForeignImage(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "install", true: "restore"}[restore], func(t *testing.T) {
			f := newProfileFixture(t, true)
			// Model the target changing after the outer whole-state proof and before
			// its final file boundary. The third image is never journal authority.
			foreign := []byte(`{"id":"old-server","password":"foreign-preserve"}`)
			transactionWrite(t, f.recordPath, foreign)
			want := f.r.Next.Record
			if restore {
				want = f.r.Previous.Record
			}
			err := replaceProfileImage(f.j.records, f.r.ServerID+".json", want, f.r.Previous.Record, f.r.Next.Record, maxProfileRecordBytes)
			got, e := os.ReadFile(f.recordPath)
			if err == nil || e != nil || !bytes.Equal(got, foreign) {
				t.Fatal("foreign image adopted at final CAS", restore, err, e)
			}
			f.assertPending(t, true)
		})
	}
}

func TestDPIProfileFinalCASTargetMustBelongToJournal(t *testing.T) {
	f := newProfileFixture(t, true)
	unrecorded := transactionImage([]byte(`{"id":"old-server","password":"not-proposed"}`))
	err := replaceProfileImage(f.j.records, f.r.ServerID+".json", unrecorded, f.r.Previous.Record, f.r.Next.Record, maxProfileRecordBytes)
	if err == nil {
		t.Fatal("unrecorded target accepted")
	}
	f.assertState(t, false)
	f.assertPending(t, true)
}
