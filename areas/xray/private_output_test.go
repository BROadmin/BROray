package xray

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestPrivateTransportValidationOutput(t *testing.T) {
	h := newHarness(t, true)
	cfg := []byte(`{"outbounds":[{"protocol":"masque","settings":{"address":"example.test","port":443},"streamSettings":{"network":"masque"}}]}`)
	if err := os.WriteFile(h.d.Root.XrayConfig(), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.d.Root.XrayBinary(), []byte("#!/bin/sh\necho synthetic-private-token-from-core\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	output, ok := h.s.runTest(context.Background(), h.d.Root.XrayBinary(), h.d.Root.XrayConfig())
	if ok || output == "" || strings.Contains(output, "synthetic-private") {
		t.Fatal("public validation output exposes cloud credentials")
	}
}
