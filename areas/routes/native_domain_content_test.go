package routes

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BROadmin/broray/modules/geodata"
)

func TestNativeDomainSubsetDoesNotWidenMatchers(t *testing.T) {
	content := nativeDomainSubset([]geodata.DomainMatcher{
		{Kind: geodata.MatchSuffix, Value: "Video.Example"},
		{Kind: geodata.MatchSuffix, Value: "video.example"},
		{Kind: geodata.MatchExact, Value: "exact.example"},
		{Kind: geodata.MatchRegexp, Value: "^cdn[0-9]+\\.example$"},
		{Kind: geodata.MatchKeyword, Value: "keyword"},
		{Kind: geodata.MatchSuffix, Value: "*.invalid.example"},
	})
	if !reflect.DeepEqual(content.Domains, []string{"video.example"}) {
		t.Fatal("unrepresentable entries became native suffixes", content.Domains)
	}
	want := []skippedDomainMatcher{
		{Kind: "exact", Value: "exact.example", Reason: "keenetic-includes-subdomains"},
		{Kind: "regexp", Value: "^cdn[0-9]+\\.example$", Reason: "unsupported-regexp"},
		{Kind: "keyword", Value: "keyword", Reason: "unsupported-keyword"},
		{Kind: "suffix", Value: "*.invalid.example", Reason: "invalid-native-name"},
	}
	if !reflect.DeepEqual(content.Skipped, want) {
		t.Fatal("skipped expressions not preserved", content.Skipped)
	}
}

func TestNativeDomainCatalogPinnedSource(t *testing.T) {
	path := os.Getenv("BRORAY_NATIVE_DOMAIN_TEST_SOURCE")
	if path == "" {
		core := os.Getenv("BRORAY_XRAY_BIN")
		if core == "" {
			t.Skip("BRORAY_XRAY_BIN and adjacent pinned data fixtures required")
		}
		path = filepath.Join(filepath.Dir(core), "dlc.dat")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash := fmt.Sprintf("%x", sha256.Sum256(data)); hash != "c449116a9ad4cf406a31c23ff254b94f4d341db0c2d5ad2924410daa62b4277b" {
		t.Fatal("pinned GeoSite fixture changed", hash)
	}
	presets := nativeDomainPresets()
	if len(presets) != len(builtins) || len(presets) != 12 {
		t.Fatal("ready route and domain catalogs differ", presets)
	}
	for i, preset := range presets {
		if preset.ID != builtins[i].ID || preset.Name != builtins[i].Name {
			t.Fatal("catalog names or ordering differ", preset, builtins[i].ID)
		}
		content, err := nativePresetContent(data, preset.ID)
		if err != nil || len(content.Domains) == 0 {
			t.Errorf("%s has no compatible source content: %v", preset.ID, err)
		}
	}
	env := newEnv(t)
	resolves := 0
	env.e.geoResolve = func(_ context.Context, kind geodata.Kind) (string, error) {
		resolves++
		if kind != geodata.GeoSite {
			t.Fatalf("unexpected source %q", kind)
		}
		return strings.Repeat("a", 40), nil
	}
	env.e.geoFetch = func(_ context.Context, source geodata.Source) ([]byte, error) {
		if !reflect.DeepEqual(source.Tags, []string{"openai"}) {
			t.Fatal("preview selected the wrong GeoSite tag", source.Tags)
		}
		return data, nil
	}
	const endpoint = "/api/routes/native-domain-preview.cgi"
	env.wantErr(env.callH("POST", endpoint, `{"id":"chatgpt"}`, nil), 403, "CSRF_REJECTED")
	env.wantErr(env.callH("POST", endpoint, `{"id":"discord"}`, policyHeaders), 400, "DOMAIN_PRESET_INVALID")
	if resolves != 0 {
		t.Fatal("invalid or unguarded request reached upstream")
	}
	preview := env.mustOK(env.callH("POST", endpoint, `{"id":"chatgpt"}`, policyHeaders))
	if preview["id"] != "chatgpt" || len(preview["domains"].([]any)) == 0 || len(preview["skipped"].([]any)) == 0 || resolves != 1 {
		t.Fatal("preview did not return compatible and omitted source entries")
	}
}
