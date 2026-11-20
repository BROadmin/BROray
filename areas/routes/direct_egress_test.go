package routes

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPolicyDirectEgressResolution(t *testing.T) {
	env := newEnv(t)
	calls := 0
	env.e.directInterface = func() (string, error) { calls++; return "eth3", nil }
	for _, state := range []string{`{"schemaVersion":1,"rules":[]}`, policyTestDomain, strings.ReplaceAll(policyTestDomain, `"enabled":true,"action":"deny"`, `"enabled":false,"action":"direct"`)} {
		writeTestPolicy(t, env.e, state)
		if _, err := env.e.configureRouting(context.Background(), []byte(policyTestBase)); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("default-off/deny resolved WAN")
	}
	writeTestPolicy(t, env.e, strings.ReplaceAll(policyTestDomain, `"action":"deny"`, `"action":"direct"`))
	got, err := env.e.configureRouting(context.Background(), []byte(policyTestBase))
	if err != nil || !strings.Contains(string(got), `"interface":"eth3"`) || calls != 1 {
		t.Fatal("DIRECT binding missing", string(got), err, calls)
	}
	expected := errors.New("WAN unavailable")
	env.e.directInterface = func() (string, error) { return "", expected }
	got, err = env.e.configureRouting(context.Background(), []byte(policyTestBase))
	if !errors.Is(err, expected) || got != nil {
		t.Fatal("unknown WAN did not fail closed", err)
	}
}
