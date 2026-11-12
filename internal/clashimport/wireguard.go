package clashimport

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/wireguardimport"
)

func wireGuard(source map[string]any) string {
	profile, err := wireguardimport.Parse(source)
	need(err == nil)
	ini := profile.INI()
	if profile.AWG == nil {
		extensions, err := json.Marshal(profile.Extension())
		need(err == nil)
		return "# BROray name: " + profile.Name + "\n" +
			wireguardimport.ExtensionComment + base64.StdEncoding.EncodeToString(extensions) + "\n" + ini
	}
	var options strings.Builder
	keys := make([]string, 0, len(profile.AWG))
	for k := range profile.AWG {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		name, known := amnezia.ClashInterfaceField(k)
		if !known {
			continue // Unknown bounded options remain in the private source.
		}
		value := profile.AWG[k]
		switch value.(type) {
		case string, json.Number:
		case bool:
			need(k == "random-trailers" || k == "disable-cookies")
		default:
			need(false)
		}
		fmt.Fprintf(&options, "%s = %v\n", name, value)
	}
	ini = strings.Replace(ini, "[Peer]\n", options.String()+"[Peer]\n", 1)
	client, err := json.Marshal(map[string]any{"config": ini})
	need(err == nil)
	root := map[string]any{"description": profile.Name, "containers": []any{
		map[string]any{"container": "amnezia-awg", "awg": map[string]any{
			"last_config": string(client), "sourceFormat": "clash-yaml", "clashOptions": profile.Source,
		}},
	}}
	encoded, err := json.Marshal(root)
	need(err == nil)
	return "vpn://" + base64.StdEncoding.EncodeToString(encoded)
}
