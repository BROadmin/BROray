package xrayschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyFilterSource357(t *testing.T) {
	for _, lane := range []string{"tcp", "udp"} {
		for _, settings := range []string{`{"blockedSourceIPs":["192.0.2.0/24"],"allowedSourceIPs":["2001:db8::/32","198.51.100.4"]}`, `{"blockedSourceIPs":[],"allowedSourceIPs":[]}`} {
			input := object(t, `{"protocol":"vless","streamSettings":{"finalmask":{"`+lane+`":[{"type":"filter","settings":`+settings+`}]}}}`)
			a, err := Classify(input)
			if err != nil || len(a.Reasons) != 1 || a.Reasons[0].Kind != "listener-only" {
				t.Fatal("known filter source needs explicit listener-only disposition")
			}
			original, _ := json.Marshal(a.Source.Fields)
			want, _ := json.Marshal(input)
			if string(original) != string(want) {
				t.Fatal("filter source reduced")
			}
			projection, _ := json.Marshal(a.Projection)
			if strings.Contains(string(projection), `"type":"filter"`) {
				t.Fatal("client-ineffective mask remained in validation projection")
			}
		}
	}
	for _, value := range []string{`true`, `null`, `[1]`, `["synthetic-private-invalid"]`, `["192.0.2.1/999"]`, `["2001:db8::/129"]`, `["[192.0.2.1"]`, `["ext:file:"]`, `["geoip:!"]`} {
		input := object(t, `{"protocol":"vless","streamSettings":{"finalmask":{"tcp":[{"type":"filter","settings":{"blockedSourceIPs":`+value+`}}]}}}`)
		if _, err := Classify(input); err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("detachment hid malformed IP rule or disclosed input")
		}
	}
	for _, rule := range []string{"!192.0.2.0/24", "[2001:db8::1]/128", "192.0.2.1/", "geoip:private", "ext:/never-read/synthetic-private.dat:!REGION", "ext-ip:private.dat:REGION"} {
		input := object(t, `{"protocol":"vless","streamSettings":{"finalmask":{"udp":[{"type":"filter","settings":{"allowedSourceIPs":["`+rule+`"]}}]}}}`)
		a, err := Classify(input)
		if err != nil || len(a.Reasons) == 0 {
			t.Fatal("valid pinned IP rule syntax rejected")
		}
		for _, reason := range a.Reasons {
			if strings.Contains(reason.Path, "synthetic-private") {
				t.Fatal("resource reference disclosed")
			}
		}
	}
}

func TestClassifyRawUserMetadata357(t *testing.T) {
	for _, protocol := range []string{"http", "socks"} {
		for _, metadata := range []string{`"level":"0"`, `"level":null`, `"level":-1`, `"level":4294967296`, `"level":1,"email":null`, `"level":1,"email":7`} {
			input := object(t, `{"protocol":"`+protocol+`","settings":{"servers":[{"address":"example.test","port":443,"users":[{"user":"synthetic-private-user","pass":"synthetic-private-pass",`+metadata+`}]}]}}`)
			if _, err := Classify(input); err == nil || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("malformed raw user metadata was detached instead of rejected")
			}
		}
		for _, level := range []string{"0", "1", "4294967295"} {
			input := object(t, `{"protocol":"`+protocol+`","settings":{"servers":[{"address":"example.test","port":443,"users":[{"user":"synthetic-private-user","pass":"synthetic-private-pass","level":`+level+`,"email":""}]}]}}`)
			a, err := Classify(input)
			if err != nil || len(a.Reasons) != 0 {
				t.Fatal("valid raw user policy metadata classification changed")
			}
		}
	}
}
