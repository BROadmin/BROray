package servers

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/udpprobe"
)

// The shipped diagnostic is deliberately IPv4-only. This private fixture
// sends a literal IPv6 SOCKS datagram without expanding that product API.
func nativeUDPIPv6(t *testing.T, socks, target string) *net.UDPAddr {
	t.Helper()
	c, err := net.DialTimeout("tcp", socks, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	var method [2]byte
	if _, err = io.ReadFull(c, method[:]); err != nil || method != [2]byte{5, 0} {
		t.Fatal("SOCKS method", err)
	}
	if _, err = c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	var head [4]byte
	if _, err = io.ReadFull(c, head[:]); err != nil || head[0] != 5 || head[1] != 0 {
		t.Fatal("SOCKS associate", err)
	}
	n := 4
	if head[3] == 4 {
		n = 16
	} else if head[3] != 1 {
		t.Fatal("unexpected relay address")
	}
	b := make([]byte, n+2)
	if _, err = io.ReadFull(c, b); err != nil {
		t.Fatal(err)
	}
	ip := net.IP(b[:n])
	if ip.IsUnspecified() {
		host, _, _ := net.SplitHostPort(socks)
		ip = net.ParseIP(host)
	}
	u, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip, Port: int(binary.BigEndian.Uint16(b[n:]))})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(5 * time.Second))
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	dst := net.ParseIP(host)
	if dst == nil || dst.To4() != nil {
		t.Fatal("requires literal IPv6 target")
	}
	request, tx, err := udpprobe.Request()
	if err != nil {
		t.Fatal(err)
	}
	packet := append([]byte{0, 0, 0, 4}, dst.To16()...)
	packet = binary.BigEndian.AppendUint16(packet, uint16(port))
	packet = append(packet, request...)
	if _, err = u.Write(packet); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	size, err := u.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if size < 10 || buf[2] != 0 {
		t.Fatal("bad relay datagram")
	}
	offset := 10
	if buf[3] == 4 {
		offset = 22
	} else if buf[3] != 1 {
		t.Fatal("bad relay datagram address")
	}
	if size < offset {
		t.Fatal("short relay datagram")
	}
	result, err := udpprobe.Parse(buf[offset:size], tx)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// A private authoritative test responder. Both its requests and answers must
// cross the MASQUE server exit; no system DNS or public resolver is involved.
func nativeDNSFixture(t *testing.T) *atomic.Int32 {
	t.Helper()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53})
	if err != nil {
		t.Fatal(err)
	}
	count := new(atomic.Int32)
	badSource := new(atomic.Bool)
	done := make(chan struct{})
	go func() {
		defer close(done)
		b := make([]byte, 4096)
		for {
			n, addr, e := pc.ReadFromUDP(b)
			if e != nil {
				return
			}
			if n < 17 || binary.BigEndian.Uint16(b[4:6]) != 1 {
				continue
			}
			end := 12
			var labels []string
			for end < n && b[end] != 0 {
				size := int(b[end])
				end++
				if size > 63 || end+size >= n {
					break
				}
				labels = append(labels, string(b[end:end+size]))
				end += size
			}
			if end+5 > n || strings.Join(labels, ".") != "native.example.test" {
				continue
			}
			end++
			typ := binary.BigEndian.Uint16(b[end : end+2])
			end += 4
			if !addr.IP.Equal(net.ParseIP("127.0.0.3")) {
				badSource.Store(true)
			}
			count.Add(1)
			reply := append([]byte(nil), b[:end]...)
			binary.BigEndian.PutUint16(reply[2:4], 0x8180)
			for i := 6; i < 12; i++ {
				reply[i] = 0
			}
			if typ == 1 {
				reply[7] = 1
				reply = append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4, 198, 18, 0, 1)
			}
			_, _ = pc.WriteToUDP(reply, addr)
		}
	}()
	t.Cleanup(func() {
		pc.Close()
		<-done
		if badSource.Load() {
			t.Error("DNS bypassed MASQUE server exit")
		}
		t.Logf("remote DNS queries through server=%d", count.Load())
	})
	return count
}

// VmHWM is the kernel's peak for this process. CPU is reported in kernel
// clock ticks, without claiming that emulated timing predicts router timing.
func nativeResourceReceipt(t *testing.T, cmd *exec.Cmd, role string) {
	t.Helper()
	started := time.Now()
	t.Cleanup(func() {
		base := fmt.Sprintf("/proc/%d/", cmd.Process.Pid)
		b, e := os.ReadFile(base + "status")
		if e != nil {
			t.Errorf("resource status %s: %v", role, e)
			return
		}
		fields := map[string]string{}
		for _, line := range strings.Split(string(b), "\n") {
			for _, k := range []string{"VmHWM:", "VmRSS:"} {
				if strings.HasPrefix(line, k) {
					fields[strings.TrimSuffix(k, ":")] = strings.TrimSpace(strings.TrimPrefix(line, k))
				}
			}
		}
		stat, e := os.ReadFile(base + "stat")
		if e != nil {
			t.Error(e)
			return
		}
		tail := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
		if len(tail) < 13 {
			t.Fatal("short proc stat")
		}
		user, _ := strconv.ParseUint(tail[11], 10, 64)
		system, _ := strconv.ParseUint(tail[12], 10, 64)
		if fields["VmHWM"] == "" {
			t.Error("missing VmHWM")
		}
		t.Logf("RESOURCE role=%s VmHWM=%s VmRSS=%s cpu_user_ticks=%d cpu_system_ticks=%d observed_ms=%d", role, fields["VmHWM"], fields["VmRSS"], user, system, time.Since(started).Milliseconds())
	})
}
