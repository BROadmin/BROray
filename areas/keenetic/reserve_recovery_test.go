package keenetic

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
)

func reserveRecoveryFixture(t *testing.T) (*harness, *platform.Lock, reserve.TransactionRecord, []byte) {
	t.Helper()
	h := newHarness(t)
	if err := h.s.EnsureInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := h.s.ownerName(h.s.capture(context.Background()))
	before, err := os.ReadFile(h.s.receiptPath())
	if err != nil {
		t.Fatal(err)
	}
	oldLine := h.r.ifaces[name].desc
	r := reserve.TransactionRecord{SchemaVersion: 1, ID: strings.Repeat("a", 64), Phase: "prepared",
		Intent:  reserve.TransactionIntent{Action: "enable", Authority: strings.Repeat("b", 64), InterfaceName: name, Upstream: "192.168.1.1:2080", PreviousDescription: "BROray - Netherlands", PreviousDescriptionLine: oldLine, NextDescription: "BROray - mix-auto", ReceiptSHA256: compat.SHA256Hex(before)},
		Receipt: reserve.TransactionImage{Exists: true, Data: before, SHA256: compat.SHA256Hex(before)}}
	h.d.Svc.ServersInterfaceDescription = func() (string, error) { return "BROray - mix-auto", nil }
	if err := h.s.EnsureInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(h.s.receiptPath())
	if err != nil {
		t.Fatal(err)
	}
	h.d.Svc.XrayRunning = func() bool { return false }
	h.r.connected = false
	guard, err := platform.TryLock(h.d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guard.Release() })
	h.r.resetCommands()
	return h, guard, r, current
}

func TestReserveColdInterfaceAcceptsOwnedReceiptUpgrade(t *testing.T) {
	h, guard, r, current := reserveRecoveryFixture(t)
	var old receipt
	if err := json.Unmarshal(r.Receipt.Data, &old); err != nil {
		t.Fatal(err)
	}
	old.Contract = "r14c34-proxy-owned-interface/1"
	old.SelectionMode = "created"
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	r.Receipt.Data, r.Receipt.SHA256 = raw, compat.SHA256Hex(raw)
	r.Intent.ReceiptSHA256 = r.Receipt.SHA256
	if err := RestoreReserveInterface(compat.WithFence(context.Background()), h.d, guard, r, current); err != nil {
		t.Fatal("accepted owned receipt upgrade blocked", err)
	}
	onlyDescriptionCommands(t, h, r.Intent.InterfaceName)
}

func TestReserveColdInterfaceRetainsNativeUnicodeProof(t *testing.T) {
	for _, nativeHex := range []bool{false, true} {
		t.Run(map[bool]string{false: "literal", true: "hex"}[nativeHex], func(t *testing.T) {
			h, guard, r, _ := reserveRecoveryFixture(t)
			name := r.Intent.InterfaceName
			desc := "BROray - Сервер 🛰"
			line := descriptionLine(desc)
			if nativeHex {
				line = reserveHexDescription(desc)
			}
			h.r.ifaces[name].desc = line
			h.r.startup = h.r.render()
			block, result := extractBlock(h.r.render(), name)
			if result != blockOK {
				t.Fatal("fixture block")
			}
			hash, ok := blockSHA256(block)
			if !ok {
				t.Fatal("fixture hash")
			}
			var old receipt
			if err := json.Unmarshal(r.Receipt.Data, &old); err != nil {
				t.Fatal(err)
			}
			old.Description, old.RunningBlockSHA256, old.StartupBlockSHA256 = desc, hash, hash
			raw, err := json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			r.Receipt.Data, r.Receipt.SHA256 = raw, compat.SHA256Hex(raw)
			r.Intent.PreviousDescription, r.Intent.PreviousDescriptionLine, r.Intent.ReceiptSHA256 = desc, line, r.Receipt.SHA256
			if err := RestoreReserveInterface(compat.WithFence(context.Background()), h.d, guard, r, raw); err != nil {
				t.Fatal("native old proof", err)
			}
			if len(h.r.cmds) != 0 {
				t.Fatal("already restored native block rewritten")
			}
			// Confirm a partially switched native block belongs to the original
			// receipt without guessing how the old UTF-8 was rendered.
			h.r.ifaces[name].desc = descriptionLine(r.Intent.NextDescription)
			if _, previous, ok := reserveRecoveryBlock(h.r.render(), old, r.Intent); !ok || previous {
				t.Fatal("native transition proof")
			}
			if !nativeHex {
				if err := RestoreReserveInterface(compat.WithFence(context.Background()), h.d, guard, r, raw); err != nil {
					t.Fatal("Unicode rollback", err)
				}
				onlyDescriptionCommands(t, h, name)
			}
		})
	}
}

func TestReserveColdInterfaceRestoresOnlyDescriptionWithStoppedCore(t *testing.T) {
	for _, phase := range []string{"saved-next", "partial-save", "already-old"} {
		t.Run(phase, func(t *testing.T) {
			h, guard, r, current := reserveRecoveryFixture(t)
			name := r.Intent.InterfaceName
			if phase == "partial-save" {
				h.r.startup = append([]string(nil), h.r.render()...)
				for i, line := range h.r.startup {
					if line == descriptionLine(r.Intent.NextDescription) {
						h.r.startup[i] = r.Intent.PreviousDescriptionLine
					}
				}
				current = append([]byte(nil), r.Receipt.Data...)
			}
			if phase == "already-old" {
				h.r.ifaces[name].desc = r.Intent.PreviousDescriptionLine
				h.r.startup = h.r.render()
			}
			before := *h.r.ifaces[name]
			if err := RestoreReserveInterface(compat.WithFence(context.Background()), h.d, guard, r, current); err != nil {
				t.Fatal("cold description restore", err)
			}
			if h.r.ifaces[name].desc != descriptionLine(r.Intent.PreviousDescription) {
				t.Fatal("description not restored")
			}
			after := *h.r.ifaces[name]
			after.desc = before.desc
			if after.upstream != before.upstream || after.protocol != before.protocol || after.up != before.up || after.udp != before.udp {
				t.Fatal("connection changed")
			}
			onlyDescriptionCommands(t, h, name)
			got, _ := os.ReadFile(h.s.receiptPath())
			if bytes.Equal(got, r.Receipt.Data) {
				t.Fatal("router helper wrote local receipt before journal CAS")
			}
		})
	}
}

func TestReserveColdInterfaceRejectsForeignOrUnboundState(t *testing.T) {
	for _, kind := range []string{"unfenced", "guard-lost", "extra", "endpoint", "description", "receipt", "old-line", "hash", "save-failed"} {
		t.Run(kind, func(t *testing.T) {
			h, guard, r, current := reserveRecoveryFixture(t)
			name := r.Intent.InterfaceName
			ctx := compat.WithFence(context.Background())
			switch kind {
			case "unfenced":
				ctx = context.Background()
			case "guard-lost":
				guard.Release()
			case "extra":
				h.r.ifaces[name].extra = []string{"    mtu 1400"}
			case "endpoint":
				h.r.ifaces[name].upstream = "192.168.1.1 2081"
			case "description":
				h.r.ifaces[name].desc = descriptionLine("BROray - foreign")
			case "receipt":
				current = []byte(`{"owner":"foreign"}`)
			case "old-line":
				r.Intent.PreviousDescriptionLine = descriptionLine("BROray - somebody")
			case "hash":
				r.Intent.ReceiptSHA256 = strings.Repeat("0", 64)
			case "save-failed":
				h.r.noSave = true
			}
			if err := RestoreReserveInterface(ctx, h.d, guard, r, current); err == nil {
				t.Fatal("unconfirmed recovery accepted")
			}
			if kind != "save-failed" && len(h.r.cmds) != 0 {
				t.Fatal("foreign/unbound state mutated")
			}
		})
	}
}
