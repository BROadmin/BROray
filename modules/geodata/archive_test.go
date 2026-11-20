package geodata

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type archiveStore interface {
	ExportArchive(context.Context) ([]byte, error)
	PrepareArchive(context.Context, []byte, CoreValidator) (*Candidate, error)
}

func archiveAPI(t *testing.T, s *Store) archiveStore {
	t.Helper()
	api, ok := any(s).(archiveStore)
	if !ok {
		t.Fatal("validated offline geodata archive API missing")
	}
	return api
}
func archiveSource(t *testing.T) (*Store, string, Snapshot) {
	t.Helper()
	s, root := testStore(t)
	c, e := s.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.Commit(testContext, c, "")
	if e != nil {
		t.Fatal(e)
	}
	return s, root, snap
}
func TestGeodataArchiveOfflineRoundtrip(t *testing.T) {
	source, root, before := archiveSource(t)
	data, err := archiveAPI(t, source).ExportArchive(testContext)
	if err != nil {
		t.Fatal(err)
	}
	target, targetRoot := testStore(t)
	validated := false
	c, err := archiveAPI(t, target).PrepareArchive(testContext, data, func(_ context.Context, s Snapshot) error {
		validated = true
		b, e := os.ReadFile(filepath.Join(targetRoot, s.Assets[0].Path))
		if e != nil {
			return e
		}
		old, e := os.ReadFile(filepath.Join(root, before.Assets[0].Path))
		if e != nil {
			return e
		}
		if !bytes.Equal(b, old) {
			t.Fatal("archive changed native payload")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := target.Load(testContext)
	if err != nil || empty.Revision != "" {
		t.Fatal("prepare changed active target", err)
	}
	next, err := target.Commit(testContext, c, "")
	if err != nil {
		t.Fatal(err)
	}
	if !validated || next.Generation == before.Generation || next.Assets[0].SHA256 != before.Assets[0].SHA256 || !reflect.DeepEqual(next.Selection, before.Selection) {
		t.Fatal("lost payload/selection or adopted archive generation")
	}
	restarted, err := Open(targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got, err := restarted.Load(testContext)
	if err != nil || got.Revision != next.Revision {
		t.Fatal("offline reload", err)
	}
	same, err := source.Load(testContext)
	if err != nil || same.Revision != before.Revision {
		t.Fatal("export mutated source", err)
	}
}
func TestGeodataArchiveValidationAndCancellation(t *testing.T) {
	source, _, _ := archiveSource(t)
	data, err := archiveAPI(t, source).ExportArchive(testContext)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"nil-validator", "rejected-core", "cancelled", "invalid-json", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			target, _ := testStore(t)
			ctx := testContext
			input := data
			validate := CoreValidator(fixtureCore)
			switch kind {
			case "nil-validator":
				validate = nil
			case "rejected-core":
				validate = func(context.Context, Snapshot) error { return errors.New("native core rejected archive") }
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "invalid-json":
				input = []byte(`{"schemaVersion":1,"schemaVersion":2}`)
			case "oversize":
				input = bytes.Repeat([]byte(" "), (2<<20)+1)
			}
			if _, err := archiveAPI(t, target).PrepareArchive(ctx, input, validate); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			got, err := target.Load(testContext)
			if err != nil || got.Revision != "" {
				t.Fatal("failure changed target", err)
			}
		})
	}
}

func TestGeodataArchiveRejectsCorruptionAndAmbiguousInput(t *testing.T) {
	source, _, _ := archiveSource(t)
	raw, err := archiveAPI(t, source).ExportArchive(testContext)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"duplicate", "alias", "selection-alias", "extra-key", "missing-file", "extra-file", "null-files", "foreign-source", "hash", "truncated", "trailing", "multistream", "gzip-checksum", "expansion", "native-invalid"} {
		t.Run(kind, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			snapshot := doc["snapshot"].(map[string]any)
			asset := snapshot["assets"].([]any)[0].(map[string]any)
			files := doc["files"].(map[string]any)
			name := asset["name"].(string)
			compressed, err := base64.StdEncoding.DecodeString(files[name].(string))
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "alias":
				doc["SchemaVersion"] = doc["schemaVersion"]
				delete(doc, "schemaVersion")
			case "selection-alias":
				selection := snapshot["selection"].(map[string]any)
				selection["geositeTags"] = selection["geoSiteTags"]
				delete(selection, "geoSiteTags")
			case "extra-key":
				doc["ignored"] = true
			case "missing-file":
				delete(files, name)
			case "extra-file":
				files["../foreign"] = files[name]
			case "null-files":
				doc["files"] = nil
			case "foreign-source":
				asset["url"] = "https://foreign.invalid/fi.dat"
			case "hash":
				asset["sha256"] = strings.Repeat("0", 64)
			case "truncated":
				compressed = compressed[:len(compressed)-1]
			case "trailing":
				compressed = append(compressed, 0)
			case "multistream":
				compressed = append(compressed, compressed...)
			case "gzip-checksum":
				compressed[len(compressed)-8] ^= 0xff
			case "expansion", "native-invalid":
				payload := []byte{0xff, 0xff, 0xff}
				if kind == "expansion" {
					payload = bytes.Repeat([]byte{1}, 4096)
				} else {
					asset["bytes"] = len(payload)
					asset["sha256"] = digest(payload)
				}
				var b bytes.Buffer
				w := gzip.NewWriter(&b)
				if _, err := w.Write(payload); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				compressed = b.Bytes()
			}
			if kind != "missing-file" {
				files[name] = base64.StdEncoding.EncodeToString(compressed)
			}
			encoded, _ := json.Marshal(snapshot)
			var meta Snapshot
			if err := json.Unmarshal(encoded, &meta); err != nil {
				t.Fatal(err)
			}
			snapshot["revision"] = snapshotRevision(meta)
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "duplicate" {
				data = bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"schemaVersion":1`), 1)
			}
			target, root := testStore(t)
			if _, err := archiveAPI(t, target).PrepareArchive(testContext, data, fixtureCore); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			current, err := target.Load(testContext)
			if err != nil || current.Revision != "" {
				t.Fatal("failure changed current", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("failed archive leaked generation", entries, err)
			}
		})
	}
}
func TestGeodataArchiveEmptyAndForeignInputs(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s, _ := testStore(t)
		data, err := archiveAPI(t, s).ExportArchive(testContext)
		if err != nil {
			t.Fatal(err)
		}
		target, _ := testStore(t)
		c, err := archiveAPI(t, target).PrepareArchive(testContext, data, fixtureCore)
		if err != nil {
			t.Fatal(err)
		}
		next, err := target.Commit(testContext, c, "")
		if err != nil || len(next.Assets) != 0 {
			t.Fatal("empty archive", err)
		}
	})
	t.Run("linked-asset", func(t *testing.T) {
		s, root, snap := archiveSource(t)
		path := filepath.Join(root, snap.Assets[0].Path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(t.TempDir(), "foreign.dat")
		if err = os.WriteFile(foreign, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(path, path+".saved"); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(foreign, path); err != nil {
			t.Fatal(err)
		}
		if _, err = archiveAPI(t, s).ExportArchive(testContext); err == nil {
			t.Fatal("adopted linked payload")
		}
		after, err := os.ReadFile(foreign)
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatal("foreign payload changed", err)
		}
	})
	t.Run("replaced-root", func(t *testing.T) {
		s, root, _ := archiveSource(t)
		if err := os.Rename(root, root+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := archiveAPI(t, s).ExportArchive(testContext); err == nil {
			t.Fatal("adopted replaced root")
		}
	})
}

func TestGeodataArchivePinnedNativeCore(t *testing.T) {
	core := os.Getenv("BRORAY_XRAY_BIN")
	if core == "" {
		t.Fatal("pinned native core required")
	}
	source, _ := testStore(t)
	selection := Selection{CountryCodes: []string{"fi"}, GeoSiteTags: []string{"youtube", "category-ads", "category-telemetry"}}
	revisions := Revisions{GeoIP: "07b6131c86aeafbc6d9742b9baa3f8d7b7c78b95", GeoSite: "925d1d5de5427626382bad46b55d21be1b748a75"}
	fetch := func(_ context.Context, s Source) ([]byte, error) {
		name := "dlc.dat"
		if s.Kind == GeoIP {
			name = "fi.dat"
		}
		return os.ReadFile(filepath.Join(filepath.Dir(core), name))
	}
	candidate, err := source.Prepare(testContext, selection, revisions, fetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	old, err := source.Commit(testContext, candidate, "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := archiveAPI(t, source).ExportArchive(testContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 2<<20 {
		t.Fatal("archive exceeds bounded import format")
	}
	target, root := testStore(t)
	checked := false
	candidate, err = archiveAPI(t, target).PrepareArchive(testContext, data, func(ctx context.Context, snapshot Snapshot) error {
		rules := []any{}
		for _, asset := range snapshot.Assets {
			refs := []string{}
			for _, tag := range asset.Tags {
				refs = append(refs, "ext:"+asset.Path+":"+tag)
			}
			key := "domain"
			if asset.Kind == GeoIP {
				key = "ip"
			}
			rules = append(rules, map[string]any{"type": "field", key: refs, "outboundTag": "deny"})
		}
		cfg, err := json.Marshal(map[string]any{"outbounds": []any{map[string]any{"tag": "deny", "protocol": "blackhole"}}, "routing": map[string]any{"rules": rules}})
		if err != nil {
			return err
		}
		path := filepath.Join(t.TempDir(), "config.json")
		if err = os.WriteFile(path, cfg, 0600); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, core, "run", "-test", "-config", path)
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+root)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("actual core rejected archived native files: %w: %s", err, output)
		}
		checked = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := target.Commit(testContext, candidate, "")
	if err != nil {
		t.Fatal(err)
	}
	if !checked || len(next.Assets) != len(old.Assets) {
		t.Fatal("native validation missing")
	}
	for i, asset := range next.Assets {
		if asset.SHA256 != old.Assets[i].SHA256 || asset.Bytes != old.Assets[i].Bytes {
			t.Fatal("native payload changed")
		}
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.Load(testContext)
	if err != nil || again.Revision != next.Revision {
		t.Fatal("offline native archive reopen failed", err)
	}
	t.Logf("native GeoIP+GeoSite archive bytes=%d; core validated all selected tags; hashes unchanged", len(data))
}
