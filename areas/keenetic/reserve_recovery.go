package keenetic

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
	"net"
	"strconv"
	"strings"
)

func RestoreReserveInterface(ctx context.Context, d *compat.Deps, guard *platform.Lock, record reserve.TransactionRecord, currentReceipt []byte) error {
	check := func() bool { return ctx.Err() == nil && compat.FenceHeld(ctx) && guard.Owns(d.Root.DaemonLock()) }
	if !check() {
		return errReserveInterface
	}
	i := record.Intent
	old, ok := reserveReceipt(record.Receipt.Data)
	current, currentOK := reserveReceipt(currentReceipt)
	if !ok || !currentOK || !record.Receipt.Exists || record.Receipt.SHA256 != compat.SHA256Hex(record.Receipt.Data) || i.ReceiptSHA256 != record.Receipt.SHA256 ||
		old.InterfaceName != i.InterfaceName || old.Description != i.PreviousDescription || !descriptionMatches(old.Description, i.PreviousDescriptionLine) ||
		net.JoinHostPort(old.Upstream.Host, strconv.Itoa(old.Upstream.Port)) != i.Upstream || !validDescription(i.NextDescription) {
		return errReserveInterface
	}
	if current.InterfaceName != old.InterfaceName || current.Owner != old.Owner || current.Protocol != old.Protocol || current.Upstream != old.Upstream || current.WriteProtocolSHA256 != old.WriteProtocolSHA256 {
		return errReserveInterface
	}
	s := newService(d)
	sn := s.capture(ctx)
	running, runningOld, ok := reserveRecoveryBlock(sn.running, old, i)
	_, startupOld, startupOK := reserveRecoveryBlock(sn.startup, old, i)
	if sn.err != nil || !ok || !startupOK || !check() {
		return errReserveInterface
	}
	if !bytes.Equal(currentReceipt, record.Receipt.Data) {
		if current.Description != i.NextDescription || current.Contract != ownerContract || (current.SelectionMode != "repaired" && current.SelectionMode != "description-synced") {
			return errReserveInterface
		}
		// A prior recovery may already have restored the router while the
		// next receipt remains. Reconstruct only validated description variants.
		lines := []string{descriptionLine(i.NextDescription), reserveHexDescription(i.NextDescription)}
		if descriptionMatches(i.NextDescription, running[1]) {
			lines = append(lines, running[1])
		}
		matched := false
		for _, line := range lines {
			candidate := append([]string(nil), running...)
			candidate[1] = line
			sha, valid := blockSHA256(candidate)
			if valid && sha == current.RunningBlockSHA256 {
				matched = true
			}
		}
		if !matched {
			return errReserveInterface
		}
	}
	if !runningOld {
		cmd, valid := descriptionCommand(old.InterfaceName, old.Description)
		if !valid || !check() {
			return errReserveInterface
		}
		if err := s.run(ctx, old.InterfaceName, stageCmd{"rollback-description", cmd}); err != nil {
			return err
		}
	}
	if !runningOld || !startupOld {
		if !check() {
			return errReserveInterface
		}
		if err := s.save(ctx, old.InterfaceName, "rollback-save"); err != nil {
			return err
		}
	}
	after := s.capture(ctx)
	if after.err != nil || !check() || !sourceExact(after.running, old.InterfaceName, old.Upstream.Host, old.Upstream.Port, old.Description, old.RunningBlockSHA256) || !sourceExact(after.startup, old.InterfaceName, old.Upstream.Host, old.Upstream.Port, old.Description, old.StartupBlockSHA256) {
		return errReserveInterface
	}
	return nil
}

func reserveReceipt(raw []byte) (receipt, bool) {
	var r receipt
	if len(raw) == 0 || len(raw) > 64*1024 {
		return r, false
	}
	v, err := strictjson.Decode(raw)
	fields, ok := v.(map[string]any)
	if err != nil || !ok {
		return r, false
	}
	for key, value := range fields {
		if value == nil {
			return r, false
		}
		switch key {
		case "schemaVersion", "contract", "owner", "interfaceName", "protocol", "upstream", "description", "selectionMode", "writeProtocolSha256", "runningBlockSha256", "startupBlockSha256", "updatedAt":
		default:
			return r, false
		}
	}
	upstream, ok := fields["upstream"].(map[string]any)
	if !ok || len(upstream) != 2 || upstream["host"] == nil || upstream["port"] == nil {
		return r, false
	}
	if json.Unmarshal(raw, &r) != nil || r.SchemaVersion != 2 || r.Owner != "BROray" || r.Protocol != "socks5" || !validName(r.InterfaceName) || !validIPv4(r.Upstream.Host) || !validPort(r.Upstream.Port) || !validDescription(r.Description) || !acceptedContracts[r.Contract] || r.WriteProtocolSHA256 != writePolicySHA256 || r.RunningBlockSHA256 != r.StartupBlockSHA256 {
		return r, false
	}
	hash, err := hex.DecodeString(r.RunningBlockSHA256)
	return r, err == nil && len(hash) == 32 && hex.EncodeToString(hash) == r.RunningBlockSHA256
}

func reserveRecoveryBlock(lines []string, old receipt, i reserve.TransactionIntent) ([]string, bool, bool) {
	block, result := extractBlock(lines, old.InterfaceName)
	if result != blockOK || len(block) < 2 || declarationCount(lines, old.InterfaceName) != 1 {
		return nil, false, false
	}
	previous := descriptionMatches(i.PreviousDescription, block[1])
	description := i.NextDescription
	if previous {
		description = i.PreviousDescription
	}
	if !signatureExact(block, old.InterfaceName, old.Upstream.Host, old.Upstream.Port, description) {
		return nil, false, false
	}
	normalized := append([]string(nil), block...)
	normalized[1] = i.PreviousDescriptionLine
	sha, ok := blockSHA256(normalized)
	if !ok || sha != old.RunningBlockSHA256 {
		return nil, false, false
	}
	actualSHA, ok := blockSHA256(block)
	return block, previous && ok && actualSHA == old.RunningBlockSHA256, true
}

// Keenetic may render UTF-8 bytes as hexadecimal in configuration snapshots.
// This representation is used for proof only, never as a router command.
func reserveHexDescription(description string) string {
	line := descriptionLine(description)
	var b strings.Builder
	for _, c := range []byte(line) {
		if c >= 0x80 {
			b.WriteString(`\x`)
			b.WriteString(hex.EncodeToString([]byte{c}))
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
