// Package health runs the 3.x "204 probe" through the Xray SOCKS inbound,
// keeps the connection monitor state and drives auto-switch.
package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Result is one probe outcome.
type Result struct {
	OK        bool      `json:"ok"`
	URL       string    `json:"url"`
	Status    int       `json:"status"`
	LatencyMs int64     `json:"latencyMs"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

// Probe requests each URL through socks5://listen:port and accepts only
// HTTP 204 (cp.cloudflare.com, gstatic generate_204). The first success wins.
func Probe(ctx context.Context, listen string, port int, urls []string) Result {
	proxy := &url.URL{Scheme: "socks5", Host: net.JoinHostPort(listen, strconv.Itoa(port))}
	tr := &http.Transport{Proxy: http.ProxyURL(proxy), DialContext: (&net.Dialer{Timeout: 6 * time.Second}).DialContext, ResponseHeaderTimeout: 8 * time.Second, DisableKeepAlives: true}
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var last Result
	for _, u := range urls {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "BROray/3.5.0 health")
		resp, err := client.Do(req)
		r := Result{URL: u, At: time.Now(), LatencyMs: time.Since(start).Milliseconds()}
		if err != nil {
			r.Error = err.Error()
			last = r
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		r.Status = resp.StatusCode
		if resp.StatusCode == http.StatusNoContent {
			r.OK = true
			return r
		}
		r.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		last = r
	}
	if last.URL == "" {
		last.Error = "no probe URL"
	}
	return last
}

// Ping measures TCP connect time to host:port (the 3.x monitor used ICMP;
// a TCP connect needs no raw socket and measures the same path).
func Ping(ctx context.Context, host string, port int) (time.Duration, error) {
	d := &net.Dialer{Timeout: 5 * time.Second}
	start := time.Now()
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	c.Close()
	return time.Since(start), nil
}

// Monitor is the rolling state of the active connection.
type Monitor struct {
	mu          sync.Mutex
	Last        Result
	Consecutive int // consecutive failures
	History     []Result
}

func (m *Monitor) Record(r Result) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Last = r
	if r.OK {
		m.Consecutive = 0
	} else {
		m.Consecutive++
	}
	m.History = append(m.History, r)
	if len(m.History) > 60 {
		m.History = m.History[len(m.History)-60:]
	}
	return m.Consecutive
}

func (m *Monitor) Snapshot() (Result, int, []Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := append([]Result(nil), m.History...)
	return m.Last, m.Consecutive, h
}

var ErrNoCandidate = errors.New("AUTO_SWITCH_NO_CANDIDATE")
