// Package subscriptions fetches server lists (URI lines, Base64 or a JSON
// array of links), stores them under data/subscriptions and refreshes the
// servers they own. Limits follow 3.x: 2 MiB, 500 nodes, 5–10080 minutes.
package subscriptions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/servers"
)

const (
	MaxBytes    = 2 << 20
	MaxNodes    = 500
	MinInterval = 5
	MaxInterval = 10080
)

var (
	ErrNotFound   = errors.New("SUBSCRIPTION_NOT_FOUND")
	ErrInvalid    = errors.New("SUBSCRIPTION_INVALID")
	idPattern     = regexp.MustCompile(`^sub-[0-9a-f]{16}$`)
	schemePattern = regexp.MustCompile(`(?i)^(vless|vmess|trojan|ss|hysteria2|hy2|tuic|socks|socks5|http|https)://`)
)

type Subscription struct {
	SchemaVersion   int    `json:"schemaVersion"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Enabled         bool   `json:"enabled"`
	AutoUpdate      bool   `json:"autoUpdateEnabled"`
	IntervalMinutes int    `json:"updateIntervalMinutes"`
	LastUpdateAt    string `json:"lastUpdateAt,omitempty"`
	NextUpdateAt    string `json:"nextUpdateAt,omitempty"`
	LastResult      string `json:"lastResult,omitempty"` // ok | error
	LastError       string `json:"lastError,omitempty"`
	NodeCount       int    `json:"nodeCount"`
	ImportedCount   int    `json:"importedCount"`
	SkippedCount    int    `json:"skippedCount"`
	CreatedAt       string `json:"createdAt,omitempty"`
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "sub-" + hex.EncodeToString(b)
}

func ValidID(s string) bool { return idPattern.MatchString(s) }

func validURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && len(s) <= 2048
}

type Store struct {
	mu  sync.Mutex
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

func (s *Store) List() ([]Subscription, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Subscription
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var sub Subscription
		if platform.ReadJSON(filepath.Join(s.dir, e.Name()), &sub) == nil && ValidID(sub.ID) {
			out = append(out, sub)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

func (s *Store) Get(id string) (Subscription, error) {
	if !ValidID(id) {
		return Subscription{}, ErrNotFound
	}
	var sub Subscription
	if err := platform.ReadJSON(s.path(id), &sub); err != nil {
		return Subscription{}, ErrNotFound
	}
	return sub, nil
}

func (s *Store) Put(sub Subscription) error {
	if !ValidID(sub.ID) || !validURL(sub.URL) || strings.TrimSpace(sub.Name) == "" || len([]rune(sub.Name)) > 96 {
		return ErrInvalid
	}
	if sub.IntervalMinutes < MinInterval || sub.IntervalMinutes > MaxInterval {
		return fmt.Errorf("%w: interval", ErrInvalid)
	}
	sub.SchemaVersion = 1
	s.mu.Lock()
	defer s.mu.Unlock()
	return platform.WriteJSONAtomic(s.path(sub.ID), sub, 0o600)
}

func (s *Store) Delete(id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil {
		return ErrNotFound
	}
	return nil
}

// Fetch downloads and decodes the subscription into share links.
func Fetch(ctx context.Context, client *http.Client, rawURL string) ([]string, error) {
	if !validURL(rawURL) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BROray/3.5.0 (Xray)")
	req.Header.Set("Accept", "text/plain, application/json;q=0.9, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SUBSCRIPTION_FETCH_FAILED: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SUBSCRIPTION_FETCH_FAILED: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("SUBSCRIPTION_FETCH_FAILED: %v", err)
	}
	if len(body) > MaxBytes {
		return nil, errors.New("SUBSCRIPTION_TOO_LARGE")
	}
	return Decode(body)
}

// Decode accepts: plain lines of links; Base64 of such lines; JSON array of
// link strings or {"links":[...]} / {"servers":[...]}. HTML is refused.
func Decode(body []byte) ([]string, error) {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return nil, errors.New("SUBSCRIPTION_EMPTY")
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return nil, errors.New("SUBSCRIPTION_IS_HTML")
	}
	if strings.HasPrefix(text, "[") || strings.HasPrefix(text, "{") {
		var arr []string
		if json.Unmarshal([]byte(text), &arr) == nil {
			return linksFromLines(arr)
		}
		var obj struct {
			Links   []string `json:"links"`
			Servers []string `json:"servers"`
		}
		if json.Unmarshal([]byte(text), &obj) == nil {
			return linksFromLines(append(obj.Links, obj.Servers...))
		}
		return nil, errors.New("SUBSCRIPTION_JSON_UNSUPPORTED")
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if links, _ := linksFromLines(lines); len(links) > 0 {
		return links, nil
	}
	// Base64 (standard or URL, padded or not), possibly with line breaks.
	compact := strings.Join(strings.Fields(text), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if dec, err := enc.DecodeString(compact); err == nil {
			decoded := strings.ReplaceAll(string(dec), "\r\n", "\n")
			if links, _ := linksFromLines(strings.Split(decoded, "\n")); len(links) > 0 {
				return links, nil
			}
		}
	}
	return nil, errors.New("SUBSCRIPTION_FORMAT_UNRECOGNIZED")
}

func linksFromLines(lines []string) ([]string, error) {
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || !schemePattern.MatchString(l) {
			continue
		}
		out = append(out, l)
		if len(out) > MaxNodes {
			return nil, errors.New("SUBSCRIPTION_TOO_MANY_NODES")
		}
	}
	return out, nil
}

// Refresh fetches a subscription and replaces its servers in the store.
// Servers that fail to parse are counted as skipped. The active server is
// kept if the same URI is still listed (its id is preserved).
func Refresh(ctx context.Context, client *http.Client, subs *Store, srvs *servers.Store, id string) (Subscription, error) {
	sub, err := subs.Get(id)
	if err != nil {
		return sub, err
	}
	links, err := Fetch(ctx, client, sub.URL)
	now := time.Now().UTC()
	if err != nil {
		sub.LastResult, sub.LastError, sub.LastUpdateAt = "error", err.Error(), now.Format(time.RFC3339)
		sub.NextUpdateAt = now.Add(time.Duration(sub.IntervalMinutes) * time.Minute).Format(time.RFC3339)
		_ = subs.Put(sub)
		return sub, err
	}
	existing, _ := srvs.List()
	byURI := map[string]servers.Server{}
	for _, s := range existing {
		if s.Source.Type == "subscription" && s.Source.SubscriptionID == id {
			byURI[s.URI] = s
		}
	}
	active := srvs.Active()
	seen := map[string]bool{}
	imported, skipped := 0, 0
	for idx, link := range links {
		parsed, perr := servers.ParseLink(link)
		if perr != nil {
			skipped++
			continue
		}
		parsed.Source = servers.Source{Type: "subscription", SubscriptionID: id, NodeIndex: idx}
		if old, ok := byURI[link]; ok {
			parsed.ID, parsed.CreatedAt = old.ID, old.CreatedAt
			parsed.Name = old.Name
			if err := srvs.Put(parsed); err == nil {
				imported++
				seen[old.ID] = true
			}
			continue
		}
		if _, aerr := srvs.Add(parsed); aerr == nil {
			imported++
			seen[parsed.ID] = true
		} else {
			skipped++
		}
	}
	for _, old := range byURI {
		if !seen[old.ID] && old.ID != active {
			_ = srvs.Delete(old.ID)
		}
	}
	sub.LastResult, sub.LastError = "ok", ""
	sub.LastUpdateAt = now.Format(time.RFC3339)
	sub.NextUpdateAt = now.Add(time.Duration(sub.IntervalMinutes) * time.Minute).Format(time.RFC3339)
	sub.NodeCount, sub.ImportedCount, sub.SkippedCount = len(links), imported, skipped
	return sub, subs.Put(sub)
}

// Due lists enabled, auto-updating subscriptions whose next update has passed.
func Due(subs *Store, now time.Time) []Subscription {
	list, _ := subs.List()
	var out []Subscription
	for _, s := range list {
		if !s.Enabled || !s.AutoUpdate {
			continue
		}
		if s.NextUpdateAt == "" {
			out = append(out, s)
			continue
		}
		if t, err := time.Parse(time.RFC3339, s.NextUpdateAt); err == nil && !now.Before(t) {
			out = append(out, s)
		}
	}
	return out
}
