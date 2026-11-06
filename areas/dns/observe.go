package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// liveRec is one DoT record of the strict r14c01 parsers (RD:364-614).
type liveRec struct {
	Index               int    `json:"index"`
	Address             string `json:"address"`
	PortRaw             string `json:"portRaw"`
	EffectivePort       int    `json:"effectivePort"`
	PortState           string `json:"portState"`
	SNI                 string `json:"sni"`
	SPKI                string `json:"spki"`
	On                  string `json:"on"`
	Interface           string `json:"interface"`
	Domain              string `json:"domain"`
	ExtraAttributeCount int    `json:"extraAttributeCount"`
	UnknownTokenCount   int    `json:"unknownTokenCount"`
	Valid               bool   `json:"valid"`
	ParseError          bool   `json:"parseError"`
}

func (r liveRec) clean() bool { return r.Valid && r.UnknownTokenCount == 0 }

// runtimeObs is the `show dns-proxy` observation (RD:486-614).
type runtimeObs struct {
	SchemaVersion          int       `json:"schemaVersion"`
	Source                 string    `json:"source"`
	Parser                 string    `json:"parser"`
	ProxyTLSSections       int       `json:"proxyTlsSections"`
	SystemProxyCount       int       `json:"systemProxyCount"`
	SystemProxyTLSSections int       `json:"systemProxyTlsSections"`
	PolicyProxyTLSSections int       `json:"policyProxyTlsSections"`
	UnknownSchemaCount     int       `json:"unknownSchemaCount"`
	Dot                    []liveRec `json:"dot"`
	ParseErrorCount        int       `json:"parseErrorCount"`
	Determinate            bool      `json:"determinate"`
}

// observed is broray_dot_fetch_observed (RD:616-640).
type observed struct {
	SchemaVersion          int       `json:"schemaVersion"`
	Source                 string    `json:"source"`
	Parser                 string    `json:"parser"`
	Dot                    []liveRec `json:"dot"`
	DohCount               int       `json:"dohCount"`
	TotalSecure            int       `json:"totalSecure"`
	DotParseErrorCount     int       `json:"dotParseErrorCount"`
	SelectorCollisionCount int       `json:"selectorCollisionCount"`
	Determinate            bool      `json:"determinate"`
	Runtime                any       `json:"runtime"`
	RuntimeReconciled      bool      `json:"runtimeReconciled"`
}

var (
	reTLSUpstreamTop   = regexp.MustCompile(`^dns-proxy[ \t\v\f\r]+tls[ \t\v\f\r]+upstream([ \t\v\f\r]|$)`)
	reTLSUpstreamBlock = regexp.MustCompile(`^tls[ \t\v\f\r]+upstream([ \t\v\f\r]|$)`)
	reDoHTop           = regexp.MustCompile(`^dns-proxy[ \t\v\f\r]+https[ \t\v\f\r]+upstream([ \t\v\f\r]|$)`)
	reDoHBlock         = regexp.MustCompile(`^https[ \t\v\f\r]+upstream([ \t\v\f\r]|$)`)
	reDigits           = regexp.MustCompile(`^[0-9]+$`)
	reProxyNameLine    = regexp.MustCompile(`^proxy-name:[ \t\v\f\r]*`)
	reProxySection     = regexp.MustCompile(`^proxy-[A-Za-z0-9_-]+:$`)
)

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\v' || b == '\f' || b == '\r' || b == '\n'
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && isSpace(s[i]) {
		i++
	}
	for j > i && isSpace(s[j-1]) {
		j--
	}
	return s[i:j]
}

func fieldsSpace(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) })
}

func portNumber(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		if reDigits.MatchString(s) {
			return 1 << 30 // out of range, keeps the record invalid
		}
		return 0
	}
	return n
}

// parseRunning implements broray_dot_fetch_running's awk/jq program.
func parseRunning(lines []string) observed {
	var recs []liveRec
	inDNSProxy := false
	doh := 0
	idx := 0
	for _, raw0 := range lines {
		raw := strings.TrimSuffix(raw0, "\r")
		line := trimSpace(raw)
		indented := raw != "" && isSpace(raw[0])
		if !indented && line != "" {
			inDNSProxy = line == "dns-proxy"
		}
		cmd := ""
		if !indented && reTLSUpstreamTop.MatchString(line) {
			cmd = line
		} else if inDNSProxy && indented && reTLSUpstreamBlock.MatchString(line) {
			cmd = "dns-proxy " + line
		}
		if (!indented && reDoHTop.MatchString(line)) || (inDNSProxy && indented && reDoHBlock.MatchString(line)) {
			doh++
		}
		if cmd == "" {
			continue
		}
		idx++
		f := fieldsSpace(cmd)
		n := len(f)
		at := func(i int) string { // awk 1-based
			if i >= 1 && i <= n {
				return f[i-1]
			}
			return ""
		}
		r := liveRec{Index: idx, EffectivePort: 853, PortState: "omitted"}
		valid := true
		unknown, extra := 0, 0
		if n < 4 || at(1) != "dns-proxy" || at(2) != "tls" || at(3) != "upstream" {
			valid = false
		} else {
			r.Address = at(4)
		}
		if r.Address == "" {
			valid = false
		}
		i := 5
		if i <= n && reDigits.MatchString(at(i)) {
			r.PortRaw = at(i)
			r.EffectivePort = portNumber(at(i))
			r.PortState = "explicit"
			if r.EffectivePort < 1 || r.EffectivePort > 65535 {
				valid = false
			}
			i++
		}
		seen := map[string]bool{}
		for i <= n {
			key := at(i)
			switch key {
			case "sni", "spki", "on", "domain":
				if seen[key] || i >= n {
					valid = false
					unknown++
					i++
					continue
				}
				seen[key] = true
				v := at(i + 1)
				switch key {
				case "sni":
					r.SNI = v
				case "spki":
					r.SPKI = v
					extra++
				case "on":
					r.On = v
					extra++
				case "domain":
					r.Domain = v
					extra++
				}
				i += 2
				continue
			}
			valid = false
			unknown++
			i++
		}
		if !seen["sni"] || r.SNI == "" {
			valid = false
			unknown++
		}
		for _, x := range f {
			if strings.ContainsAny(x, "\t\r\n\"\\") {
				valid = false
				unknown++
			}
		}
		if strings.Contains(r.Address, ":") {
			valid = false
			unknown++
		}
		r.Interface = r.On
		r.ExtraAttributeCount, r.UnknownTokenCount, r.Valid, r.ParseError = extra, unknown, valid, !valid
		recs = append(recs, r)
	}
	if recs == nil {
		recs = []liveRec{}
	}
	parseErrors := 0
	groups := map[string]int{}
	for _, r := range recs {
		if r.ParseError || r.UnknownTokenCount != 0 {
			parseErrors++
		}
		groups[r.Address+"\x00"+strconv.Itoa(r.EffectivePort)]++
	}
	collisions := 0
	for _, c := range groups {
		if c > 1 {
			collisions++
		}
	}
	return observed{SchemaVersion: 2, Source: "running-config", Parser: "r14c01-strict", Dot: recs, DohCount: doh,
		TotalSecure: len(recs) + doh, DotParseErrorCount: parseErrors, SelectorCollisionCount: collisions,
		Determinate: parseErrors == 0 && collisions == 0}
}

// parseRuntime implements broray_dot_fetch_runtime's awk/jq program.
func parseRuntime(lines []string) runtimeObs {
	var recs []liveRec
	inProxyTLS, inRecord := false, false
	idx, unknownSchema, sections, systemProfiles, systemSections := 0, 0, 0, 0, 0
	current := ""
	var r liveRec
	var seenAddr, seenPort, seenSNI, seenSPKI, seenIface, seenDomain, valid bool
	unknown, extra := 0, 0
	reset := func() {
		r = liveRec{EffectivePort: 853, PortState: "omitted"}
		seenAddr, seenPort, seenSNI, seenSPKI, seenIface, seenDomain = false, false, false, false, false, false
		valid, unknown, extra = true, 0, 0
	}
	flush := func() {
		if !inRecord {
			return
		}
		idx++
		if !seenAddr || !seenSNI || r.SNI == "" {
			valid = false
			unknown++
		}
		if r.Address == "" {
			valid = false
		}
		if seenPort {
			if r.PortRaw == "" || !reDigits.MatchString(r.PortRaw) {
				valid = false
			} else {
				r.EffectivePort = portNumber(r.PortRaw)
				r.PortState = "explicit"
				if r.EffectivePort < 1 || r.EffectivePort > 65535 {
					valid = false
				}
			}
		}
		if strings.ContainsAny(r.Address, ":\"\\") || strings.ContainsAny(r.SNI, "\"\\") || strings.ContainsAny(r.SPKI, "\"\\") ||
			strings.ContainsAny(r.On, "\"\\") || strings.ContainsAny(r.Domain, "\"\\") {
			valid = false
			unknown++
		}
		r.Index = idx
		r.Interface = r.On
		r.ExtraAttributeCount, r.UnknownTokenCount, r.Valid, r.ParseError = extra, unknown, valid, !valid
		recs = append(recs, r)
		inRecord = false
	}
	reset()
	for _, raw0 := range lines {
		line := trimSpace(strings.TrimSuffix(raw0, "\r"))
		switch {
		case line == "proxy-status:":
			flush()
			inProxyTLS, current = false, ""
			continue
		case reProxyNameLine.MatchString(line):
			flush()
			inProxyTLS = false
			current = reProxyNameLine.ReplaceAllString(line, "")
			if current == "System" {
				systemProfiles++
			}
			continue
		case line == "proxy-tls:":
			flush()
			sections++
			if current == "System" {
				systemSections++
				inProxyTLS = true
			} else {
				inProxyTLS = false
			}
			continue
		case reProxySection.MatchString(line):
			flush()
			inProxyTLS = false
			continue
		}
		if !inProxyTLS {
			continue
		}
		if line == "server-tls:" {
			flush()
			reset()
			inRecord = true
			continue
		}
		if !inRecord {
			if line != "" {
				unknownSchema++
			}
			continue
		}
		if line == "" {
			continue
		}
		sep := strings.Index(line, ":")
		if sep < 0 {
			valid = false
			unknown++
			unknownSchema++
			continue
		}
		key, value := line[:sep], trimSpace(line[sep+1:])
		dup := func(seen *bool) {
			if *seen {
				valid = false
				unknown++
			}
			*seen = true
		}
		switch key {
		case "address":
			dup(&seenAddr)
			r.Address = value
		case "port":
			dup(&seenPort)
			r.PortRaw = value
		case "sni":
			dup(&seenSNI)
			r.SNI = value
		case "spki":
			dup(&seenSPKI)
			r.SPKI = value
			if value != "" {
				extra++
			}
		case "interface":
			dup(&seenIface)
			r.On = value
			if value != "" {
				extra++
			}
		case "domain":
			dup(&seenDomain)
			r.Domain = value
			if value != "" {
				extra++
			}
		default:
			valid = false
			unknown++
			unknownSchema++
		}
	}
	flush()
	if recs == nil {
		recs = []liveRec{}
	}
	parseErrors := 0
	for _, x := range recs {
		if x.ParseError || x.UnknownTokenCount != 0 {
			parseErrors++
		}
	}
	return runtimeObs{SchemaVersion: 1, Source: "show-dns-proxy", Parser: "r14c01-strict", ProxyTLSSections: sections,
		SystemProxyCount: systemProfiles, SystemProxyTLSSections: systemSections, PolicyProxyTLSSections: sections - systemSections,
		UnknownSchemaCount: unknownSchema, Dot: recs, ParseErrorCount: parseErrors,
		Determinate: systemProfiles == 1 && systemSections == 1 && unknownSchema == 0 && parseErrors == 0}
}

func liveSame(a, b liveRec) bool {
	return a.Address == b.Address && a.EffectivePort == b.EffectivePort && a.SNI == b.SNI && a.SPKI == b.SPKI &&
		a.Interface == b.Interface && a.Domain == b.Domain
}

// reconcile combines both observations (RD:616-640).
func reconcile(c observed, r runtimeObs) observed {
	matched := func(entries, others []liveRec) int {
		n := 0
		for _, e := range entries {
			if !e.clean() {
				continue
			}
			k := 0
			for _, o := range others {
				if o.clean() && liveSame(e, o) {
					k++
				}
			}
			if k == 1 {
				n++
			}
		}
		return n
	}
	cm, rm := matched(c.Dot, r.Dot), matched(r.Dot, c.Dot)
	det := c.Determinate && r.Determinate && len(c.Dot) == len(r.Dot) && cm == len(c.Dot) && rm == len(r.Dot)
	c.Runtime = r
	c.RuntimeReconciled = det
	c.Determinate = det
	return c
}

// unavailableObserved is the stub of broray_dot_status when the router read failed (RD:977-983).
func unavailableObserved() observed {
	return observed{SchemaVersion: 2, Source: "unavailable", Parser: "r14c01-strict", Dot: []liveRec{},
		Runtime: map[string]any{"schemaVersion": 1, "source": "unavailable", "dot": []any{}, "proxyTlsSections": 0,
			"unknownSchemaCount": 0, "parseErrorCount": 0, "determinate": false}}
}

// ---------- Keenetic reads ----------

// textShower is implemented by test transports that serve CLI text directly.
type textShower interface {
	ShowText(ctx context.Context, command string) ([]string, error)
}

// ndmcBinary is the KeeneticOS CLI used for text "show" commands whose 3.x
// parsers were validated against ndmc output.
var ndmcBinary = "/bin/ndmc"

// showText returns the CLI text of a read-only "show ..." command: the
// transport's own text form when it has one, then ndmc (the exact 3.x
// source), then the RCI JSON rendered into the same key: value layout.
func (s *service) showText(ctx context.Context, command string) ([]string, error) {
	if ts, ok := s.d.App.T.(textShower); ok {
		return ts.ShowText(ctx, command)
	}
	if st, err := os.Stat(ndmcBinary); err == nil && st.Mode().IsRegular() {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, ndmcBinary, "-c", command)
		cmd.Env = cleanEnv()
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil || errb.Len() > 0 {
			return nil, errors.New(strings.TrimSpace(errb.String()))
		}
		return splitLines(out.String()), nil
	}
	var raw json.RawMessage
	if err := s.d.App.T.Show(ctx, strings.ReplaceAll(command, " ", "/"), &raw); err != nil {
		return nil, err
	}
	return renderRCI(raw)
}

func cleanEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") || strings.HasPrefix(kv, "LD_PRELOAD=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// renderRCI renders an RCI JSON object in the ndmc "key: value" layout
// (objects and array elements open an indented "key:" section), keeping the
// key order of the document.
func renderRCI(raw json.RawMessage) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out []string
	var walk func(key string, depth int) error
	scalar := func(v any) string {
		switch x := v.(type) {
		case nil:
			return ""
		case string:
			return x
		case json.Number:
			return x.String()
		case bool:
			if x {
				return "yes"
			}
			return "no"
		}
		return ""
	}
	pad := func(d int) string { return strings.Repeat("    ", d) }
	walk = func(key string, depth int) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				if key != "" {
					out = append(out, pad(depth)+key+":")
				}
				for dec.More() {
					kt, err := dec.Token()
					if err != nil {
						return err
					}
					k, _ := kt.(string)
					next := depth + 1
					if key == "" {
						next = depth
					}
					if err := walk(k, next); err != nil {
						return err
					}
				}
				_, err = dec.Token()
				return err
			case '[':
				for dec.More() {
					if err := walk(key, depth); err != nil {
						return err
					}
				}
				_, err = dec.Token()
				return err
			}
		default:
			out = append(out, pad(depth)+key+": "+scalar(t))
		}
		return nil
	}
	if err := walk("", 0); err != nil {
		return nil, err
	}
	return out, nil
}

// fetchObserved reads running-config and `show dns-proxy` and reconciles
// them. The error is a contract error (KEENETIC_UNAVAILABLE / KEENETIC_RUNTIME_UNAVAILABLE).
func (s *service) fetchObserved(ctx context.Context) (observed, error) {
	lines, err := s.d.App.T.RunningConfig(ctx)
	if err != nil {
		return observed{}, dotErr("KEENETIC_UNAVAILABLE", "Не удалось прочитать конфигурацию Keenetic.", lastLines(err.Error(), 20))
	}
	c := parseRunning(lines)
	rl, err := s.showText(ctx, "show dns-proxy")
	if err != nil {
		return observed{}, dotErr("KEENETIC_RUNTIME_UNAVAILABLE", "Не удалось прочитать runtime DNS proxy Keenetic.", lastLines(err.Error(), 20))
	}
	return reconcile(c, parseRuntime(rl)), nil
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

func sortedCopy(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}
