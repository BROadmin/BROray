package routepolicy

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/BROadmin/broray/modules/geodata"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoreDirectInterfaceBinding(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("pinned actual core required")
	}
	for _, family := range []struct{ name, host string }{{"IPv4", "127.0.0.1"}, {"IPv6", "::1"}} {
		for _, binding := range []string{"lo", "br359-absent"} {
			t.Run(family.name+"/"+binding, func(t *testing.T) {
				target, err := net.Listen("tcp", net.JoinHostPort(family.host, "0"))
				if err != nil {
					t.Fatal(err)
				}
				defer target.Close()
				var accepted atomic.Int32
				go func() {
					for {
						c, e := target.Accept()
						if e != nil {
							return
						}
						accepted.Add(1)
						go func() { defer c.Close(); c.SetDeadline(time.Now().Add(2 * time.Second)); io.Copy(c, c) }()
					}
				}()
				udp, err := net.ListenPacket("udp", net.JoinHostPort(family.host, "0"))
				if err != nil {
					t.Fatal(err)
				}
				defer udp.Close()
				var packets atomic.Int32
				go func() {
					b := make([]byte, 512)
					for {
						n, a, e := udp.ReadFrom(b)
						if e != nil {
							return
						}
						packets.Add(1)
						udp.WriteTo(b[:n], a)
					}
				}()
				base := []byte(`{"inbounds":[{"tag":"socks","protocol":"socks","settings":{"auth":"noauth","udp":true}}],"outbounds":[{"tag":"proxy","protocol":"blackhole"}]}`)
				raw, err := Render(base, []Rule{{ID: "direct", Enabled: true, Action: Direct, Domains: []string{"full:binding.example.test"}}}, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, binding)
				if err != nil {
					t.Fatal(err)
				}
				var cfg map[string]any
				json.Unmarshal(raw, &cfg)
				cfg["dns"] = map[string]any{"hosts": map[string]any{"binding.example.test": family.host}}
				proxy := startCore(t, bin, t.TempDir(), cfg)
				tcpErr := echoThroughSOCKS(proxy, "binding.example.test", target.Addr().(*net.TCPAddr).Port)
				udpErr := bindingUDPThroughSOCKS(proxy, "binding.example.test", udp.LocalAddr().(*net.UDPAddr).Port)
				if binding == "lo" {
					if tcpErr != nil || udpErr != nil || accepted.Load() != 1 || packets.Load() != 1 {
						t.Fatalf("positive binding control: tcp=%v udp=%v accepts=%d packets=%d", tcpErr, udpErr, accepted.Load(), packets.Load())
					}
				} else if tcpErr == nil || udpErr == nil || accepted.Load() != 0 || packets.Load() != 0 {
					t.Fatalf("missing binding leaked: tcp=%v udp=%v accepts=%d packets=%d", tcpErr, udpErr, accepted.Load(), packets.Load())
				}
			})
		}
	}
}

func bindingUDPThroughSOCKS(proxy, domain string, port int) error {
	c, e := net.DialTimeout("tcp", proxy, 2*time.Second)
	if e != nil {
		return e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, e = c.Write([]byte{5, 1, 0}); e != nil {
		return e
	}
	auth := make([]byte, 2)
	if _, e = io.ReadFull(c, auth); e != nil {
		return e
	}
	if auth[0] != 5 || auth[1] != 0 {
		return fmt.Errorf("SOCKS UDP auth")
	}
	if _, e = c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); e != nil {
		return e
	}
	head := make([]byte, 4)
	if _, e = io.ReadFull(c, head); e != nil {
		return e
	}
	if head[0] != 5 || head[1] != 0 {
		return fmt.Errorf("SOCKS UDP refusal")
	}
	size := 4
	if head[3] == 4 {
		size = 16
	} else if head[3] != 1 {
		return fmt.Errorf("SOCKS UDP address")
	}
	address := make([]byte, size+2)
	if _, e = io.ReadFull(c, address); e != nil {
		return e
	}
	endpoint := &net.UDPAddr{IP: net.IP(address[:size]), Port: int(binary.BigEndian.Uint16(address[size:]))}
	u, e := net.DialUDP("udp", nil, endpoint)
	if e != nil {
		return e
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(time.Second))
	packet := append([]byte{0, 0, 0, 3, byte(len(domain))}, []byte(domain)...)
	packet = append(packet, byte(port>>8), byte(port))
	packet = append(packet, []byte("UDP-BOUND")...)
	if _, e = u.Write(packet); e != nil {
		return e
	}
	reply := make([]byte, 512)
	n, e := u.Read(reply)
	if e != nil {
		return e
	}
	if n < 9 || string(reply[n-9:n]) != "UDP-BOUND" {
		return fmt.Errorf("SOCKS UDP echo mismatch")
	}
	return nil
}
