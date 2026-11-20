package reserve

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The child listens until its parent closes stdin. No signals or PID lookup
// are needed for fixture cleanup, and it cannot contact an external host.
func init() {
	if os.Getenv("BRORAY_RESERVE_LISTENER_CHILD") != "1" {
		return
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, l.Addr())
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
	_ = l.Close()
	os.Exit(0)
}

func ownProcess(t *testing.T) ProcessIdentity {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := CaptureProcess(os.Getpid(), exe)
	if err != nil {
		t.Fatal(err)
	}
	if p.PID != os.Getpid() || p.BootID == "" || p.StartTicks == 0 || p.Executable == "" || p.Inode == 0 {
		t.Fatal("incomplete process evidence", p)
	}
	return p
}

func TestReserveOwnedIPv4AndIPv6Listener(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0", "0.0.0.0:0", "[::]:0"} {
		t.Run(addr, func(t *testing.T) {
			network := "tcp4"
			if strings.Contains(addr, "[") {
				network = "tcp6"
			}
			l, err := net.Listen(network, addr)
			if err != nil {
				t.Fatal("required VM loopback listener unavailable", err)
			}
			defer l.Close()
			bound := l.Addr().(*net.TCPAddr).AddrPort()
			endpoint := bound
			if bound.Addr().IsUnspecified() {
				ip := netip.MustParseAddr("127.0.0.1")
				if bound.Addr().Is6() {
					ip = netip.IPv6Loopback()
				}
				endpoint = netip.AddrPortFrom(ip, bound.Port())
			}
			p := ownProcess(t)
			proof, err := VerifyListener(p, endpoint)
			if err != nil || proof.Process != p || proof.Endpoint != endpoint || proof.SocketInode == 0 {
				t.Fatal("owned listener not proven", proof, err)
			}
			_ = l.Close()
			if _, err := VerifyListener(p, endpoint); err == nil {
				t.Fatal("closed listener remains ready")
			}
		})
	}
}

func TestReserveRejectsForeignListener(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "BRORAY_RESERVE_LISTENER_CHILD=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Error("fixture did not exit cleanly", err)
		}
	}()
	if err := stdout.(*os.File).SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := netip.ParseAddrPort(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyListener(ownProcess(t), endpoint); err == nil {
		t.Fatal("foreign child socket attributed to parent")
	}
	child, err := CaptureProcess(cmd.Process.Pid, exe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyListener(child, endpoint); err != nil {
		t.Fatal("actual child owner refused", err)
	}
}

func TestReserveRejectsChangedProcessEvidence(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	endpoint := l.Addr().(*net.TCPAddr).AddrPort()
	p := ownProcess(t)
	for i, mutate := range []func(*ProcessIdentity){
		func(v *ProcessIdentity) { v.StartTicks++ },
		func(v *ProcessIdentity) { v.BootID = "foreign-boot" },
		func(v *ProcessIdentity) { v.Inode++ },
		func(v *ProcessIdentity) { v.Device++ },
		func(v *ProcessIdentity) { v.Executable = "/missing/reserve-xray" },
		func(v *ProcessIdentity) { v.PID = 0 },
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			bad := p
			mutate(&bad)
			if _, err := VerifyListener(bad, endpoint); err == nil {
				t.Fatal("changed identity accepted")
			}
		})
	}
}

func TestReserveListenerRejectsInvalidInputs(t *testing.T) {
	if _, err := CaptureProcess(0, "/bin/false"); err == nil {
		t.Fatal("PID0 accepted")
	}
	if _, err := CaptureProcess(os.Getpid(), "/bin/false"); err == nil {
		t.Fatal("wrong executable accepted")
	}
	p := ownProcess(t)
	for _, endpoint := range []netip.AddrPort{{}, netip.MustParseAddrPort("127.0.0.1:0"), netip.MustParseAddrPort("0.0.0.0:80")} {
		if _, err := VerifyListener(p, endpoint); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
}

func TestReserveRejectsAmbiguousReusePort(t *testing.T) {
	lc := net.ListenConfig{Control: func(network, address string, raw syscall.RawConn) error {
		var optionErr error
		err := raw.Control(func(fd uintptr) {
			optionErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		})
		if err != nil {
			return err
		}
		return optionErr
	}}
	first, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := lc.Listen(context.Background(), "tcp4", first.Addr().String())
	if err != nil {
		t.Fatal("required ambiguity fixture unavailable", err)
	}
	defer second.Close()
	endpoint := first.Addr().(*net.TCPAddr).AddrPort()
	if _, err := VerifyListener(ownProcess(t), endpoint); err == nil {
		t.Fatal("two different matching sockets accepted as a unique listener")
	}
}

func TestReserveProcAddressUsesNativeWordsAndRejectsMalformed(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1", "2001:db8::1234"} {
		ip := netip.MustParseAddr(address)
		b := append([]byte(nil), ip.AsSlice()...)
		for i := 0; i < len(b); i += 4 {
			word := binary.BigEndian.Uint32(b[i : i+4])
			binary.NativeEndian.PutUint32(b[i:i+4], word)
		}
		endpoint, err := procEndpoint(hex.EncodeToString(b) + ":1234")
		if err != nil || endpoint != netip.AddrPortFrom(ip, 0x1234) {
			t.Fatal(address, endpoint, err)
		}
	}
	for _, raw := range []string{"", "0100:1234", "0100007F:0000", "0100007F:FFFFF", "0100007F:0001:0002", "ZZ00007F:1234"} {
		if _, err := procEndpoint(raw); err == nil {
			t.Fatal("malformed proc address accepted", raw)
		}
	}
}

func TestReserveProcStartRejectsWrongPIDAndZombie(t *testing.T) {
	fields := append([]string{"S"}, make([]string, 19)...)
	for i := 1; i < len(fields); i++ {
		fields[i] = "0"
	}
	fields[19] = "12345"
	valid := "42 (worker with ) name) " + strings.Join(fields, " ")
	if start, err := startTicks([]byte(valid), 42); err != nil || start != 12345 {
		t.Fatal(start, err)
	}
	for _, raw := range []string{
		strings.Replace(valid, "42 (", "43 (", 1),
		strings.Replace(valid, ") S ", ") Z ", 1),
		strings.Replace(valid, "12345", "0", 1),
		"42 worker S 0",
	} {
		if _, err := startTicks([]byte(raw), 42); err == nil {
			t.Fatal("invalid stat accepted", raw)
		}
	}
}
