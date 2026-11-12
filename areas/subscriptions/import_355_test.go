package subscriptions

import (
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func TestYour355JSONAndURI(t *testing.T) {
	for _, level := range []string{"0", "8", "255", "256", "4294967295", "-1", "4294967296", "0.5", `"8"`, "true", "null"} {
		t.Run(level, func(t *testing.T) {
			body := `{"outbounds":[{"protocol":"vless","settings":{"address":"proxy.example.test","port":443,"id":"00000000-0000-4000-8000-000000000001","encryption":"none","level":` + level + `},"streamSettings":{"network":"xhttp","security":"none","xhttpSettings":{"path":"/test","extra":{"xmux":{"maxConcurrency":"","maxConnections":"1"}}}}}]}`
			ex, err := extractNodes([]byte(body), newMetadata())
			if err != nil {
				t.Fatal(err)
			}
			st, serr := stageNodes("sub-test", ex, true, nil)
			valid := level == "0" || level == "8" || level == "255" || level == "256" || level == "4294967295"
			if (serr == nil) != valid {
				t.Fatalf("valid=%v accepted=%d warnings=%v err=%v", valid, st.Accepted, st.Warnings, serr)
			}
			if valid {
				r := st.Records[0]
				if r["uuid"] != "00000000-0000-4000-8000-000000000001" || r["network"] != "xhttp" || fmt.Sprint(r["accountLevel"]) != level {
					t.Fatal("endpoint or account level lost")
				}
				if _, err := servers.Outbound(r); err != nil {
					t.Fatal("valid account level disabled under BROray's local core policy")
				}
				if !strings.Contains(strings.ToLower(strings.Join(st.Warnings, " ")), "политик") {
					t.Fatal("missing policy warning")
				}
			}
		})
	}
	for _, v := range []string{`""`, `"0"`, `0`, `"-1"`, `"2147483648"`, `true`, `null`, `0.5`, `"1-0"`} {
		link := `vless://00000000-0000-4000-8000-000000000001@proxy.example.test:443?type=xhttp&security=none&extra=` + url.QueryEscape(`{"xmux":{"maxConcurrency":`+v+`,"maxConnections":"1"}}`)
		ex, err := extractNodes([]byte(link), newMetadata())
		if err != nil {
			t.Fatal(err)
		}
		st, serr := stageNodes("sub-test", ex, true, nil)
		valid := v == `""` || v == `"0"` || v == `0`
		if (serr == nil) != valid {
			t.Errorf("URI %s valid=%v accepted=%d warnings=%v", v, valid, st.Accepted, st.Warnings)
		}
	}
}

func TestYour355FailedUpdate(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA)
	id := e.create(t, p.URL, "")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	before := liveIDs(t, e)
	if len(before) != 1 {
		t.Fatal(before)
	}
	oldBytes, err := os.ReadFile(servers.RecordPath(e.d, before[0]))
	if err != nil {
		t.Fatal(err)
	}
	p.set("tuic://synthetic@proxy.example.test:443")
	if err := e.refreshSync(t, id); err == nil {
		t.Fatal("expected failed import")
	}
	result := e.record(t, id)["lastUpdateResult"].(map[string]any)
	if !numIs(result["received"], 1) || !numIs(result["rejected"], 1) || !numIs(result["accepted"], 0) || !numIs(result["parsed"], 0) {
		t.Fatalf("lost counts: %v", result)
	}
	if !strings.Contains(strings.ToLower(fmt.Sprint(result["warnings"])), "tuic") {
		t.Fatalf("lost reason: %v", result)
	}
	if !reflect.DeepEqual(before, liveIDs(t, e)) {
		t.Fatal("saved catalog changed")
	}
	newBytes, err := os.ReadFile(servers.RecordPath(e.d, before[0]))
	if err != nil || !reflect.DeepEqual(oldBytes, newBytes) {
		t.Fatal("saved catalog bytes changed", err)
	}
	p.set(`trojan://pw@0.0.0.0:1#AppNotSupported`)
	if err := e.refreshSync(t, id); err == nil {
		t.Fatal("expected provider denial")
	}
	result = e.record(t, id)["lastUpdateResult"].(map[string]any)
	if !numIs(result["parsed"], 1) || !numIs(result["received"], 1) || !numIs(result["rejected"], 1) {
		t.Fatalf("lost parsed count: %v", result)
	}
	p.setStatus(503)
	if err := e.refreshSync(t, id); err == nil {
		t.Fatal("expected fetch failure")
	}
	result = e.record(t, id)["lastUpdateResult"].(map[string]any)
	for _, key := range []string{"received", "parsed", "accepted", "rejected"} {
		if !numIs(result[key], 0) {
			t.Fatalf("fetch failure count %s: %v", key, result)
		}
	}
	if len(result["warnings"].([]any)) != 0 {
		t.Fatal("fetch failure retained stale warnings")
	}
}

func TestYour355ProtocolLevelBounds(t *testing.T) {
	for _, protocol := range []string{"vmess", "trojan", "shadowsocks", "http", "socks"} {
		for _, level := range []string{"8", "255", "256", "4294967295", "4294967296", `"8"`, "-1", "0.5", "null", "true"} {
			t.Run(protocol+"/"+level, func(t *testing.T) {
				credential := `"password":"synthetic-password"`
				switch protocol {
				case "vmess":
					credential = `"id":"00000000-0000-4000-8000-000000000001","security":"auto"`
				case "shadowsocks":
					credential += `,"method":"aes-128-gcm"`
				case "http", "socks":
					credential = `"user":"synthetic","pass":"synthetic-password"`
				}
				stream := ""
				if protocol == "trojan" {
					stream = `,"streamSettings":{"security":"tls","tlsSettings":{"serverName":"proxy.example.test"}}`
				}
				body := `{"outbounds":[{"protocol":"` + protocol + `","settings":{"address":"proxy.example.test","port":443,` + credential + `,"level":` + level + `}` + stream + `}]}`
				ex, err := extractNodes([]byte(body), newMetadata())
				if err != nil {
					t.Fatal(err)
				}
				st, serr := stageNodes("sub-test", ex, true, nil)
				valid := level == "8" || level == "255" || ((protocol == "vmess" || protocol == "http" || protocol == "socks") && (level == "256" || level == "4294967295"))
				if (serr == nil) != valid {
					t.Fatalf("valid=%v accepted=%d warnings=%v err=%v", valid, st.Accepted, st.Warnings, serr)
				}
			})
		}
	}
}

func TestYour355JSONRangeBounds(t *testing.T) {
	for _, value := range []any{"", "0", "1-2", float64(0), "-1", "1-0", "2147483648", float64(-1), float64(0.5), nil, true} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			valid := value == "" || value == "0" || value == "1-2" || value == float64(0)
			defer func() {
				if panicked := recover() != nil; panicked == valid {
					t.Errorf("range validity=%v panic=%v", valid, panicked)
				}
			}()
			rangePair(value)
		})
	}
}
