package reserve

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

// ProcessIdentity identifies a Linux process instance, not merely its PID.
type ProcessIdentity struct {
	PID           int
	StartTicks    uint64
	BootID        string
	Executable    string
	Device, Inode uint64
}

// ListenerProof is read-only evidence. It does not prove proxy traffic,
// configuration identity, or authorize signaling the process.
type ListenerProof struct {
	Process     ProcessIdentity
	Endpoint    netip.AddrPort
	SocketInode uint64
}

var errProcess = errors.New("RESERVE_PROCESS_UNCONFIRMED")
var errListener = errors.New("RESERVE_LISTENER_UNCONFIRMED")

// openProcess pins a real procfs directory. Reading a reused pathname must
// never silently substitute a different process for a captured instance.
func openProcess(pid int) (*os.File, error) {
	if pid <= 0 {
		return nil, errProcess
	}
	root, err := platform.OpenDirNoFollow("/proc")
	if err != nil {
		return nil, errProcess
	}
	defer root.Close()
	var fs unix.Statfs_t
	if unix.Fstatfs(int(root.Fd()), &fs) != nil || fs.Type != unix.PROC_SUPER_MAGIC {
		return nil, errProcess
	}
	dir, err := platform.OpenDirAtNoFollow(root, strconv.Itoa(pid))
	if err != nil {
		return nil, errProcess
	}
	return dir, nil
}

func procRead(dir *os.File, name string, limit int64) ([]byte, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, errProcess
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errProcess
	}
	return b, nil
}

func startTicks(raw []byte, pid int) (uint64, error) {
	s := string(raw)
	end := strings.LastIndexByte(s, ')')
	if !strings.HasPrefix(s, strconv.Itoa(pid)+" (") || end < 0 {
		return 0, errProcess
	}
	f := strings.Fields(s[end+1:])
	if len(f) < 20 || len(f[0]) != 1 || f[0] == "Z" || f[0] == "X" {
		return 0, errProcess
	}
	n, err := strconv.ParseUint(f[19], 10, 64)
	if err != nil || n == 0 {
		return 0, errProcess
	}
	return n, nil
}

func capture(dir *os.File, pid int, executable string) (ProcessIdentity, error) {
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return ProcessIdentity{}, errProcess
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ProcessIdentity{}, errProcess
	}
	bootID := strings.TrimSpace(string(boot))
	if len(bootID) != 36 {
		return ProcessIdentity{}, errProcess
	}
	raw, err := procRead(dir, "stat", 8192)
	if err != nil {
		return ProcessIdentity{}, errProcess
	}
	start, err := startTicks(raw, pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	var expected, actual, after unix.Stat_t
	if unix.Stat(executable, &expected) != nil || unix.Fstatat(int(dir.Fd()), "exe", &actual, 0) != nil ||
		expected.Mode&unix.S_IFMT != unix.S_IFREG || expected.Mode&0111 == 0 || expected.Dev != actual.Dev || expected.Ino != actual.Ino {
		return ProcessIdentity{}, errProcess
	}
	raw, err = procRead(dir, "stat", 8192)
	if err != nil {
		return ProcessIdentity{}, errProcess
	}
	end, err := startTicks(raw, pid)
	if err != nil || start != end || unix.Fstatat(int(dir.Fd()), "exe", &after, 0) != nil || actual.Dev != after.Dev || actual.Ino != after.Ino {
		return ProcessIdentity{}, errProcess
	}
	return ProcessIdentity{PID: pid, StartTicks: start, BootID: bootID, Executable: executable, Device: uint64(actual.Dev), Inode: uint64(actual.Ino)}, nil
}

func CaptureProcess(pid int, executable string) (ProcessIdentity, error) {
	dir, err := openProcess(pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	defer dir.Close()
	return capture(dir, pid, executable)
}

// Linux renders each 32-bit address word in native byte order, including
// IPv6. Do not assume the little endian order used by the ARM64 test VM.
func procEndpoint(raw string) (netip.AddrPort, error) {
	addr, port, ok := strings.Cut(raw, ":")
	if !ok || len(port) != 4 || (len(addr) != 8 && len(addr) != 32) {
		return netip.AddrPort{}, errListener
	}
	b, err := hex.DecodeString(addr)
	if err != nil {
		return netip.AddrPort{}, errListener
	}
	for i := 0; i < len(b); i += 4 {
		word := binary.NativeEndian.Uint32(b[i : i+4])
		binary.BigEndian.PutUint32(b[i:i+4], word)
	}
	ip, ok := netip.AddrFromSlice(b)
	n, err := strconv.ParseUint(port, 16, 16)
	if !ok || err != nil || n == 0 {
		return netip.AddrPort{}, errListener
	}
	return netip.AddrPortFrom(ip.Unmap(), uint16(n)), nil
}

func listenerInode(dir *os.File, endpoint netip.AddrPort) (uint64, error) {
	netdir, err := platform.OpenDirAtNoFollow(dir, "net")
	if err != nil {
		return 0, errListener
	}
	defer netdir.Close()
	var found uint64
	for _, name := range []string{"tcp", "tcp6"} {
		table, err := procRead(netdir, name, 2<<20)
		if name == "tcp6" && endpoint.Addr().Is4() && errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return 0, errListener
		}
		for _, line := range bytes.Split(table, []byte{'\n'}) {
			f := strings.Fields(string(line))
			if len(f) < 4 || f[3] != "0A" {
				continue
			}
			if len(f) < 10 {
				return 0, errListener
			}
			bound, err := procEndpoint(f[1])
			if err != nil {
				return 0, errListener
			}
			if bound.Port() != endpoint.Port() {
				continue
			}
			address := bound.Addr()
			// A v6 wildcard might also accept v4. Its IPV6_V6ONLY setting
			// cannot be inferred here, so do not guess a v4 owner.
			if endpoint.Addr().Is4() && address.Is6() && address.IsUnspecified() {
				return 0, errListener
			}
			if address != endpoint.Addr() && !(address.IsUnspecified() && address.Is4() == endpoint.Addr().Is4()) {
				continue
			}
			inode, err := strconv.ParseUint(f[9], 10, 64)
			if err != nil || inode == 0 || found != 0 {
				return 0, errListener
			}
			found = inode
		}
	}
	if found == 0 {
		return 0, errListener
	}
	return found, nil
}

func ownsSocket(dir *os.File, inode uint64) bool {
	fds, err := platform.OpenDirAtNoFollow(dir, "fd")
	if err != nil {
		return false
	}
	defer fds.Close()
	names, err := fds.Readdirnames(4097)
	if (err != nil && err != io.EOF) || len(names) > 4096 {
		return false
	}
	want := "socket:[" + strconv.FormatUint(inode, 10) + "]"
	for _, name := range names {
		if _, err := strconv.ParseUint(name, 10, 32); err != nil {
			return false
		}
		buf := make([]byte, 256)
		n, err := unix.Readlinkat(int(fds.Fd()), name, buf)
		if errors.Is(err, unix.ENOENT) {
			continue
		} // another FD may close
		if err != nil {
			return false
		}
		if n < len(buf) && string(buf[:n]) == want {
			return true
		}
	}
	return false
}

func VerifyListener(expected ProcessIdentity, endpoint netip.AddrPort) (ListenerProof, error) {
	if !endpoint.IsValid() || endpoint.Port() == 0 || endpoint.Addr().IsUnspecified() || endpoint.Addr().Zone() != "" || endpoint.Addr().Is4In6() {
		return ListenerProof{}, errListener
	}
	dir, err := openProcess(expected.PID)
	if err != nil {
		return ListenerProof{}, err
	}
	defer dir.Close()
	var inode uint64
	for i := 0; i < 2; i++ {
		current, err := capture(dir, expected.PID, expected.Executable)
		if err != nil || current != expected {
			return ListenerProof{}, errProcess
		}
		observed, err := listenerInode(dir, endpoint)
		if err != nil || (i > 0 && inode != observed) || !ownsSocket(dir, observed) {
			return ListenerProof{}, errListener
		}
		inode = observed
	}
	current, err := capture(dir, expected.PID, expected.Executable)
	if err != nil || current != expected {
		return ListenerProof{}, errProcess
	}
	return ListenerProof{Process: expected, Endpoint: endpoint, SocketInode: inode}, nil
}
