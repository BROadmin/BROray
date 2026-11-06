package routes

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// rcRoute is one `ip route` line of show running-config
// (lib/routes-router-config.sh:220-453).
type rcRoute struct {
	Known          bool
	Reason         string
	Line           int
	Network        string
	Mask           string
	Prefix         int
	Destination    string
	Interface      string
	Gateway        string
	Metric         *int
	MetricExplicit bool
	Automatic      bool
	Exclusive      bool
}

// rcSnapshot is the parsed running-config route state.
type rcSnapshot struct {
	Source       string
	FetchedAt    string
	FetchedEpoch int64
	Routes       []rcRoute
	Unknown      int
	NonRoute     []string // non-route lines (for baseline comparison)
}

func (s *rcSnapshot) complete() bool { return s != nil && s.Unknown == 0 }

func isIPv4Token(s string) bool { _, ok := parseIPv4Octets(s); return ok }

// parseRunningConfig parses configured static routes.
func parseRunningConfig(lines []string) *rcSnapshot {
	s := &rcSnapshot{Source: "running-config", FetchedAt: stamp(), FetchedEpoch: nowFn().Unix()}
	for i, raw := range lines {
		nr := i + 1
		line := strings.TrimRight(raw, "\r")
		t := strings.TrimSpace(line)
		f := strings.Fields(t)
		if len(f) < 2 || f[0] != "ip" || f[1] != "route" {
			s.NonRoute = append(s.NonRoute, line)
			continue
		}
		unknown := func(reason string) {
			s.Routes = append(s.Routes, rcRoute{Known: false, Reason: reason, Line: nr})
			s.Unknown++
		}
		if len(f) < 4 {
			unknown("insufficient-fields")
			continue
		}
		network := f[2]
		if !isIPv4Token(network) {
			unknown("unsupported-network-token")
			continue
		}
		r := rcRoute{Known: true, Line: nr, Network: network, Gateway: "0.0.0.0"}
		optStart := 0
		if !isIPv4Token(f[3]) {
			r.Mask, r.Prefix, r.Interface = "255.255.255.255", 32, f[3]
			optStart = 4
		} else {
			r.Mask = f[3]
			r.Prefix = maskPrefix(r.Mask)
			if r.Prefix < 0 || len(f) < 5 {
				unknown("invalid-mask-or-missing-interface")
				continue
			}
			if isIPv4Token(f[4]) {
				r.Gateway = f[4]
				if len(f) < 6 {
					unknown("missing-interface-after-gateway")
					continue
				}
				r.Interface = f[5]
				optStart = 6
			} else {
				r.Interface = f[4]
				optStart = 5
			}
		}
		if r.Interface == "" {
			unknown("empty-interface")
			continue
		}
		known, reason := true, ""
		for _, tok := range f[optStart:] {
			switch {
			case tok == "auto":
				r.Automatic = true
			case tok == "exclusive":
				r.Exclusive = true
			case allDigits(tok):
				if r.MetricExplicit {
					known, reason = false, "multiple-numeric-options"
				} else {
					m, _ := strconv.Atoi(strings.TrimLeft(tok, "0"))
					r.Metric, r.MetricExplicit = &m, true
				}
			default:
				known, reason = false, "unsupported-option-token"
			}
		}
		if !known {
			unknown(reason)
			continue
		}
		r.Destination = r.Network + "/" + strconv.Itoa(r.Prefix)
		s.Routes = append(s.Routes, r)
	}
	return s
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (r rcRoute) metricOr(def int) int {
	if r.Metric == nil {
		return def
	}
	return *r.Metric
}

// identityCount: same destination, interface, gateway 0.0.0.0 (proto static).
func (s *rcSnapshot) identityCount(dest, iface string) int {
	n := 0
	for _, r := range s.Routes {
		if r.Known && r.Destination == dest && r.Interface == iface && r.Gateway == "0.0.0.0" {
			n++
		}
	}
	return n
}

// attributeCount: identity plus an explicit metric equal to metric.
func (s *rcSnapshot) attributeCount(dest, iface string, metric int) int {
	n := 0
	for _, r := range s.Routes {
		if r.Known && r.MetricExplicit && r.Destination == dest && r.Interface == iface && r.Gateway == "0.0.0.0" && *r.Metric == metric {
			n++
		}
	}
	return n
}

// targetCount: same destination and interface, any gateway.
func (s *rcSnapshot) targetCount(dest, iface string) int {
	n := 0
	for _, r := range s.Routes {
		if r.Known && r.Destination == dest && r.Interface == iface {
			n++
		}
	}
	return n
}

// routeStateKey is the canonical route state (broray_routes_sync_route_state_equal).
func (s *rcSnapshot) routeStateKey() string {
	keys := make([]string, 0, len(s.Routes))
	for _, r := range s.Routes {
		m := "null"
		if r.Metric != nil {
			m = strconv.Itoa(*r.Metric)
		}
		keys = append(keys, strings.Join([]string{strconv.FormatBool(r.Known), r.Network, r.Mask, strconv.Itoa(r.Prefix), r.Destination,
			r.Interface, r.Gateway, m, strconv.FormatBool(r.Automatic), strconv.FormatBool(r.Exclusive), strconv.FormatBool(r.MetricExplicit)}, "|"))
	}
	sort.Strings(keys)
	return strconv.Itoa(s.Unknown) + "\n" + strings.Join(keys, "\n")
}

// nonRouteCanonical drops volatile comment lines ("!" lines: the
// Last change / Username / Md5 checksum headers and separators) and the
// static routes.
func (s *rcSnapshot) nonRouteCanonical() string {
	out := make([]string, 0, len(s.NonRoute))
	for _, l := range s.NonRoute {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "!") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// ---------- shared running-config cache ----------

// configSnapshot returns a cached snapshot no older than ttl, otherwise reads
// running-config (one read at a time, like the 3.x guarded cache refresh).
func (e *engine) configSnapshot(ctx context.Context, ttl time.Duration) (*rcSnapshot, error) {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if e.cache != nil && nowFn().Sub(e.cacheAt) <= ttl {
		return e.cache, nil
	}
	s, err := e.readConfig(ctx)
	if err != nil {
		return nil, err
	}
	e.cache, e.cacheAt = s, nowFn()
	return s, nil
}

// fetchConfig always reads running-config and refreshes the cache.
func (e *engine) fetchConfig(ctx context.Context) (*rcSnapshot, error) {
	s, err := e.readConfig(ctx)
	if err != nil {
		return nil, err
	}
	e.cacheMu.Lock()
	e.cache, e.cacheAt = s, nowFn()
	e.cacheMu.Unlock()
	return s, nil
}

func (e *engine) readConfig(ctx context.Context) (*rcSnapshot, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lines, err := e.T.RunningConfig(cctx)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errEmptyConfig
	}
	return parseRunningConfig(lines), nil
}

func (e *engine) invalidateCache() {
	e.cacheMu.Lock()
	e.cache = nil
	e.cacheMu.Unlock()
}

func (e *engine) fetchStartup(ctx context.Context) (*rcSnapshot, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lines, err := e.T.StartupConfig(cctx)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errEmptyConfig
	}
	return parseRunningConfig(lines), nil
}
