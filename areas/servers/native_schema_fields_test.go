package servers

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func nativeCoverageFixture(t *testing.T, kind string) map[string]any {
	o := nativeBackendFixture(t, kind)
	st := o["streamSettings"].(map[string]any)
	st["sockopt"] = map[string]any{"tcpFastOpen": false, "tcpKeepAliveIdle": 0, "tcpKeepAliveInterval": 30, "domainStrategy": "UseIPv4"}
	block := "xdriveSettings"
	if strings.HasPrefix(kind, "masque") {
		block = "masqueSettings"
		o["settings"].(map[string]any)["remoteDNS"] = []any{"192.0.2.53", "2001:db8::53"}
	}
	m := st[block].(map[string]any)
	m["xmux"] = map[string]any{"maxConcurrency": 0, "maxConnections": "1-2", "cMaxReuseTimes": 3, "hMaxRequestTimes": "4-5", "hMaxReusableSecs": "6", "hKeepAlivePeriod": -1}
	if kind == "template" {
		x := m["template"].(map[string]any)
		x["flatten"] = true
		x["concurrency"] = 17
		x["auth"] = map[string]any{"type": "oauth2", "tokenUrl": "https://storage.example.test/token", "form": map[string]any{"refresh_token": "synthetic-private-refresh"}, "header": map[string]any{"Authorization": "Bearer {token}"}, "tokenPath": "response.token", "expiryPath": "response.expires"}
		for _, op := range []string{"put", "get", "delete", "list"} {
			q := x[op].(map[string]any)
			q["headers"] = map[string]any{"X-Token": "synthetic-private-header"}
			q["hrefPath"] = "response.href"
			q["hrefMethod"] = "POST"
		}
		x["put"].(map[string]any)["body"] = `{"data":"{data}","secret":"{secret0}"}`
	}
	return o
}

// One type mutation per actual nested field of each backend. This schema corpus
// does not imply provider traffic or all combinations of numeric boundaries.
func TestNativeClientFieldTypes(t *testing.T) {
	for _, kind := range []string{"google-drive", "s3", "template", "local", "masque", "masque-warp"} {
		base := nativeCoverageFixture(t, kind)
		t.Run(kind+"/positive", func(t *testing.T) { assertNativeUnchanged(t, base) })
		var visit func(any, []string)
		visit = func(current any, path []string) {
			children := map[string]any{}
			switch v := current.(type) {
			case map[string]any:
				children = v
			case []any:
				for i, value := range v {
					children[strconv.Itoa(i)] = value
				}
			}
			for _, key := range sortedKeys(children) {
				value := children[key]
				p := append(append([]string{}, path...), key)
				t.Run(kind+"/"+strings.Join(p, "."), func(t *testing.T) {
					copy := normalize(base)
					var parent any = copy
					for _, k := range p[:len(p)-1] {
						switch v := parent.(type) {
						case map[string]any:
							parent = v[k]
						case []any:
							i, _ := strconv.Atoi(k)
							parent = v[i]
						default:
							t.Fatal("invalid fixture path")
						}
					}
					bad := any(map[string]any{"synthetic-private-invalid": true})
					if _, ok := value.(map[string]any); ok {
						bad = []any{"synthetic-private-invalid"}
					}
					switch v := parent.(type) {
					case map[string]any:
						v[key] = bad
					case []any:
						i, _ := strconv.Atoi(key)
						v[i] = bad
					default:
						t.Fatal("invalid fixture leaf")
					}
					b, _ := json.Marshal(copy)
					_, err := ParseLink(string(b), "coverage", nil)
					if err == nil {
						t.Fatal("wrong type accepted for client field")
					}
					if err != nil && strings.Contains(err.Error(), "synthetic-private") {
						t.Fatal("error leaked a private value")
					}
				})
				visit(value, p)
			}
		}
		visit(base, nil)
	}
}
