package keenetic

import (
	"context"
	"errors"
	"net/netip"

	"github.com/BROadmin/broray/internal/compat"
)

var errReserveInterface = errors.New("RESERVE_INTERFACE_UNCONFIRMED")

func (s *service) VerifyOwnedInterfaceConfiguration(ctx context.Context, description string) (compat.OwnedInterfaceTarget, error) {
	return s.verifyOwnedInterfaceTarget(ctx, description, false)
}

func (s *service) VerifyOwnedInterfaceTarget(ctx context.Context, description string) (compat.OwnedInterfaceTarget, error) {
	return s.verifyOwnedInterfaceTarget(ctx, description, true)
}

// Configuration ownership survives a disconnected core. Commit verification
// additionally requires the existing live SOCKS/interface readiness checks.
func (s *service) verifyOwnedInterfaceTarget(ctx context.Context, description string, live bool) (compat.OwnedInterfaceTarget, error) {
	if ctx.Err() != nil || !compat.FenceHeld(ctx) || !validDescription(description) {
		return compat.OwnedInterfaceTarget{}, errReserveInterface
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expected, ok := s.expectedDescription()
	if !ok || expected != description {
		return compat.OwnedInterfaceTarget{}, errReserveInterface
	}
	sn := s.capture(ctx)
	name := s.ownerName(sn)
	if name == "" {
		return compat.OwnedInterfaceTarget{}, errReserveInterface
	}
	r, ok := s.ownerValid(sn, name)
	e := s.readExpected(ctx, name)
	if !ok || r.SchemaVersion != 2 || (live && !e.Active) || e.Listen == "" || !validPort(e.Port) ||
		r.Description != description || r.Upstream.Host != e.Listen || r.Upstream.Port != e.Port || (live && !s.runtimeReady(ctx, name, description)) {
		return compat.OwnedInterfaceTarget{}, errReserveInterface
	}
	// Never accept a cached status as commit evidence. Repeat ownership and
	// expected runtime checks after the router's live interface response.
	after, ok := s.ownerValid(s.capture(ctx), name)
	finalDescription, descriptionOK := s.expectedDescription()
	if !ok || after != r || !descriptionOK || finalDescription != description || s.readExpected(ctx, name) != e || (live && !s.runtimeReady(ctx, name, description)) || ctx.Err() != nil {
		return compat.OwnedInterfaceTarget{}, errReserveInterface
	}
	addr, err := netip.ParseAddr(e.Listen)
	if err != nil || !addr.Is4() || addr.IsUnspecified() {
		return compat.OwnedInterfaceTarget{}, errReserveInterface
	}
	block, result := extractBlock(sn.running, name)
	if result != blockOK || len(block) < 2 {
		return compat.OwnedInterfaceTarget{}, errReserveInterface
	}
	return compat.OwnedInterfaceTarget{Name: name, Upstream: netip.AddrPortFrom(addr, uint16(e.Port)), DescriptionLine: block[1]}, nil
}

func (s *service) VerifyOwnedInterface(ctx context.Context, description string) (string, error) {
	target, err := s.VerifyOwnedInterfaceTarget(ctx, description)
	return target.Name, err
}
