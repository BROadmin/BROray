package servers

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDPISourceValidationServiceExists(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	if !reflect.ValueOf(f.e.s).MethodByName("ValidateSourceDPIProposals").IsValid() {
		t.Fatal("owned batch DPI source validation service absent")
	}
}

func dpiBatchInputs(t *testing.T, f reserveRuntimeFixture) []compat.DPIProposal {
	t.Helper()
	var proposals []compat.DPIProposal
	for _, link := range []string{"trojan://old@127.0.0.1:3#First", "trojan://old@127.0.0.1:4#Second"} {
		id := f.e.addServer(t, link)
		rec, err := ReadRecord(f.e.d, id)
		if err != nil {
			t.Fatal(err)
		}
		rec["source"] = map[string]any{"type": "subscription", "subscriptionId": "dpi-source", "enabled": true}
		rec, err = prepareDPIRecord(rec, true)
		if err != nil {
			t.Fatal(err)
		}
		if err = WriteRecord(f.e.d, rec); err != nil {
			t.Fatal(err)
		}
		path := RecordPath(f.e.d, id)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rec, err = ReadRecord(f.e.d, id)
		if err != nil {
			t.Fatal(err)
		}
		rec["password"] = "new"
		next, err := Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		proposals = append(proposals, compat.DPIProposal{Path: path, PreviousSHA256: compat.SHA256Hex(raw), Record: next})
	}
	return proposals
}
func TestDPISourceBatchValidationLease(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	proposals := dpiBatchInputs(t, f)
	calls := 0
	before := f.process.RuntimeSnapshot()
	lease, err := f.e.s.validateSourceDPIProposalsWith(f.ctx, "dpi-source", proposals, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		calls++
		return reserveVerifiedOutcome(ctx, p)
	})
	if err != nil || lease == nil || calls != 2 {
		t.Fatal("batch failed", err, calls)
	}
	defer lease.Close()
	if err = lease.Check(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range proposals {
		raw, e := os.ReadFile(p.Path)
		if e != nil || compat.SHA256Hex(raw) != p.PreviousSHA256 || bytes.Equal(raw, p.Record) {
			t.Fatal("old image mutated", e)
		}
	}
	if before != f.process.RuntimeSnapshot() {
		t.Fatal("main changed")
	}
	// Planned changes of roster and records do not change the validated connection.
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
	if err = os.WriteFile(proposals[0].Path, proposals[0].Record, 0600); err != nil {
		t.Fatal(err)
	}
	if err = lease.Check(f.ctx); err != nil {
		t.Fatal("planned catalog/roster changes invalidated connection", err)
	}
	proposals[0].Record = append(proposals[0].Record, ' ')
	if err = lease.Check(f.ctx); err == nil {
		t.Fatal("candidate bytes changed")
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err = lease.Check(f.ctx); err == nil {
		t.Fatal("closed lease accepted")
	}
}
func TestDPISourceBatchRejectsDriftAcrossProbes(t *testing.T) {
	for _, kind := range []string{"first-record", "settings", "core", "preferences", "main", "input", "unhealthy", "unknown", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			proposals := dpiBatchInputs(t, f)
			calls := 0
			lease, err := f.e.s.validateSourceDPIProposalsWith(f.ctx, "dpi-source", proposals, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
				calls++
				out := reserveVerifiedOutcome(ctx, p)
				if calls != 2 {
					return out
				}
				switch kind {
				case "first-record":
					raw, e := os.ReadFile(proposals[0].Path)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(proposals[0].Path, append(raw, ' '), 0600); e != nil {
						t.Fatal(e)
					}
				case "settings":
					if e := os.WriteFile(settingsFile(f.e.d), []byte(`{"socksPort":40031}`), 0600); e != nil {
						t.Fatal(e)
					}
				case "core":
					path := f.e.d.Root.XrayBinary()
					raw, e := os.ReadFile(path)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.Rename(path, path+".old"); e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(path, raw, 0700); e != nil {
						t.Fatal(e)
					}
				case "preferences":
					writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
				case "main":
					if e := f.process.Stop(context.Background()); e != nil {
						t.Fatal(e)
					}
				case "input":
					proposals[0].Record = append(proposals[0].Record, ' ')
				case "unhealthy":
					out.Verdict = "unhealthy"
				case "unknown":
					out.Verdict = "unknown"
				case "cleanup":
					work := filepath.Dir(filepath.Dir(p.ConfigPath))
					if e := os.Rename(work, work+"-retained"); e != nil {
						t.Fatal(e)
					}
					if e := os.Mkdir(work, 0700); e != nil {
						t.Fatal(e)
					}
				}
				return out
			})
			if lease != nil {
				lease.Close()
				t.Fatal("invalid lease returned")
			}
			if err == nil || calls != 2 {
				t.Fatal("drift admitted", kind, err, calls)
			}
			if kind == "unhealthy" && !errors.Is(err, compat.ErrDPIProposalUnhealthy) {
				t.Fatal("unhealthy classification", err)
			}
			if kind == "cleanup" && !errors.Is(err, compat.ErrReserveTransactionPending) {
				t.Fatal("cleanup lost pending authority", err)
			}
		})
	}
}
func TestDPISourceBatchAdmissionAndProductionProbe(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	base := dpiBatchInputs(t, f)
	for _, kind := range []string{"source", "duplicate", "path", "hash", "ordinary", "unfenced", "empty"} {
		t.Run(kind, func(t *testing.T) {
			proposals := append([]compat.DPIProposal(nil), base...)
			source := "dpi-source"
			ctx := f.ctx
			switch kind {
			case "source":
				source = "foreign"
			case "duplicate":
				proposals[1] = proposals[0]
			case "path":
				proposals[0].Path = filepath.Join(t.TempDir(), "foreign")
			case "hash":
				proposals[0].PreviousSHA256 = "wrong"
			case "ordinary":
				r, e := readRecordFile(proposals[0].Path)
				if e != nil {
					t.Fatal(e)
				}
				r, e = prepareDPIRecord(r, false)
				if e != nil {
					t.Fatal(e)
				}
				proposals[0].Record, e = Marshal(r)
				if e != nil {
					t.Fatal(e)
				}
			case "unfenced":
				ctx = context.Background()
			case "empty":
				proposals = nil
			}
			calls := 0
			lease, err := f.e.s.validateSourceDPIProposalsWith(ctx, source, proposals, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
				calls++
				return reserveVerifiedOutcome(ctx, p)
			})
			if lease != nil {
				lease.Close()
			}
			if err == nil || lease != nil || calls != 0 {
				t.Fatal("invalid admission", kind, err, calls)
			}
		})
	}
	lease, err := f.e.s.ValidateSourceDPIProposals(f.ctx, "dpi-source", base)
	if lease != nil {
		lease.Close()
	}
	if !errors.Is(err, compat.ErrDPIProposalUnhealthy) || lease != nil {
		t.Fatal("production dead proxy accepted", err)
	}
}

func TestDPISourceLeaseCommitAuthority(t *testing.T) {
	for _, kind := range []string{"unfenced", "settings", "core"} {
		t.Run(kind, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			proposals := dpiBatchInputs(t, f)
			lease, err := f.e.s.validateSourceDPIProposalsWith(f.ctx, "dpi-source", proposals, reserveVerifiedOutcome)
			if err != nil || lease == nil {
				t.Fatal(err)
			}
			defer lease.Close()
			ctx := f.ctx
			switch kind {
			case "unfenced":
				ctx = context.Background()
			case "settings":
				if err = os.WriteFile(settingsFile(f.e.d), []byte(`{"socksPort":40129}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "core":
				path := f.e.d.Root.XrayBinary()
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.Rename(path, path+".retained"); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path, raw, 0700); e != nil {
					t.Fatal(e)
				}
			}
			if err = lease.Check(ctx); err == nil {
				t.Fatal("changed/unfenced commit authority accepted", kind)
			}
		})
	}
}
