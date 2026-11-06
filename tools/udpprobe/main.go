// udpprobe is the standalone client-side UDP path check (same code as
// `broray diag udp`), for a LAN computer behind the Keenetic:
//
//	udpprobe -routed 74.125.250.129:19302 -direct 162.159.207.0:3478
//
// -routed must be a STUN server whose /32 is in a BROray route set, -direct
// one that is not. Prints JSON; exit 0 only for verdict UDP_VIA_BROray.
package main

import (
	"encoding/json"
	"flag"
	"os"
	"time"

	"github.com/BROadmin/broray/internal/udpprobe"
)

func main() {
	routed := flag.String("routed", "", "STUN host:port routed to ProxyN by BROray")
	direct := flag.String("direct", "", "STUN host:port NOT routed (WAN)")
	socks := flag.String("socks", "", "optional Xray SOCKS5 host:port (router LAN IP:2080)")
	timeout := flag.Duration("timeout", 5*time.Second, "per-probe timeout")
	flag.Parse()
	rep := udpprobe.Run(*routed, *direct, *socks, *timeout)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rep)
	if rep.Verdict != "UDP_VIA_BROray" {
		os.Exit(1)
	}
}
