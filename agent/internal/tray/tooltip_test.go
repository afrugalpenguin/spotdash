package tray

import (
	"strings"
	"testing"
)

func TestTheTooltipCarriesTheAgentVersion(t *testing.T) {
	got := tooltip("v0.1.0-rc1")

	if !strings.Contains(got, "v0.1.0-rc1") {
		t.Errorf("tooltip = %q, want it to contain v0.1.0-rc1", got)
	}
}
