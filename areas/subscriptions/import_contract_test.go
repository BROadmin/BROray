package subscriptions

import (
	"encoding/json"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func TestXHTTPJSONSignedKeepAlive(t *testing.T) {
	for _, v := range []string{"-1", "0", "9223372036854775807"} {
		t.Run(v, func(t *testing.T) {
			body := `{"outbounds":[{"protocol":"vless","settings":{"address":"example.test","port":443,"id":"test","encryption":"none"},"streamSettings":{"network":"xhttp","xhttpSettings":{"extra":{"xmux":{"hKeepAlivePeriod":` + v + `}}}}}]}`
			lines, ok := xrayJSONToLinks([]byte(body), 10)
			if !ok || len(lines) != 1 || lines[0] == unsupportedMarker {
				t.Fatal("signed keepalive rejected in JSON conversion")
			}
			r, e := servers.ParseLink(lines[0], "test", nil)
			if e != nil {
				t.Fatal(e)
			}
			out, e := servers.Outbound(r)
			if e != nil {
				t.Fatal(e)
			}
			x := out["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)["extra"].(map[string]any)["xmux"].(map[string]any)["hKeepAlivePeriod"]
			b, _ := json.Marshal(x)
			if string(b) != v {
				t.Fatalf("signed value changed: %s", b)
			}
		})
	}
}
