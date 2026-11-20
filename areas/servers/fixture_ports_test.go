package servers

import (
	"fmt"
	"os"
	"testing"
)

func TestFixtureTCPPortsAvoidAutomaticAllocation(t *testing.T) {
	raw, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		t.Fatal(err)
	}
	var low, high int
	if n, err := fmt.Sscanf(string(raw), "%d %d", &low, &high); err != nil || n != 2 || low < 1 || high > 65535 || low > high {
		t.Fatal("invalid ephemeral range", err)
	}
	seen := map[int]bool{}
	for i := 0; i < 64; i++ {
		port := freeTCP(t)
		if port >= low && port <= high {
			t.Fatalf("fixture port %d can be stolen by automatic listener/client allocation in %d..%d", port, low, high)
		}
		if seen[port] {
			t.Fatal("released fixture port reused", port)
		}
		seen[port] = true
	}
}
