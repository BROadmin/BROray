package keenetic

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
)

// IPv6 warning fact (owner decision, 3.5.0): BROray routes only IPv4, so it
// warns when the router has global IPv6. Detection is read-only and cached:
// status.cgi, the home snapshot (30 s) and the routes overview all read the
// cached fact, the router is asked at most once per IPv6CacheTTL (dev
// builds may shorten it with BRORAY_DEV_IPV6_TTL).
var (
	IPv6CacheTTL        = 5 * time.Minute
	ipv6UnknownRetryTTL = time.Minute // a failed read is retried sooner
	ipv6QueryTimeout    = 20 * time.Second
)

// ipv6Fact returns the cached answer immediately. A stale or absent answer
// starts one background refresh under the daemon context; a slow router can
// never delay status.cgi, Home or Routes. A closed browser does not cancel
// the shared refresh.
func (s *service) ipv6Fact(_ context.Context) map[string]any {
	s.ipv6Mu.Lock()
	ttl := IPv6CacheTTL
	if s.ipv6.State != kn.IPv6Global && s.ipv6.State != kn.IPv6None && ipv6UnknownRetryTTL < ttl {
		ttl = ipv6UnknownRetryTTL
	}
	if (s.ipv6At.IsZero() || time.Since(s.ipv6At) >= ttl) && !s.ipv6Refreshing && s.d.Ctx.Err() == nil {
		s.ipv6Refreshing = true
		go s.refreshIPv6()
	}
	fact, checked := s.ipv6, s.ipv6Stamp
	s.ipv6Mu.Unlock()
	return fact.Object(checked)
}

func (s *service) refreshIPv6() {
	fact := s.detectIPv6(s.d.Ctx)
	s.ipv6Mu.Lock()
	defer s.ipv6Mu.Unlock()
	s.ipv6Refreshing = false
	if s.d.Ctx.Err() != nil {
		return // daemon stopped: do not cache a partial answer
	}
	s.ipv6, s.ipv6At = fact, time.Now()
	s.ipv6Stamp = compat.Stamp(s.ipv6At)
}

// detectIPv6 runs the read-only IPv6 queries: RCI JSON first (structured),
// then the CLI text (ndmc or the transport's text form). A query that fails
// both ways contributes nothing; no successful query means "unknown".
func (s *service) detectIPv6(ctx context.Context) kn.IPv6Facts {
	var acc kn.IPv6Facts
	for _, q := range kn.IPv6ReadQueries {
		if ctx.Err() != nil {
			break
		}
		if f, ok := s.readIPv6(ctx, q); ok {
			acc = acc.Merge(f)
		}
	}
	return acc
}

func (s *service) readIPv6(ctx context.Context, command string) (kn.IPv6Facts, bool) {
	cctx, cancel := context.WithTimeout(ctx, ipv6QueryTimeout)
	defer cancel()
	var raw json.RawMessage
	if err := s.d.App.T.Show(cctx, strings.ReplaceAll(command, " ", "/"), &raw); err == nil {
		// The router answered: an unreadable or error answer is final (the
		// text fallback would only re-render the same RCI answer).
		f, err := kn.ParseIPv6JSON(raw)
		return f, err == nil
	}
	if lines, err := s.showText(cctx, command); err == nil {
		if f, err := kn.ParseIPv6Text(lines); err == nil {
			return f, true
		}
	}
	return kn.IPv6Facts{}, false
}
