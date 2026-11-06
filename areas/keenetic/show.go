package keenetic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// textShower is implemented by test transports that serve CLI text directly.
type textShower interface {
	ShowText(ctx context.Context, command string) ([]string, error)
}

// ndmcBinary is the KeeneticOS CLI. 3.x read every "show" through ndmc and
// its parsers were validated against that text, so reads prefer it.
var ndmcBinary = "/bin/ndmc"

// showText returns the CLI text of a read-only command ("show running-config",
// "more startup-config", "show interface ProxyN", "show ndns"). Order: the
// transport's own text form (tests), ndmc, then the transport (RCI) with
// JSON rendered into the ndmc "key: value" layout. Like 3.x, any stderr
// output or non-zero rc is a failed read.
func (s *service) showText(ctx context.Context, command string) ([]string, error) {
	if ts, ok := s.d.App.T.(textShower); ok {
		return ts.ShowText(ctx, command)
	}
	if st, err := os.Stat(ndmcBinary); err == nil && st.Mode().IsRegular() {
		cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, ndmcBinary, "-c", command)
		cmd.Env = cleanEnv()
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil || errb.Len() > 0 {
			msg := strings.TrimSpace(errb.String())
			if msg == "" && err != nil {
				msg = err.Error()
			}
			return nil, errors.New(msg)
		}
		return splitLines(out.String()), nil
	}
	switch command {
	case "show running-config":
		return s.d.App.T.RunningConfig(ctx)
	case "more startup-config":
		return s.d.App.T.StartupConfig(ctx)
	}
	var raw json.RawMessage
	if err := s.d.App.T.Show(ctx, strings.ReplaceAll(command, " ", "/"), &raw); err != nil {
		return nil, err
	}
	return renderRCI(raw)
}

// captureConfig is broray_interface_capture_config: running or startup
// configuration with indentation canonicalised to four spaces (RCI and ndmc
// agree on content; 3.x grammar is written for the ndmc layout).
func (s *service) captureConfig(ctx context.Context, source string) ([]string, error) {
	cmd := "show running-config"
	if source == "startup" {
		cmd = "more startup-config"
	}
	lines, err := s.showText(ctx, cmd)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if t := strings.TrimLeft(l, " \t"); t != l && t != "" {
			l = "    " + t
		}
		out = append(out, l)
	}
	return out, nil
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

// renderRCI renders an RCI JSON object in the ndmc "key: value" layout,
// keeping document key order (nested objects/array elements open "key:").
func renderRCI(raw json.RawMessage) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out []string
	pad := func(d int) string { return strings.Repeat("    ", d) }
	scalar := func(v any) string {
		switch x := v.(type) {
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
	var walk func(key string, depth int) error
	walk = func(key string, depth int) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				next := depth
				if key != "" {
					out = append(out, pad(depth)+key+":")
					next = depth + 1
				}
				for dec.More() {
					kt, err := dec.Token()
					if err != nil {
						return err
					}
					k, _ := kt.(string)
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
		}
		out = append(out, pad(depth)+key+": "+scalar(tok))
		return nil
	}
	if err := walk("", 0); err != nil {
		return nil, err
	}
	return out, nil
}

// keyValues parses "key: value" lines (leading spaces trimmed); the first
// occurrence of a key wins (broray_interface_value / ndns field).
func keyValues(lines []string) map[string]string {
	out := map[string]string{}
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		if _, seen := out[k]; seen {
			continue
		}
		out[k] = strings.TrimSpace(line[i+1:])
	}
	return out
}
