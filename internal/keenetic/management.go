package keenetic

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
)

// ManagementEndpoint is the router's LAN HTTP management endpoint used for
// /auth. It must be an explicit private address: no loopback or DNS fallback.
// Ported from BROmihomo internal/platform/management.go.
type ManagementEndpoint struct {
	Address netip.Addr
	Port    uint16
}

func (e ManagementEndpoint) URL() (*url.URL, error) {
	if !e.Address.IsValid() || !e.Address.IsPrivate() || e.Address.IsLoopback() || e.Port == 0 {
		return nil, errors.New("INVALID_MANAGEMENT_ENDPOINT")
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(e.Address.String(), strconv.Itoa(int(e.Port))), Path: "/auth"}, nil
}
