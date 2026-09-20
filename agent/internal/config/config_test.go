package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig puts contents into a config.json inside a fresh temp directory and
// returns its path.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

const validConfig = `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "log_level": "debug",
  "sources": {
    "clock": {
      "enabled": true,
      "interval_ms": 1000,
      "sleep_start": "23:30",
      "sleep_end": "07:00"
    },
    "telemetry": {
      "enabled": false,
      "interval_ms": 2000
    }
  }
}`

func TestLoadReadsEveryField(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load returned an error for a valid config: %v", err)
	}

	if cfg.Listen != "127.0.0.1:9000" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, "127.0.0.1:9000")
	}
	if cfg.Token != "s3cret" {
		t.Errorf("Token = %q, want %q", cfg.Token, "s3cret")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(cfg.Sources))
	}

	clock := cfg.Sources["clock"]
	if !clock.Enabled {
		t.Error("clock source should be enabled")
	}
	if got, want := clock.Interval(), time.Second; got != want {
		t.Errorf("clock Interval() = %v, want %v", got, want)
	}
	if cfg.Sources["telemetry"].Enabled {
		t.Error("telemetry source should be disabled")
	}
}

func TestLoadPreservesUnknownPerSourceSettings(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load returned an error for a valid config: %v", err)
	}

	// A source owns its own settings keys. The config package must hand them
	// back untouched so a source can decode what it needs without every new
	// source key requiring a change here.
	var settings struct {
		SleepStart string `json:"sleep_start"`
		SleepEnd   string `json:"sleep_end"`
	}
	if err := json.Unmarshal(cfg.Sources["clock"].Settings, &settings); err != nil {
		t.Fatalf("decoding preserved clock settings: %v", err)
	}
	if settings.SleepStart != "23:30" || settings.SleepEnd != "07:00" {
		t.Errorf("settings = %+v, want sleep_start 23:30 and sleep_end 07:00", settings)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{"token":"s3cret","sources":{}}`))
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}

	if cfg.Listen != DefaultListen {
		t.Errorf("Listen = %q, want the default %q", cfg.Listen, DefaultListen)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %q, want the default %q", cfg.LogLevel, DefaultLogLevel)
	}
}

func TestLoadAllowsDisabledSourceWithNoInterval(t *testing.T) {
	// A disabled source is never polled, so its interval is irrelevant and must
	// not block startup.
	_, err := Load(writeConfig(t, `{"token":"s3cret","sources":{"clock":{"enabled":false}}}`))
	if err != nil {
		t.Fatalf("a disabled source with no interval should load, got: %v", err)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load should refuse to start when the config file is absent")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the missing path, got: %v", err)
	}
}

func TestLoadRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		wantIn   string
	}{
		{
			name:     "malformed json",
			contents: `{"token": "s3cret",}`,
			wantIn:   "config.json",
		},
		{
			name:     "empty token",
			contents: `{"token":"","sources":{}}`,
			wantIn:   "token",
		},
		{
			name:     "missing token",
			contents: `{"sources":{}}`,
			wantIn:   "token",
		},
		{
			name:     "whitespace only token",
			contents: `{"token":"   ","sources":{}}`,
			wantIn:   "token",
		},
		{
			name:     "example placeholder token",
			contents: `{"token":"` + PlaceholderToken + `","sources":{}}`,
			wantIn:   "token",
		},
		{
			name:     "example placeholder token with padding",
			contents: `{"token":"  ` + PlaceholderToken + ` ","sources":{}}`,
			wantIn:   "token",
		},
		{
			name:     "listen without a port",
			contents: `{"token":"s3cret","listen":"0.0.0.0","sources":{}}`,
			wantIn:   "listen",
		},
		{
			name:     "listen with a non numeric port",
			contents: `{"token":"s3cret","listen":"0.0.0.0:http","sources":{}}`,
			wantIn:   "listen",
		},
		{
			name:     "listen with an out of range port",
			contents: `{"token":"s3cret","listen":"0.0.0.0:70000","sources":{}}`,
			wantIn:   "listen",
		},
		{
			name:     "unknown log level",
			contents: `{"token":"s3cret","log_level":"chatty","sources":{}}`,
			wantIn:   "log_level",
		},
		{
			name:     "enabled source with zero interval",
			contents: `{"token":"s3cret","sources":{"clock":{"enabled":true,"interval_ms":0}}}`,
			wantIn:   "interval_ms",
		},
		{
			name:     "enabled source with negative interval",
			contents: `{"token":"s3cret","sources":{"clock":{"enabled":true,"interval_ms":-5}}}`,
			wantIn:   "interval_ms",
		},
		{
			name:     "unknown top level key",
			contents: `{"token":"s3cret","sources":{},"listn":"0.0.0.0:8765"}`,
			wantIn:   "listn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.contents))
			if err == nil {
				t.Fatal("Load should have refused this config")
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("error should mention %q, got: %v", tt.wantIn, err)
			}
		})
	}
}

// The example file is what a fresh clone copies, so it must be refused until
// the token is replaced. Reading the real file keeps PlaceholderToken and
// config.example.json from drifting apart.
func TestLoadRejectsTheShippedExampleUntouched(t *testing.T) {
	_, err := Load(filepath.Join("..", "..", "config.example.json"))
	if err == nil {
		t.Fatal("config.example.json loaded as is, so a fresh clone would serve with a public token")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error should mention the token, got: %v", err)
	}
}

func TestSourceNamesAreSorted(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}

	// Map iteration order is random. Stable output matters for logs and for
	// /health, so the config exposes a sorted name list.
	got := cfg.SourceNames()
	want := []string{"clock", "telemetry"}
	if len(got) != len(want) {
		t.Fatalf("SourceNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SourceNames() = %v, want %v", got, want)
		}
	}
}

func TestLoadAcceptsAValidAccentColor(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "accent_color": "#4e9eea",
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load returned an error for a valid accent_color: %v", err)
	}
	if cfg.AccentColor != "#4e9eea" {
		t.Errorf("AccentColor = %q, want %q", cfg.AccentColor, "#4e9eea")
	}
}

func TestLoadAllowsAnAbsentAccentColor(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if cfg.AccentColor != "" {
		t.Errorf("AccentColor = %q, want empty when unset", cfg.AccentColor)
	}
}

func TestLoadRejectsAMalformedAccentColor(t *testing.T) {
	cases := []string{"blue", "#fff", "#gggggg", "4e9eea"}
	for _, bad := range cases {
		_, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "accent_color": "`+bad+`",
  "sources": {}
}`))
		if err == nil {
			t.Errorf("Load accepted invalid accent_color %q, want an error", bad)
		}
	}
}

func TestLoadAcceptsValidHiddenFaces(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "hidden_faces": ["calendar", "telemetry"],
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load returned an error for valid hidden_faces: %v", err)
	}
	if len(cfg.HiddenFaces) != 2 || cfg.HiddenFaces[0] != "calendar" || cfg.HiddenFaces[1] != "telemetry" {
		t.Errorf("HiddenFaces = %v, want [calendar telemetry]", cfg.HiddenFaces)
	}
}

func TestLoadRejectsAnUnknownFaceName(t *testing.T) {
	_, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "hidden_faces": ["calendar", "weather"],
  "sources": {}
}`))
	if err == nil {
		t.Fatal("Load accepted an unknown face name, want an error")
	}
}

func TestLoadRejectsHidingClock(t *testing.T) {
	_, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "hidden_faces": ["clock"],
  "sources": {}
}`))
	if err == nil {
		t.Fatal("Load accepted hiding the clock face, want an error: it is the panel's non-negotiable fallback")
	}
}

func TestLoadRejectsHidingEveryFace(t *testing.T) {
	all := `["` + strings.Join(KnownFaces, `", "`) + `"]`
	_, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "hidden_faces": `+all+`,
  "sources": {}
}`))
	if err == nil {
		t.Fatal("Load accepted hiding every face, want an error: the panel would show nothing")
	}
}

func TestLoadDefaultsClockStyleToDigital(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClockStyle != "digital" {
		t.Errorf("ClockStyle = %q, want the default of %q", cfg.ClockStyle, "digital")
	}
}

func TestLoadAcceptsAnalogueClockStyle(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "clock_style": "analogue",
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load returned an error for a valid clock_style: %v", err)
	}
	if cfg.ClockStyle != "analogue" {
		t.Errorf("ClockStyle = %q, want %q", cfg.ClockStyle, "analogue")
	}
}

func TestLoadRejectsAnUnknownClockStyle(t *testing.T) {
	_, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "clock_style": "roman-numerals",
  "sources": {}
}`))
	if err == nil {
		t.Fatal("Load accepted an unknown clock_style, want an error")
	}
}

func TestLoadDefaultsHideNextEventToFalse(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HideNextEvent {
		t.Error("HideNextEvent = true, want the default of false")
	}
}

func TestLoadAcceptsHideNextEventTrue(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
  "listen": "127.0.0.1:9000",
  "token": "s3cret",
  "hide_next_event": true,
  "sources": {}
}`))
	if err != nil {
		t.Fatalf("Load returned an error for a valid hide_next_event: %v", err)
	}
	if !cfg.HideNextEvent {
		t.Error("HideNextEvent = false, want true")
	}
}

func TestSaveRoundTripsEveryFieldIncludingSourceSettings(t *testing.T) {
	path := writeConfig(t, validConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cfg.AccentColor = "#7c83fd"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if reloaded.AccentColor != "#7c83fd" {
		t.Errorf("AccentColor after round trip = %q, want %q", reloaded.AccentColor, "#7c83fd")
	}
	if reloaded.Listen != cfg.Listen || reloaded.Token != cfg.Token {
		t.Errorf("Listen/Token did not round trip: got %+v", reloaded)
	}
	clock, ok := reloaded.Sources["clock"]
	if !ok {
		t.Fatal(`Save dropped the "clock" source`)
	}
	if !clock.Enabled || clock.IntervalMS != 1000 {
		t.Errorf("clock source did not round trip: %+v", clock)
	}
	// sleep_start/sleep_end are source-specific keys with no field on Source;
	// they must survive only because Settings is written back verbatim.
	var clockSettings struct {
		SleepStart string `json:"sleep_start"`
	}
	if err := json.Unmarshal(clock.Settings, &clockSettings); err != nil {
		t.Fatalf("unmarshalling round-tripped clock settings: %v", err)
	}
	if clockSettings.SleepStart != "23:30" {
		t.Errorf("sleep_start = %q, want %q, source-specific settings did not survive Save", clockSettings.SleepStart, "23:30")
	}
}

func TestSaveWritesAtomically(t *testing.T) {
	path := writeConfig(t, validConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("a temp file %q was left behind, Save should clean up on success", e.Name())
		}
	}
}

// A source resolves relative file settings against the directory config.json is
// in, not the working directory, so it needs to know where that is.
func TestLoadTellsEverySourceWhereTheConfigLives(t *testing.T) {
	path := writeConfig(t, `{"token":"s3cret","sources":{"clock":{"enabled":true,"interval_ms":1000},"telemetry":{"enabled":false}}}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := filepath.Dir(path)
	for _, name := range cfg.SourceNames() {
		if got := cfg.Sources[name].Dir; got != want {
			t.Errorf("source %q Dir = %q, want %q", name, got, want)
		}
	}
}

func TestLoadMakesARelativeConfigPathAbsolute(t *testing.T) {
	path := writeConfig(t, `{"token":"s3cret","sources":{"clock":{"enabled":true,"interval_ms":1000}}}`)
	dir := filepath.Dir(path)
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	cfg, err := Load("config.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.Sources["clock"].Dir
	if !filepath.IsAbs(got) {
		t.Errorf("Dir = %q, want an absolute path so a later change of working directory cannot move it", got)
	}
}

func TestSaveDoesNotWriteTheDirBack(t *testing.T) {
	path := writeConfig(t, `{"token":"s3cret","sources":{"clock":{"enabled":true,"interval_ms":1000}}}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), filepath.Dir(path)) || strings.Contains(strings.ToLower(string(data)), `"dir"`) {
		t.Errorf("the config directory leaked into config.json:\n%s", data)
	}
}
