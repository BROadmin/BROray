// fakerci imitates the KeeneticOS RCI endpoint (127.0.0.1:79 on a router) for
// host tests: GET /rci/show/running-config, /rci/more/startup-config,
// /rci/show/interface/<X>, /rci/show/ndns, /rci/show/version, /rci/show/ip/route,
// /rci/show/ipv6/addresses, /rci/show/ipv6/prefixes, and POST /rci/ with
// [{"parse":"<cli>"}]. GET /log returns every parsed command. POST
// /ipv6?enabled=1 (or FAKERCI_IPV6=1 at start) gives the router a global
// IPv6 address on the WAN and a delegated prefix on Bridge0; enabled=0 leaves
// only link-local addresses (BROray must not warn). POST /firmware?udp=0
// (or FAKERCI_NO_SOCKS5_UDP=1 at start) emulates KeeneticOS before 4.1:
// "proxy socks5-udp" is an unknown command and "show version" reports
// 4.0.5; udp=1 switches back to 5.02.
//
//	go run ./tools/fakerci :17979
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
)

var (
	mu      sync.Mutex
	blocks  = map[string][]string{}
	startup []string
	cmdLog  []string
	ipv6On  = os.Getenv("FAKERCI_IPV6") == "1"
	noUDP   = os.Getenv("FAKERCI_NO_SOCKS5_UDP") == "1"
)

func release() string {
	if noUDP {
		return "4.0.5"
	}
	return "5.02.B.2.0-2"
}

// IPv6 answers in the assumed RCI shape (see internal/keenetic/ipv6_test.go).
func ipv6Addresses() any {
	list := []map[string]string{
		{"address": "fe80::52ff:20ff:fe00:1", "interface": "GigabitEthernet1", "valid-lifetime": "infinite"},
		{"address": "fe80::1", "interface": "Bridge0", "valid-lifetime": "infinite"},
	}
	if ipv6On {
		list = append(list,
			map[string]string{"address": "2a02:2698:a002:1d::5", "interface": "GigabitEthernet1", "valid-lifetime": "86400"},
			map[string]string{"address": "2a02:2698:a400:7e01::1", "interface": "Bridge0", "valid-lifetime": "86400"})
	}
	return map[string]any{"address": list}
}

func ipv6Prefixes() any {
	if !ipv6On {
		return map[string]any{}
	}
	return map[string]any{"prefix": []map[string]string{{"prefix": "2a02:2698:a400:7e00::/56", "interface": "GigabitEthernet1", "valid-lifetime": "86400"}}}
}

var header = []string{"! $$$ Model: Keenetic Ultra", "! $$$ Version: 5.02.B.2.0-2"}
var base = []string{"interface Bridge0", "    ip address 192.168.1.1 255.255.255.0", "    up", "!", "interface Wireguard0", "    description \"owner vpn\"", "    up", "!"}

func render() []string {
	out := append([]string{}, header...)
	out = append(out, base...)
	names := make([]string, 0, len(blocks))
	for n := range blocks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.HasPrefix(n, "interface ") {
			out = append(out, n)
			out = append(out, ordered(blocks[n])...)
			out = append(out, "!")
		}
	}
	for _, n := range names {
		if strings.HasPrefix(n, "ip route ") {
			out = append(out, n)
		}
	}
	out = append(out, "dns-proxy", "    tls upstream 94.140.14.14 sni dns.adguard-dns.com")
	for _, n := range names {
		if strings.HasPrefix(n, "dns-proxy tls upstream ") {
			out = append(out, "    "+strings.TrimPrefix(n, "dns-proxy "))
		}
	}
	out = append(out, "!", "ip http proxy other", "    upstream http 192.168.1.1 9999", "!")
	for _, n := range names {
		if strings.HasPrefix(n, "ip http proxy ") {
			out = append(out, n)
			out = append(out, blocks[n]...)
			out = append(out, "!")
		}
	}
	return out
}

// ordered renders an interface body in KeeneticOS order (as on KN-1811):
// description, security-level, ip global, proxy protocol, proxy upstream,
// proxy connect via, up. Plain "proxy connect" is an action, not stored.
func ordered(body []string) []string {
	rank := func(l string) int {
		t := strings.TrimSpace(l)
		for i, p := range []string{"description", "security-level", "ip global", "proxy protocol", "proxy upstream", "proxy socks5-udp", "proxy connect via", "up"} {
			if t == p || strings.HasPrefix(t, p+" ") {
				return i
			}
		}
		return 99
	}
	out := []string{}
	for _, l := range body {
		if strings.TrimSpace(l) != "proxy connect" {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

func replaceLine(body []string, prefix, line string) []string {
	for i, l := range body {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			body[i] = "    " + line
			return body
		}
	}
	return append(body, "    "+line)
}

// failOn holds commands that fail (POST /fail?cmd=<command>, DELETE /fail clears).
var failOn = map[string]bool{}

func parse(cmd string) (string, string) {
	cmdLog = append(cmdLog, cmd)
	if failOn[cmd] {
		return "error", "injected failure"
	}
	f := strings.Fields(cmd)
	switch {
	case cmd == "system configuration save":
		startup = render()
		return "message", "saving (http/rci)."
	case strings.HasPrefix(cmd, "no interface "):
		delete(blocks, "interface "+f[2])
		return "message", "interface removed."
	case strings.HasPrefix(cmd, "interface "):
		key := "interface " + f[1]
		if len(f) == 2 {
			if _, ok := blocks[key]; !ok {
				blocks[key] = []string{}
			}
			return "message", "created"
		}
		body, ok := blocks[key]
		if !ok {
			return "error", "no such interface"
		}
		rest := strings.Join(f[2:], " ")
		if noUDP && (rest == "proxy socks5-udp" || rest == "no proxy socks5-udp") {
			return "error", "no such command: socks5-udp"
		}
		switch {
		case strings.HasPrefix(rest, "proxy upstream "):
			body = replaceLine(body, "proxy upstream ", rest)
		case strings.HasPrefix(rest, "description "):
			body = replaceLine(body, "description ", rest)
		case strings.HasPrefix(rest, "proxy protocol "):
			body = replaceLine(body, "proxy protocol ", rest)
		case rest == "proxy socks5-udp":
			// KeeneticOS: SOCKS5 UDP needs the SOCKS5 protocol first.
			proto := false
			for _, l := range body {
				if strings.TrimSpace(l) == "proxy protocol socks5" {
					proto = true
				}
			}
			if !proto {
				return "error", "proxy protocol is not socks5"
			}
			body = replaceLine(body, "proxy socks5-udp", rest)
		case rest == "no proxy socks5-udp":
			var kept []string
			for _, l := range body {
				if strings.TrimSpace(l) != "proxy socks5-udp" {
					kept = append(kept, l)
				}
			}
			body = kept
		case rest == "proxy connect":
		case rest == "up":
			body = replaceLine(body, "up", "up")
		default:
			body = replaceLine(body, rest, rest)
		}
		blocks[key] = body
		return "message", "ok"
	case strings.HasPrefix(cmd, "ip route "):
		if len(f) != 6 {
			return "error", "argument parse error."
		}
		blocks[cmd] = nil
		return "message", "added static route"
	case strings.HasPrefix(cmd, "no ip route "):
		for k := range blocks {
			if strings.HasPrefix(k, "ip route "+f[3]+" "+f[4]+" "+f[5]+" ") {
				delete(blocks, k)
				return "message", "deleted static route"
			}
		}
		return "error", "no such route"
	case strings.HasPrefix(cmd, "dns-proxy tls upstream "):
		blocks[cmd] = nil
		return "message", "DNS-over-TLS name server added."
	case strings.HasPrefix(cmd, "no dns-proxy tls upstream "):
		for k := range blocks {
			if strings.HasPrefix(k, "dns-proxy tls upstream "+f[4]+" "+f[5]) {
				delete(blocks, k)
				return "message", "deleted."
			}
		}
		return "error", "not found"
	case strings.HasPrefix(cmd, "no ip http proxy "):
		delete(blocks, "ip http proxy "+f[4])
		return "message", "removed proxy."
	case strings.HasPrefix(cmd, "ip http proxy "):
		key := "ip http proxy " + f[3]
		if len(f) == 4 {
			if _, ok := blocks[key]; !ok {
				blocks[key] = []string{}
			}
			return "message", "created"
		}
		if _, ok := blocks[key]; !ok {
			return "error", "no such app"
		}
		blocks[key] = append(blocks[key], "    "+strings.Join(f[4:], " "))
		return "message", "ok"
	}
	return "error", "argument parse error."
}

func main() {
	addr := ":17979"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	startup = render()
	http.HandleFunc("/rci/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var cmds []struct {
				Parse string `json:"parse"`
			}
			b, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(b, &cmds); err != nil {
				w.WriteHeader(400)
				return
			}
			var out []map[string]any
			for _, c := range cmds {
				st, msg := parse(c.Parse)
				code, ident := "1", "x"
				if strings.HasPrefix(msg, "no such command") {
					code, ident = "7405600", "Command::Base"
				}
				out = append(out, map[string]any{"parse": map[string]any{"prompt": "(config)", "status": []map[string]string{{"status": st, "code": code, "ident": ident, "message": msg}}}})
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		switch r.URL.Path {
		case "/rci/show/running-config":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": render()})
		case "/rci/more/startup-config":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": startup})
		case "/rci/show/interface/Bridge0":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "Bridge0", "address": "192.168.1.1", "mask": "255.255.255.0", "state": "up"})
		case "/rci/show/dns-proxy":
			// RCI shape of "show dns-proxy": proxy-status[] with the System
			// profile's proxy-tls.server-tls[] (key order as KeeneticOS).
			ups := []string{"94.140.14.14 853 dns.adguard-dns.com"}
			names := make([]string, 0)
			for n := range blocks {
				if strings.HasPrefix(n, "dns-proxy tls upstream ") {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			for _, n := range names {
				f := strings.Fields(n)
				port, sni := "853", ""
				if len(f) > 4 && f[4] != "sni" {
					port = f[4]
				}
				for i := range f {
					if f[i] == "sni" && i+1 < len(f) {
						sni = f[i+1]
					}
				}
				ups = append(ups, f[3]+" "+port+" "+sni)
			}
			var b strings.Builder
			b.WriteString(`{"proxy-status":[{"proxy-name":"System","proxy-tls":{"server-tls":[`)
			for i, u := range ups {
				f := strings.Fields(u)
				if i > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, `{"address":%q,"port":%q,"sni":%q}`, f[0], f[1], f[2])
			}
			b.WriteString(`]}}]}`)
			_, _ = w.Write([]byte(b.String()))
		case "/rci/show/ipv6/addresses":
			_ = json.NewEncoder(w).Encode(ipv6Addresses())
		case "/rci/show/ipv6/prefixes":
			_ = json.NewEncoder(w).Encode(ipv6Prefixes())
		case "/rci/show/ndns":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "testrouter", "domain": "keenetic.pro"})
		case "/rci/show/version":
			_ = json.NewEncoder(w).Encode(map[string]any{"release": release(), "title": map[bool]string{false: "5.2 Beta 2", true: "4.0.5"}[noUDP], "model": "Keenetic Ultra", "hw_id": "KN-1811", "arch": "aarch64"})
		default:
			if strings.HasPrefix(r.URL.Path, "/rci/show/interface/Proxy") {
				name := strings.TrimPrefix(r.URL.Path, "/rci/show/interface/")
				if body, ok := blocks["interface "+name]; ok {
					desc := ""
					for _, l := range body {
						if t := strings.TrimSpace(l); strings.HasPrefix(t, "description ") {
							desc = strings.Trim(strings.TrimPrefix(t, "description "), `"`)
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": name, "index": 0, "type": "Proxy", "description": desc,
						"interface-name": name, "link": "up", "connected": "yes", "state": "up", "mtu": 1500, "tx-queue-length": 1000,
						"global": false, "defaultgw": false, "priority": 0, "security-level": "public"})
					return
				}
			}
			w.WriteHeader(404)
		}
	})
	http.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodDelete {
			failOn = map[string]bool{}
			return
		}
		if c := r.URL.Query().Get("cmd"); c != "" {
			failOn[c] = true
		}
	})
	http.HandleFunc("/ipv6", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			ipv6On = r.URL.Query().Get("enabled") == "1"
		}
		fmt.Fprintln(w, ipv6On)
	})
	http.HandleFunc("/firmware", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			noUDP = r.URL.Query().Get("udp") == "0"
		}
		fmt.Fprintln(w, release())
	})
	http.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintln(w, strings.Join(cmdLog, "\n"))
	})
	log.Fatal(http.ListenAndServe(addr, nil))
}
