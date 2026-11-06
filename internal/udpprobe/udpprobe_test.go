package udpprobe

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func responder(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go Responder(pc)
	return pc.LocalAddr().String()
}

func TestDirectSeesOwnAddress(t *testing.T) {
	srv := responder(t)
	got, err := Direct(srv, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IP.Equal(net.ParseIP("127.0.0.1")) || got.Port == 0 {
		t.Fatalf("mapped = %v", got)
	}
}

func TestParseRejectsForeignTransaction(t *testing.T) {
	req, tx, _ := Request()
	if _, err := Parse(req, tx); err == nil {
		t.Fatal("request accepted as response")
	}
}

func TestTimeoutIsReported(t *testing.T) {
	pc, _ := net.ListenPacket("udp4", "127.0.0.1:0") // silent
	defer pc.Close()
	if _, err := Direct(pc.LocalAddr().String(), 600*time.Millisecond); err == nil {
		t.Fatal("no timeout")
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestXraySOCKS5UDP is the host half of the UDP E2E proof: a real Xray
// SOCKS inbound (udp:true, as BROray's XrayConfig generates it) relays a STUN
// request; freedom sends it from 127.0.0.3, so the responder sees a source
// that only the Xray path can produce. Set BRORAY_XRAY_BIN to run it.
func TestXraySOCKS5UDP(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	srv := responder(t)
	port := freePort(t)
	cfg := fmt.Sprintf(`{"log":{"loglevel":"warning"},
"inbounds":[{"tag":"socks-in","listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth","udp":true,"ip":"127.0.0.1"}}],
"outbounds":[{"tag":"direct","protocol":"freedom","sendThrough":"127.0.0.3"}]}`, port)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", p)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	socks := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", socks); err == nil {
			c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	direct, err := Direct(srv, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	via, err := ViaSOCKS5(socks, srv, 3*time.Second)
	if err != nil {
		t.Fatalf("UDP through Xray SOCKS5: %v", err)
	}
	t.Logf("direct=%v via-xray=%v", direct, via)
	if !via.IP.Equal(net.ParseIP("127.0.0.3")) {
		t.Fatalf("UDP did not leave through Xray: %v", via)
	}
	if via.IP.Equal(direct.IP) {
		t.Fatal("direct and Xray paths are indistinguishable")
	}
}

func TestRunVerdicts(t *testing.T) {
	srv := responder(t)
	silent, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer silent.Close()
	refused, _ := net.Listen("tcp4", "127.0.0.1:0")
	refusedAddr := refused.Addr().String()
	refused.Close()
	for _, c := range []struct {
		name, routed, direct, socks, want string
	}{
		{"bypass", srv, srv, "", "BYPASS_WAN"},
		{"timeout", silent.LocalAddr().String(), srv, "", "ROUTED_UDP_TIMEOUT"},
		{"socks down", srv, srv, refusedAddr, "SOCKS_UDP_FAILED"},
		{"no target", "", srv, "", "INCOMPLETE"},
	} {
		if got := Run(c.routed, c.direct, c.socks, 600*time.Millisecond); got.Verdict != c.want {
			t.Errorf("%s: verdict %s, want %s (%+v)", c.name, got.Verdict, c.want, got.Results)
		}
	}
}
