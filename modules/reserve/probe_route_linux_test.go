package reserve

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func healthFreeEndpoint(t *testing.T, network, address string) netip.AddrPort {
	t.Helper()
	l, err := net.Listen(network, address)
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).AddrPort()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// This local SOCKS server echoes only the allowed control request. Thus a
// successful echo proves that Xray used this upstream, not DIRECT/DNS.
func healthUpstream(t *testing.T) (netip.AddrPort, *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	hits := new(atomic.Int32)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(4 * time.Second))
				var hello [2]byte
				if _, err := io.ReadFull(c, hello[:]); err != nil || hello[0] != 5 || hello[1] == 0 {
					return
				}
				methods := make([]byte, int(hello[1]))
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				if _, err := c.Write([]byte{5, 0}); err != nil {
					return
				}
				var request [5]byte
				if _, err := io.ReadFull(c, request[:]); err != nil || request[0] != 5 || request[1] != 1 || request[3] != 3 {
					return
				}
				dest := make([]byte, int(request[4])+2)
				if _, err := io.ReadFull(c, dest); err != nil {
					return
				}
				host := string(dest[:len(dest)-2])
				if (host != "cp.cloudflare.com" && host != "www.gstatic.com") || binary.BigEndian.Uint16(dest[len(dest)-2:]) != 443 {
					return
				}
				hits.Add(1)
				if _, err := c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
					return
				}
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return l.Addr().(*net.TCPAddr).AddrPort(), hits
}

func healthEchoViaSOCKS(proxy netip.AddrPort, host string, port uint16) bool {
	c, err := net.DialTimeout("tcp", proxy.String(), time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		return false
	}
	var hello [2]byte
	if _, err = io.ReadFull(c, hello[:]); err != nil || hello != [2]byte{5, 0} {
		return false
	}
	request := []byte{5, 1, 0}
	if ip, err := netip.ParseAddr(host); err == nil {
		kind := byte(4)
		if ip.Is4() {
			kind = 1
		}
		request = append(request, kind)
		request = append(request, ip.AsSlice()...)
	} else {
		request = append(request, 3, byte(len(host)))
		request = append(request, []byte(host)...)
	}
	request = binary.BigEndian.AppendUint16(request, port)
	if _, err = c.Write(request); err != nil {
		return false
	}
	var response [4]byte
	if _, err = io.ReadFull(c, response[:]); err != nil || response[0] != 5 || response[1] != 0 {
		return false
	}
	n := 4
	switch response[3] {
	case 1:
	case 4:
		n = 16
	case 3:
		var size [1]byte
		if _, err = io.ReadFull(c, size[:]); err != nil {
			return false
		}
		n = int(size[0])
	default:
		return false
	}
	if _, err = io.ReadFull(c, make([]byte, n+2)); err != nil {
		return false
	}
	payload := []byte("reserve-route-proof")
	if _, err = c.Write(payload); err != nil {
		return false
	}
	got := make([]byte, len(payload))
	_, err = io.ReadFull(c, got)
	return err == nil && string(got) == string(payload)
}

func TestReserveHealthRouteActualXray(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal("required pinned Xray fixture unavailable", err)
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != "c2d1e66a130556d32089adcf38202fcb6f9027e41fb456cc2cda9a5fabdc093a" {
		t.Fatal("Xray fixture identity mismatch")
	}
	upstream, hits := healthUpstream(t)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _ = c.SetDeadline(time.Now().Add(4 * time.Second)); _, _ = io.Copy(c, c) }()
		}
	}()
	targetEP := target.Addr().(*net.TCPAddr).AddrPort()
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network == "tcp6" {
				address = "[::1]:0"
			}
			normal := healthFreeEndpoint(t, network, address)
			diagnostic := healthFreeEndpoint(t, network, address)
			if normal.Port() == diagnostic.Port() {
				t.Fatal("fixture endpoint collision; do not silently retry")
			}
			cfg := map[string]any{
				"log":       map[string]any{"loglevel": "warning"},
				"inbounds":  []any{map[string]any{"tag": "socks", "listen": normal.Addr().String(), "port": normal.Port(), "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}},
				"outbounds": []any{map[string]any{"tag": "proxy", "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": upstream.Addr().String(), "port": upstream.Port()}}}}, map[string]any{"tag": "direct", "protocol": "freedom"}},
				"routing":   map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"type": "field", "inboundTag": []string{"socks"}, "network": "tcp", "outboundTag": "direct"}}},
			}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := AttachHealthInbound(raw, diagnostic)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, rendered, 0600); err != nil {
				t.Fatal(err)
			}
			log, err := os.Create(filepath.Join(dir, "xray.log"))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, "run", "-c", path)
			cmd.Stdout = log
			cmd.Stderr = log
			if err := cmd.Start(); err != nil {
				_ = log.Close()
				t.Fatal(err)
			}
			// Never reap asynchronously: until this cleanup, this exact child
			// retains its PID even if Xray has already exited.
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = log.Close() })
			ready := false
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				p, e := CaptureProcess(cmd.Process.Pid, bin)
				if e == nil {
					if _, e = VerifyListener(p, diagnostic); e == nil {
						ready = true
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !ready {
				t.Fatal("owned diagnostic listener not ready, PID=" + strconv.Itoa(cmd.Process.Pid))
			}
			before := hits.Load()
			if !healthEchoViaSOCKS(normal, targetEP.Addr().String(), targetEP.Port()) || hits.Load() != before {
				t.Fatal("normal DIRECT rule changed")
			}
			for _, host := range []string{"cp.cloudflare.com", "www.gstatic.com"} {
				if !healthEchoViaSOCKS(diagnostic, host, 443) {
					t.Fatal("diagnostic proxy path failed", host)
				}
			}
			if hits.Load() != before+2 {
				t.Fatal("control requests did not use designated proxy")
			}
			if healthEchoViaSOCKS(diagnostic, targetEP.Addr().String(), targetEP.Port()) || healthEchoViaSOCKS(diagnostic, "cp.cloudflare.com", 80) || hits.Load() != before+2 {
				t.Fatal("diagnostic listener permits non-control traffic")
			}
		})
	}
}
