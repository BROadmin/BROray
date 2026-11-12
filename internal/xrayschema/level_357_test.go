package xrayschema

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestFullLevel357Classifier(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "http", "socks", "trojan", "shadowsocks"} {
		maximum := "4294967295"
		if protocol == "trojan" || protocol == "shadowsocks" {
			maximum = "255"
		}
		for _, level := range []string{"0", "1", "8", maximum} {
			for _, legacy := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/legacy-%t/%s", protocol, legacy, level), func(t *testing.T) {
					settings := `{"level":` + level + `}`
					if legacy {
						switch protocol {
						case "vless", "vmess":
							settings = `{"vnext":[{"users":[` + settings + `]}]}`
						case "http", "socks":
							settings = `{"servers":[{"users":[` + settings + `]}]}`
						default:
							settings = `{"servers":[` + settings + `]}`
						}
					}
					input := object(t, `{"protocol":"`+protocol+`","settings":`+settings+`}`)
					a, err := Classify(input)
					if err != nil || len(a.Reasons) != 0 {
						t.Fatal("valid level incorrectly requires provider policy")
					}
					want, _ := json.Marshal(input)
					got, _ := json.Marshal(a.Projection)
					if string(want) != string(got) {
						t.Fatal("classifier dropped explicit account level")
					}
				})
			}
		}
	}
}
