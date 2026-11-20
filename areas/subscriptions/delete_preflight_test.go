package subscriptions

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

// A deletion must not silently skip an unreadable member or start deleting
// records before discovering that another member cannot be removed safely.
func TestSubscriptionDeletePreflightsEntireCatalog(t *testing.T) {
	for _, kind := range []string{"malformed", "duplicate-source", "id-mismatch", "record-link", "quality-directory", "quality-parent-link", "disabled-parent-link", "invalid-active", "linked-active"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			sub := e.create(t, "https://example.com/preflight", "")
			if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B"); err != nil {
				t.Fatal(err)
			}
			rows := e.s.liveServers()
			for _, f := range rows {
				sourceOf(f.rec)["subscriptionId"] = sub
				if err := servers.WriteRecord(e.d, f.rec); err != nil {
					t.Fatal(err)
				}
			}
			last := rows[len(rows)-1]
			outside := filepath.Join(t.TempDir(), "outside.json")
			writeFile(t, outside, `{"must":"survive"}`)
			protected := []string{e.s.recPath(sub), outside}
			for _, f := range rows {
				protected = append(protected, f.path)
			}
			switch kind {
			case "malformed":
				writeFile(t, last.path, `{"source":`)
			case "duplicate-source":
				raw, err := os.ReadFile(last.path)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, last.path, strings.TrimSuffix(strings.TrimSpace(string(raw)), "}")+`,"source":{"type":"manual"}}`)
			case "id-mismatch":
				raw, err := os.ReadFile(last.path)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, last.path, strings.ReplaceAll(string(raw), last.id, "srv-0000000000000000"))
			case "record-link":
				if err := os.Remove(last.path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, last.path); err != nil {
					t.Fatal(err)
				}
			case "quality-directory":
				p := servers.QualityPath(e.d, last.id)
				writeFile(t, filepath.Join(p, "occupied"), "keep")
				protected = append(protected, filepath.Join(p, "occupied"))
			case "quality-parent-link":
				p := filepath.Dir(servers.QualityPath(e.d, last.id))
				if err := os.MkdirAll(p, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(outside), p); err != nil {
					t.Fatal(err)
				}
			case "disabled-parent-link":
				p := e.s.disabledDir(sub)
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(outside), p); err != nil {
					t.Fatal(err)
				}
			case "invalid-active":
				writeFile(t, servers.ActiveFile(e.d), "../ambiguous")
				protected = append(protected, servers.ActiveFile(e.d))
			case "linked-active":
				if err := os.Symlink(outside, servers.ActiveFile(e.d)); err != nil {
					t.Fatal(err)
				}
				protected = append(protected, servers.ActiveFile(e.d))
			}
			before := map[string][]byte{}
			links := map[string]string{}
			for _, p := range protected {
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				before[p] = b
				if link, err := os.Readlink(p); err == nil {
					links[p] = link
				}
			}
			r := call(t, "POST", "/api/subscriptions/delete.cgi?id="+sub, `{}`, nil)
			if r.Status < 400 {
				t.Errorf("ambiguous catalog reported success: %s", r.Raw)
			}
			for p, b := range before {
				after, err := os.ReadFile(p)
				if err != nil || !bytes.Equal(b, after) {
					t.Errorf("preflight changed %s: %v", p, err)
				}
			}
			for p, want := range links {
				if got, err := os.Readlink(p); err != nil || got != want {
					t.Errorf("link changed %s: %v", p, err)
				}
			}
		})
	}
}

func TestSubscriptionDeleteDuplicateStorageCopies(t *testing.T) {
	e := newEnv(t)
	sub := e.create(t, "https://example.com/duplicate-copy", "")
	if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A"); err != nil {
		t.Fatal(err)
	}
	f := e.s.liveServers()[0]
	sourceOf(f.rec)["subscriptionId"] = sub
	if err := servers.WriteRecord(e.d, f.rec); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	disabled := filepath.Join(e.s.disabledDir(sub), filepath.Base(f.path))
	writeFile(t, disabled, string(b))
	quality := servers.QualityPath(e.d, f.id)
	writeFile(t, quality, `{"status":"available"}`)
	r := call(t, "POST", "/api/subscriptions/delete.cgi?id="+sub, `{}`, nil)
	if r.Status != 200 || r.data()["removed"] != float64(2) {
		t.Fatalf("duplicate owned copies: %s", r.Raw)
	}
	for _, p := range []string{f.path, disabled, quality, e.s.recPath(sub)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("owned entry remains %s: %v", p, err)
		}
	}
}

func TestSubscriptionDeleteRefusesSharedForeignIdentity(t *testing.T) {
	e := newEnv(t)
	sub := e.create(t, "https://example.com/identity-collision", "")
	if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A"); err != nil {
		t.Fatal(err)
	}
	f := e.s.liveServers()[0]
	// The live record belongs to subX. Only the disabled copy belongs to sub.
	sourceOf(f.rec)["subscriptionId"] = sub
	if err := e.s.writeServer(e.s.disabledDir(sub), f.rec); err != nil {
		t.Fatal(err)
	}
	disabled := filepath.Join(e.s.disabledDir(sub), filepath.Base(f.path))
	quality := servers.QualityPath(e.d, f.id)
	writeFile(t, quality, `{"status":"available","owner":"live-foreign"}`)
	before := map[string][]byte{}
	for _, p := range []string{f.path, disabled, quality, e.s.recPath(sub)} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = b
	}
	r := call(t, "POST", "/api/subscriptions/delete.cgi?id="+sub, `{}`, nil)
	if r.Status < 400 {
		t.Errorf("conflicting source ownership accepted: %s", r.Raw)
	}
	for p, b := range before {
		after, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(b, after) {
			t.Errorf("ownership conflict changed %s: %v", p, err)
		}
	}
}
