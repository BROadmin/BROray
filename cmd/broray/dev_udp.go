//go:build dev

package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	akeenetic "github.com/BROadmin/broray/areas/keenetic"
	"github.com/BROadmin/broray/internal/udpprobe"
)

// Dev builds: «Проверить UDP» without Internet STUN servers.
//
//	BRORAY_DEV_STUN=local            in-process STUN responders on 127.0.0.10/11
//	                                 (routed candidates) and 127.0.0.20 (direct)
//	BRORAY_DEV_STUN_PROXYN=<socks>   emulate Keenetic routing against tools/fakerci:
//	                                 a datagram to an address with
//	                                 `ip route <ip> 255.255.255.255 ProxyN …` in the
//	                                 fake running-config is relayed through the Xray
//	                                 SOCKS <socks> (ProxyN → SOCKS5 UDP → Xray)
//	BRORAY_DEV_RCI                   fakerci base URL (default http://127.0.0.1:17979)
//
// Router builds never contain this file.
func init() {
	if os.Getenv("BRORAY_DEV_STUN") == "local" {
		var routed, direct []string
		for _, ip := range []string{"127.0.0.10", "127.0.0.11", "127.0.0.20"} {
			pc, err := net.ListenPacket("udp4", ip+":0")
			if err != nil {
				log.Printf("dev STUN %s: %v", ip, err)
				continue
			}
			go udpprobe.Responder(pc)
			if ip == "127.0.0.20" {
				direct = append(direct, pc.LocalAddr().String())
			} else {
				routed = append(routed, pc.LocalAddr().String())
			}
		}
		akeenetic.STUNRoutedCandidates, akeenetic.STUNDirectCandidates, akeenetic.STUNAllowLocal = routed, direct, true
	}
	socks := os.Getenv("BRORAY_DEV_STUN_PROXYN")
	if socks == "" {
		return
	}
	rci := os.Getenv("BRORAY_DEV_RCI")
	if rci == "" {
		rci = "http://127.0.0.1:17979"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	akeenetic.STUNDirect = func(server string, timeout time.Duration) (*net.UDPAddr, error) {
		host, _, _ := net.SplitHostPort(server)
		if resp, err := client.Get(rci + "/rci/show/running-config"); err == nil {
			var rc struct {
				Message []string `json:"message"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&rc)
			resp.Body.Close()
			for _, l := range rc.Message {
				f := strings.Fields(l)
				if len(f) >= 5 && f[0] == "ip" && f[1] == "route" && f[2] == host && f[3] == "255.255.255.255" && strings.HasPrefix(f[4], "Proxy") {
					return udpprobe.ViaSOCKS5(socks, server, timeout)
				}
			}
		}
		return udpprobe.Direct(server, timeout)
	}
}
