package servers

import (
	"os"
	"testing"
)

// This is configuration validation with the pinned official core, not mask
// traffic against a remote peer. It runs only in the explicit offline core VM.
func TestFinalMaskOfficialCoreConfigurations(t *testing.T) {
	if os.Getenv("BRORAY_XRAY_BIN") == "" {
		t.Skip("official core configuration check requires BRORAY_XRAY_BIN")
	}
	for _, fixture := range finalMaskSchemaFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			network := "raw"
			if fixture.lane == "udp" {
				network = "kcp"
			}
			r, err := finalMaskImport(t, finalMaskFixtureValue(t, fixture), network)
			if err != nil {
				t.Fatal(err)
			}
			coverageRoundTrip(t, r, true)
		})
	}
}
