package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/modules/geodata"
	"golang.org/x/sys/unix"
)

const policyTestBase = ` {"inbounds":[{"tag":"socks","protocol":"socks"}],"outbounds":[{"tag":"proxy","protocol":"blackhole"}]} `
const policyTestDomain = `{"schemaVersion":1,"rules":[{"id":"own","name":"Own","enabled":true,"action":"deny","domains":["example.com"]}]}`

func writeTestPolicy(t *testing.T, e *engine, data string) {
	t.Helper()
	if err := os.WriteFile(e.p("route-policy.v1.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSavedPolicyDomainAndDefaultOff(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	for _, state := range []string{"", `{"schemaVersion":1,"rules":[]}`, `{"schemaVersion":1,"rules":[{"id":"geo","enabled":false,"action":"deny","countryCodes":["fi"]}]}`} {
		if state != "" {
			writeTestPolicy(t, env.e, state)
		}
		got, err := env.e.configureRouting(ctx, []byte(policyTestBase))
		if err != nil || string(got) != policyTestBase {
			t.Fatalf("default off changed base: %s %v", got, err)
		}
	}
	writeTestPolicy(t, env.e, policyTestDomain)
	got, err := env.e.configureRouting(ctx, []byte(policyTestBase))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(got, &config); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(config["routing"], []byte("IPOnDemand")) || !bytes.Contains(config["routing"], []byte("broray-deny")) || !bytes.Contains(config["routing"], []byte("domain:example.com")) {
		t.Fatalf("saved domain policy missing: %s", got)
	}
}

func TestSavedPolicyRefusesInvalidState(t *testing.T) {
	for _, state := range []string{
		``, `null`, `{}`, `{"schemaVersion":2,"rules":[]}`, `{"schemaVersion":1,"rules":null}`,
		`{"schemaVersion":1,"rules":[],"unknown":true}`, `{"schemaVersion":1,"rules":[]} {}`,
		`{"schemaVersion":1,"rules":[{"id":"x","enabled":false,"action":"allow"}]}`,
		`{"schemaVersion":1,"rules":[],"geoRevision":"bad"}`,
		`{"schemaVersion":1,"rules":[{"id":"x","enabled":true,"action":"deny"}]}`,
		strings.Repeat(" ", (4<<20)+1),
	} {
		env := newEnv(t)
		writeTestPolicy(t, env.e, state)
		if _, err := env.e.configureRouting(context.Background(), []byte(policyTestBase)); err == nil {
			t.Fatalf("accepted invalid policy (%d bytes)", len(state))
		}
	}
}

func TestSavedPolicyRefusesUnsafePaths(t *testing.T) {
	for _, kind := range []string{"leaf-symlink", "parent-symlink", "fifo", "directory", "missing-root"} {
		t.Run(kind, func(t *testing.T) {
			env := newEnv(t)
			leaf := env.e.p("route-policy.v1.json")
			foreign := filepath.Join(t.TempDir(), "foreign.json")
			if err := os.WriteFile(foreign, []byte(policyTestDomain), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "leaf-symlink":
				err = os.Symlink(foreign, leaf)
			case "parent-symlink":
				dir := t.TempDir()
				err = os.Symlink(env.e.root, filepath.Join(dir, "alias"))
				env.e.root = filepath.Join(dir, "alias")
			case "fifo":
				err = unix.Mkfifo(leaf, 0600)
			case "directory":
				err = os.Mkdir(leaf, 0700)
			case "missing-root":
				env.e.root = filepath.Join(t.TempDir(), "absent")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := env.e.configureRouting(context.Background(), []byte(policyTestBase)); err == nil {
				t.Fatal("unsafe policy path accepted")
			}
			got, err := os.ReadFile(foreign)
			if err != nil || string(got) != policyTestDomain {
				t.Fatal("foreign file changed")
			}
		})
	}
}

func TestSavedPolicyGeoSnapshotBinding(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	policy := `{"schemaVersion":1,"geoRevision":"REV","rules":[{"id":"country","enabled":true,"action":"deny","countryCodes":["fi"]}]}`
	writeTestPolicy(t, env.e, strings.ReplaceAll(policy, "REV", strings.Repeat("a", 64)))
	if _, err := env.e.configureRouting(ctx, []byte(policyTestBase)); err == nil {
		t.Fatal("missing geo store accepted")
	}
	root := filepath.Join(env.d.Root.Runtime(), geodata.AssetDirectory)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := geodata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Native GeoIPList FI, 1.2.3.0/24; validation here is an integration fixture,
	// actual Xray traffic and core validation are covered by module tests.
	fixture := []byte{10, 14, 10, 2, 'F', 'I', 18, 8, 10, 4, 1, 2, 3, 0, 16, 24}
	candidate, err := store.Prepare(ctx, geodata.Selection{CountryCodes: []string{"fi"}}, geodata.Revisions{GeoIP: strings.Repeat("a", 40)}, func(context.Context, geodata.Source) ([]byte, error) { return fixture, nil }, func(context.Context, geodata.Snapshot) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Commit(ctx, candidate, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.e.configureRouting(ctx, []byte(policyTestBase)); err == nil {
		t.Fatal("wrong geo revision accepted")
	}
	writeTestPolicy(t, env.e, strings.ReplaceAll(policy, "REV", snapshot.Revision))
	got, err := env.e.configureRouting(ctx, []byte(policyTestBase))
	if err != nil || !bytes.Contains(got, []byte("ext:broray-geodata/"+snapshot.Assets[0].Path+":fi")) {
		t.Fatalf("geo config: %s %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(root, snapshot.Assets[0].Path), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := env.e.configureRouting(ctx, []byte(policyTestBase)); err == nil {
		t.Fatal("corrupt geo asset accepted")
	}
}

func TestSavedPolicySetupRegistersDespitePrepareFailure(t *testing.T) {
	env := newEnv(t)
	env.d.Maintenance = true
	if err := os.Rename(env.e.p("state"), env.e.p("state-held-for-test")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.e.p("state"), []byte("blocks required directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := setup(env.d); err == nil {
		t.Fatal("fixture did not cause prepare failure")
	}
	if env.d.Svc.RoutesConfigureXray == nil {
		t.Fatal("routes callback not registered")
	}
	writeTestPolicy(t, env.e, "invalid")
	if _, err := env.d.Svc.ConfigureRouting(context.Background(), []byte(policyTestBase)); err == nil {
		t.Fatal("bad policy silently ignored after setup")
	}
}

func TestSavedPolicyCancellation(t *testing.T) {
	env := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := env.e.configureRouting(ctx, []byte(policyTestBase)); err == nil {
		t.Fatal("canceled config accepted")
	}
}

func TestSavedPolicyRefusesAmbiguousEnabled(t *testing.T) {
	for name, state := range map[string]string{
		"duplicate-rules":   `{"schemaVersion":1,"rules":[{"id":"deny","enabled":true,"action":"deny","domains":["example.com"]}],"rules":[]}`,
		"duplicate-enabled": `{"schemaVersion":1,"rules":[{"id":"deny","enabled":true,"enabled":false,"action":"deny","domains":["example.com"]}]}`,
		"missing-enabled":   `{"schemaVersion":1,"rules":[{"id":"deny","action":"deny","domains":["example.com"]}]}`,
		"null-enabled":      `{"schemaVersion":1,"rules":[{"id":"deny","enabled":null,"action":"deny","domains":["example.com"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			env := newEnv(t)
			writeTestPolicy(t, env.e, state)
			if _, err := env.e.configureRouting(context.Background(), []byte(policyTestBase)); err == nil {
				t.Fatal("ambiguous saved deny silently disabled")
			}
		})
	}
}
