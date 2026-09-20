package tray

import (
	"strings"
	"testing"
)

func TestTheTitleIsPlainWhenAutostartIsAvailable(t *testing.T) {
	if got := autostartTitle(""); got != "Start with Windows" {
		t.Errorf("title = %q", got)
	}
}

func TestTheTitleCarriesTheReasonWhenUnavailable(t *testing.T) {
	got := autostartTitle("this copy is running from a temporary folder")

	if !strings.HasPrefix(got, "Start with Windows") || !strings.Contains(got, "temporary folder") {
		t.Errorf("title = %q, want the label and the reason", got)
	}
}
