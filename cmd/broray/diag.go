package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/BROadmin/broray/internal/udpprobe"
)

// runDiag: `broray diag udp -routed H:P [-direct H:P] [-socks H:P]` — a
// read-only STUN probe; it changes nothing in Keenetic, Xray or BROray.
func runDiag(args []string) int {
	if len(args) == 0 || args[0] != "udp" {
		fmt.Fprintln(os.Stderr, "usage: broray diag udp -routed HOST:PORT [-direct HOST:PORT] [-socks HOST:PORT]")
		return 2
	}
	fs := flag.NewFlagSet("diag udp", flag.ContinueOnError)
	routed := fs.String("routed", "", "STUN host:port routed to ProxyN by BROray")
	direct := fs.String("direct", "", "STUN host:port NOT routed (WAN)")
	socks := fs.String("socks", "", "Xray SOCKS5 host:port")
	timeout := fs.Duration("timeout", 5*time.Second, "per-probe timeout")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	rep := udpprobe.Run(*routed, *direct, *socks, *timeout)
	printJSON(rep)
	if rep.Verdict != "UDP_VIA_BROray" {
		return 1
	}
	return 0
}
