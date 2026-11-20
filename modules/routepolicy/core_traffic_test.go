package routepolicy

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BROadmin/broray/modules/geodata"
)

func protoBytes(n uint64, value []byte) []byte {
	b := binary.AppendUvarint(nil, n<<3|2)
	b = binary.AppendUvarint(b, uint64(len(value)))
	return append(b, value...)
}

func syntheticCountry(t *testing.T, root string, ip net.IP) geodata.Snapshot {
	t.Helper()
	raw := ip.To16()
	bits := uint64(128)
	if v4 := ip.To4(); v4 != nil {
		raw = v4
		bits = 32
	}
	cidr := append(protoBytes(1, raw), binary.AppendUvarint([]byte{16}, bits)...)
	entry := append(protoBytes(1, []byte("FI")), protoBytes(2, cidr)...)
	data := protoBytes(1, entry)
	if err := geodata.Validate(geodata.GeoIP, data, []string{"fi"}); err != nil {
		t.Fatal(err)
	}
	generation := "gen-" + strings.Repeat("0", 32)
	if err := os.Mkdir(filepath.Join(root, generation), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := geodata.SourcePlan(geodata.Selection{CountryCodes: []string{"fi"}}, geodata.Revisions{GeoIP: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	rel := generation + "/" + plan[0].Name
	if err := os.WriteFile(filepath.Join(root, rel), data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return geodata.Snapshot{Generation: generation, Assets: []geodata.Asset{{Source: plan[0], Path: rel, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}}}
}

func dnsFixture(t *testing.T, ip net.IP) (int, *atomic.Int32) {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	queries := &atomic.Int32{}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 17 {
				continue
			}
			question := append([]byte(nil), buf[:n]...)
			end := 12
			for end < n && question[end] != 0 {
				size := int(question[end])
				if size > 63 || end+size+1 >= n {
					end = n
					break
				}
				end += size + 1
			}
			end++
			if end+4 > n {
				continue
			}
			kind := binary.BigEndian.Uint16(question[end : end+2])
			end += 4
			answer := append([]byte(nil), question[:end]...)
			answer[2], answer[3] = 0x81, 0x80
			answer[6], answer[7], answer[8], answer[9], answer[10], answer[11] = 0, 0, 0, 0, 0, 0
			var raw []byte
			if kind == 1 && ip.To4() != nil {
				raw = ip.To4()
			}
			if kind == 28 && ip.To4() == nil {
				raw = ip.To16()
			}
			if len(raw) > 0 {
				queries.Add(1)
				answer[7] = 1
				answer = append(answer, 0xc0, 0x0c, byte(kind>>8), byte(kind), 0, 1, 0, 0, 0, 10, 0, byte(len(raw)))
				answer = append(answer, raw...)
			}
			_, _ = conn.WriteTo(answer, addr)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port, queries
}

func startCore(t *testing.T, bin, root string, config map[string]any) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	inbound := config["inbounds"].([]any)[0].(map[string]any)
	inbound["listen"], inbound["port"] = "127.0.0.1", port
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "core.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(path + ".log")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-config", path)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+root)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	// Keep the child unreaped until cleanup: its PID cannot be recycled.
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		log.Close()
		if t.Failed() {
			b, _ := os.ReadFile(path + ".log")
			if len(b) > 8192 {
				b = b[len(b)-8192:]
			}
			t.Logf("core: %s", b)
		}
	})
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for i := 0; i < 50; i++ {
		c, err := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if err == nil {
			c.Close()
			return address
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("core did not start")
	return ""
}

func echoThroughSOCKS(proxy, domain string, port int) error {
	c, err := net.DialTimeout("tcp4", proxy, 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		return err
	}
	auth := make([]byte, 2)
	if _, err = io.ReadFull(c, auth); err != nil {
		return err
	}
	if auth[0] != 5 || auth[1] != 0 {
		return fmt.Errorf("SOCKS auth")
	}
	request := append([]byte{5, 1, 0, 3, byte(len(domain))}, []byte(domain)...)
	request = append(request, byte(port>>8), byte(port))
	if _, err = c.Write(request); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err = io.ReadFull(c, head); err != nil {
		return err
	}
	if head[0] != 5 || head[1] != 0 {
		return fmt.Errorf("SOCKS refused %v", head)
	}
	size := 0
	switch head[3] {
	case 1:
		size = 4
	case 4:
		size = 16
	case 3:
		b := []byte{0}
		if _, err = io.ReadFull(c, b); err != nil {
			return err
		}
		size = int(b[0])
	default:
		return fmt.Errorf("SOCKS address")
	}
	if _, err = io.ReadFull(c, make([]byte, size+2)); err != nil {
		return err
	}
	if _, err = c.Write([]byte("OK")); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(c, reply); err != nil {
		return err
	}
	if string(reply) != "OK" {
		return fmt.Errorf("unexpected payload")
	}
	return nil
}

func TestCoreDomainDirectVersusResolvedIPDeny(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("pinned actual core required")
	}
	for _, family := range []struct{ name, network, ip, strategy string }{{"A", "tcp4", "127.0.0.1", "UseIPv4"}, {"AAAA", "tcp6", "::1", "UseIPv6"}} {
		t.Run(family.name, func(t *testing.T) {
			root := t.TempDir()
			managed := filepath.Join(root, geodata.AssetDirectory)
			if err := os.Mkdir(managed, 0700); err != nil {
				t.Fatal(err)
			}
			snapshot := syntheticCountry(t, managed, net.ParseIP(family.ip))
			dnsPort, queries := dnsFixture(t, net.ParseIP(family.ip))
			target, err := net.Listen(family.network, net.JoinHostPort(family.ip, "0"))
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			connections := &atomic.Int32{}
			go func() {
				for {
					c, err := target.Accept()
					if err != nil {
						return
					}
					connections.Add(1)
					go func() { defer c.Close(); c.SetDeadline(time.Now().Add(3 * time.Second)); _, _ = io.Copy(c, c) }()
				}
			}()
			for _, blocked := range []bool{false, true} {
				t.Run(fmt.Sprintf("deny-%t", blocked), func(t *testing.T) {
					rules := []Rule{{ID: "direct", Enabled: true, Action: Direct, Domains: []string{"full:policy.example.test"}}, {ID: "country", Enabled: blocked, Action: Deny, CountryCodes: []string{"fi"}}}
					config := map[string]any{"log": map[string]any{"loglevel": "warning"}, "dns": map[string]any{"tag": "policy-test-dns", "queryStrategy": family.strategy, "servers": []any{map[string]any{"address": "127.0.0.1", "port": dnsPort}}}, "inbounds": []any{map[string]any{"tag": "socks", "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}}, "outbounds": []any{map[string]any{"tag": "proxy", "protocol": "blackhole"}}}
					base, err := json.Marshal(config)
					if err != nil {
						t.Fatal(err)
					}
					rendered, err := Render(base, rules, snapshot, geodata.AssetDirectory, testTags, []string{"socks"}, "lo")
					if err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(rendered, &config); err != nil {
						t.Fatal(err)
					}
					// Test-only DNS plumbing; production policy never introduces this rule.
					routing := config["routing"].(map[string]any)
					routing["rules"] = append([]any{map[string]any{"type": "field", "inboundTag": []string{"policy-test-dns"}, "outboundTag": "direct"}}, routing["rules"].([]any)...)
					for _, item := range config["outbounds"].([]any) {
						out := item.(map[string]any)
						if out["tag"] == "direct" {
							out["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.0/8", "::1/128"}}}}
						}
					}
					proxy := startCore(t, bin, root, config)
					before := connections.Load()
					dnsBefore := queries.Load()
					err = echoThroughSOCKS(proxy, "policy.example.test", target.Addr().(*net.TCPAddr).Port)
					if !blocked && err != nil {
						t.Fatalf("DIRECT control did not deliver traffic: %v", err)
					}
					if blocked && (err == nil || connections.Load() != before) {
						t.Fatalf("IP deny bypassed by domain DIRECT: payloadError=%v connections=%d", err, connections.Load()-before)
					}
					if queries.Load() == dnsBefore {
						t.Fatal("no matching DNS A/AAAA lookup observed")
					}
					t.Logf("family=%s deny=%t targetConnections=%d DNSqueries=%d", family.name, blocked, connections.Load()-before, queries.Load()-dnsBefore)
				})
			}
		})
	}
}
