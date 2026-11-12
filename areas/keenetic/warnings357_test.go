package keenetic

import (
	"strings"
	"testing"
)

func TestWarnings357UDPUnconfirmed(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		r := &udpCheckResult{}
		r.Server.OK = proxy
		r.Proxy.Ran = proxy
		r.Socks5UDP = true
		udpVerdict(r)
		if !strings.Contains(r.Message, "Не удалось подтвердить") || strings.Contains(r.Message, "не передаёт") {
			t.Fatal("finite probe asserts universal capability", r.Message)
		}
	}
}
