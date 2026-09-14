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
		t.Fatalf("building test config: %v", err)
	}
	return cfg
}

// stubFactories stands in for the real factory table so that registry
// behaviour can be tested without depending on which sources happen to exist.
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
		t.Fatalf("build returned an error: %v", err)
	}

	if len(built) != 1 {
		t.Fatalf("got %d sources, want only the enabled one", len(built))
	}
	if built[0].Name() != "alpha" {
		t.Errorf("built source is %q, want alpha", built[0].Name())
	}
}

func TestBuildAppliesTheConfiguredInterval(t *testing.T) {
	cfg := testConfig(t, `{"sources": {"alpha": {"enabled": true, "interval_ms": 2500}}}`)

	built, err := build(cfg, stubFactories("alpha"))
	if err != nil {
		t.Fatalf("build returned an error: %v", err)
	}

	if got, want := built[0].Interval(), 2500*time.Millisecond; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestBuildRejectsAnUnknownSourceName(t *testing.T) {
	// A source named in config with no implementation behind it is a
	// misconfiguration. Starting anyway would leave the user staring at a face
	// that never populates, with nothing saying why.
	cfg := testConfig(t, `{"sources": {"spotify": {"enabled": true, "interval_ms": 1000}}}`)

	_, err := build(cfg, stubFactories("alpha"))
	if err == nil {
		t.Fatal("build should reject a source name it cannot satisfy")
	}
	if !strings.Contains(err.Error(), "spotify") {
		t.Errorf("error should name the unknown source, got: %v", err)
	}
}

func TestBuildRejectsAnUnknownSourceEvenWhenDisabled(t *testing.T) {
	// A disabled unknown source is still a typo worth catching. It is the most
	// likely way a working source gets silently switched off.
	cfg := testConfig(t, `{"sources": {"alpah": {"enabled": false, "interval_ms": 1000}}}`)

	_, err := build(cfg, stubFactories("alpha"))
	if err == nil {
		t.Fatal("build should reject an unknown source name even when it is disabled")
	}
	if !strings.Contains(err.Error(), "alpah") {
		t.Errorf("error should name the unknown source, got: %v", err)
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
		t.Fatal("build should surface a source that cannot be constructed")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should name the source that failed, got: %v", err)
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
		t.Fatalf("build returned an error: %v", err)
	}

	want := []string{"alpha", "zulu"}
	for i, name := range want {
		if built[i].Name() != name {
			t.Fatalf("source order = %v, want %v", buildNames(built), want)
		}
	}
}

func TestBuildKnowsTheClockSource(t *testing.T) {
	// The real factory table, exercised through the exported entry point.
	cfg := testConfig(t, `{"sources": {"clock": {"enabled": true, "interval_ms": 1000}}}`)

	built, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	if len(built) != 1 || built[0].Name() != "clock" {
		t.Fatalf("built %v, want a single clock source", buildNames(built))
	}

	value, err := built[0].Poll(context.Background())
	if err != nil {
		t.Fatalf("polling the built clock source: %v", err)
	}
	if value == nil {
		t.Error("the clock source returned no value")
	}
}

func TestBuildRejectsABadClockWindow(t *testing.T) {
	// The clock source validates its sleep window at construction, so a bad
	// window stops the agent rather than failing on every poll forever.
	cfg := testConfig(t, `{"sources": {"clock": {"enabled": true, "interval_ms": 1000, "sleep_start": "25:00"}}}`)

	_, err := Build(cfg)
	if err == nil {
		t.Fatal("Build should surface an unparseable sleep window")
	}
	if !strings.Contains(err.Error(), "clock") {
		t.Errorf("error should name the source that failed, got: %v", err)
	}
}

func buildNames(built []Source) []string {
	out := make([]string, len(built))
	for i, s := range built {
		out[i] = s.Name()
	}
	return out
}
