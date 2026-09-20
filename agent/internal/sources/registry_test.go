package sources

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

func testConfig(t *testing.T, contents string) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	if err := json.Unmarshal([]byte(contents), cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	return cfg
}

// stubFactories stands in for the real factory table.
func stubFactories(names ...string) map[string]Factory {
	out := make(map[string]Factory, len(names))
	for _, name := range names {
		name := name
		out[name] = func(cfg config.Source) (Source, error) {
			return &fakeSource{name: name, interval: cfg.Interval(), poll: alwaysReturns(name)}, nil
		}
	}
	return out
}

func TestBuildReturnsOnlyEnabledSources(t *testing.T) {
	cfg := testConfig(t, `{
		"sources": {
			"alpha": {"enabled": true, "interval_ms": 1000},
			"beta": {"enabled": false, "interval_ms": 2000}
		}
	}`)

	built, err := build(cfg, stubFactories("alpha", "beta"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(built) != 1 {
		t.Fatalf("got %d sources, want 1", len(built))
	}
	if built[0].Name() != "alpha" {
		t.Errorf("name = %q, want alpha", built[0].Name())
	}
}

func TestBuildAppliesTheConfiguredInterval(t *testing.T) {
	cfg := testConfig(t, `{"sources": {"alpha": {"enabled": true, "interval_ms": 2500}}}`)

	built, err := build(cfg, stubFactories("alpha"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if got, want := built[0].Interval(), 2500*time.Millisecond; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestBuildRejectsAnUnknownSourceName(t *testing.T) {
	cfg := testConfig(t, `{"sources": {"spotify": {"enabled": true, "interval_ms": 1000}}}`)

	_, err := build(cfg, stubFactories("alpha"))
	if err == nil {
		t.Fatal("build accepted an unknown source name")
	}
	if !strings.Contains(err.Error(), "spotify") {
		t.Errorf("error = %v, want it to name spotify", err)
	}
}

func TestBuildRejectsAnUnknownSourceEvenWhenDisabled(t *testing.T) {
	// A typo in a disabled block is still worth catching.
	cfg := testConfig(t, `{"sources": {"alpah": {"enabled": false, "interval_ms": 1000}}}`)

	_, err := build(cfg, stubFactories("alpha"))
	if err == nil {
		t.Fatal("build accepted a disabled unknown source")
	}
	if !strings.Contains(err.Error(), "alpah") {
		t.Errorf("error = %v, want it to name alpah", err)
	}
}

func TestBuildReportsSourceConstructionFailure(t *testing.T) {
	cfg := testConfig(t, `{"sources": {"alpha": {"enabled": true, "interval_ms": 1000}}}`)
	factories := map[string]Factory{
		"alpha": func(config.Source) (Source, error) {
			return nil, errTestConstruction
		},
	}

	_, err := build(cfg, factories)
	if err == nil {
		t.Fatal("build hid a construction failure")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error = %v, want it to name alpha", err)
	}
}

func TestBuiltSourcesAreInAStableOrder(t *testing.T) {
	cfg := testConfig(t, `{
		"sources": {
			"zulu": {"enabled": true, "interval_ms": 2000},
			"alpha": {"enabled": true, "interval_ms": 1000}
		}
	}`)

	built, err := build(cfg, stubFactories("alpha", "zulu"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	want := []string{"alpha", "zulu"}
	for i, name := range want {
		if built[i].Name() != name {
			t.Fatalf("source order = %v, want %v", buildNames(built), want)
		}
	}
}

func TestBuildKnowsTheClockSource(t *testing.T) {
	// Uses the real factory table.
	cfg := testConfig(t, `{"sources": {"clock": {"enabled": true, "interval_ms": 1000}}}`)

	built, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(built) != 1 || built[0].Name() != "clock" {
		t.Fatalf("built %v, want [clock]", buildNames(built))
	}

	value, err := built[0].Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if value == nil {
		t.Error("Poll returned no value")
	}
}

func TestBuildRejectsABadClockWindow(t *testing.T) {
	// The clock validates its sleep window at construction.
	cfg := testConfig(t, `{"sources": {"clock": {"enabled": true, "interval_ms": 1000, "sleep_start": "25:00"}}}`)

	_, err := Build(cfg)
	if err == nil {
		t.Fatal("Build accepted an unparseable sleep window")
	}
	if !strings.Contains(err.Error(), "clock") {
		t.Errorf("error = %v, want it to name clock", err)
	}
}

func buildNames(built []Source) []string {
	out := make([]string, len(built))
	for i, s := range built {
		out[i] = s.Name()
	}
	return out
}
