package keenetic

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
)

// SOCKS5 UDP capability. "interface ProxyN proxy socks5-udp" exists since
// KeeneticOS 4.1; older firmware rejects the command as unknown. When the
// socks5-udp stage is rejected that way, BROray records it and keeps the
// ProxyN TCP-only: create succeeds, status warns PROXY_SOCKS5_UDP_UNSUPPORTED,
// matchesExpected does not require UDP, reconcile does not repair it again
// and a rollback never sends "no proxy socks5-udp". The record names the
// KeeneticOS release ("show version"); after a firmware change it no longer
// applies and the next create/repair/reconcile probes again. Without a
// readable release it applies only to the daemon run that wrote it.

const udpUnsupportedMsg = "KeeneticOS не поддерживает UDP через SOCKS5 (нужна версия 4.1+): звонки и QUIC не идут через BROray."

func (s *service) udpCapFile() string { return s.d.Path("state", "keenetic-socks5-udp.json") }

// unsupportedCommand is the conservative classifier of a rejected stage:
// only a KeeneticOS "unknown command" answer counts; any other error (an
// argument error, a refused protocol, a transport failure) stays a failure.
func unsupportedCommand(st []kn.Status) bool {
	for _, x := range st {
		if x.Status != "error" {
			continue
		}
		m := strings.ToLower(x.Message)
		if strings.Contains(m, "no such command") || strings.Contains(m, "unknown command") || strings.Contains(m, "command not found") {
			return true
		}
	}
	return false
}

// udpUnsupportedErr reports that err is the socks5-udp stage rejected as an
// unknown command.
func udpUnsupportedErr(err error) bool {
	var ae *actionError
	return errors.As(err, &ae) && ae.unsupported && strings.TrimPrefix(ae.stage, "rollback-") == "socks5-udp"
}

// firmware is the KeeneticOS release from "show version" ("" if unknown);
// read once per daemon run (a firmware update reboots the router).
func (s *service) firmware(ctx context.Context) string {
	s.capMu.Lock()
	defer s.capMu.Unlock()
	if s.fwRead {
		return s.fw
	}
	if s.d.App == nil || s.d.App.T == nil {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var v struct {
		Release string `json:"release"`
	}
	if err := s.d.App.T.Show(cctx, "show/version", &v); err != nil {
		return "" // retried on the next call
	}
	s.fw, s.fwRead = strings.TrimSpace(v.Release), true
	if len(s.fw) > 64 {
		s.fw = s.fw[:64]
	}
	return s.fw
}

// udpUnsupported reports a valid "socks5-udp unsupported" record for the
// running firmware (or, without a known release, for this daemon run).
func (s *service) udpUnsupported(ctx context.Context) bool {
	m, ok := compat.ReadObject(s.udpCapFile())
	if !ok || !numIs(m["schemaVersion"], 1) || m["socks5UdpSupported"] != false {
		return false
	}
	recorded, _ := m["firmware"].(string)
	if recorded != "" {
		return recorded == s.firmware(ctx)
	}
	token, _ := m["daemonRun"].(string)
	return token != "" && token == s.runToken
}

// recordUDP stores the probe result: unsupported → the record, supported →
// no record.
func (s *service) recordUDP(ctx context.Context, supported bool) {
	if supported {
		_ = os.Remove(s.udpCapFile())
		return
	}
	fw := s.firmware(ctx)
	m := map[string]any{"schemaVersion": 1, "socks5UdpSupported": false, "firmware": fw, "observedAt": compat.Now()}
	if fw == "" {
		m["daemonRun"] = s.runToken
	}
	_ = os.MkdirAll(s.d.Root.State(), 0o700)
	if err := compat.WriteJSON(s.udpCapFile(), m); err != nil {
		s.d.Log.Printf("keenetic: socks5-udp capability: %v", err)
	}
	s.d.Log.Printf("keenetic: KeeneticOS %q rejected \"proxy socks5-udp\" as an unknown command: ProxyN stays TCP-only", fw)
}

func numIs(v any, n int) bool {
	switch x := v.(type) {
	case float64:
		return x == float64(n)
	case int:
		return x == n
	case interface{ Int64() (int64, error) }:
		i, err := x.Int64()
		return err == nil && i == int64(n)
	}
	return false
}
