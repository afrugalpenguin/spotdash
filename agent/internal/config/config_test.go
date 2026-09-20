package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig puts contents into a config.json in a fresh temp directory and
// returns its path.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
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
		t.Fatalf("Load: %v", err)
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
		t.Error("clock Enabled = false, want true")
	}
	if got, want := clock.Interval(), time.Second; got != want {
		t.Errorf("clock Interval() = %v, want %v", got, want)
	}
	if cfg.Sources["telemetry"].Enabled {
		t.Error("telemetry Enabled = true, want false")
	}
}

func TestLoadPreservesUnknownPerSourceSettings(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A source owns its settings keys, so they must come back untouched.
	var settings struct {
		SleepStart string `json:"sleep_start"`
		SleepEnd   string `json:"sleep_end"`
	}
	if err := json.Unmarshal(cfg.Sources["clock"].Settings, &settings); err != nil {
		t.Fatalf("Unmarshal clock settings: %v", err)
	}
	if settings.SleepStart != "23:30" || settings.SleepEnd != "07:00" {
		t.Errorf("settings = %+v, want sleep_start 23:30 and sleep_end 07:00", settings)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{"token":"s3cret","sources":{}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Listen != DefaultListen {
		t.Errorf("Listen = %q, want the default %q", cfg.Listen, DefaultListen)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %q, want the default %q", cfg.LogLevel, DefaultLogLevel)
	}
}

func TestLoadAllowsDisabledSourceWithNoInterval(t *testing.T) {
	// A disabled source is never polled, so its interval does not matter.
	_, err := Load(writeConfig(t, `{"token":"s3cret","sources":{"clock":{"enabled":false}}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load of a missing file succeeded, want error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want it to name the path", err)
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
				t.Fatal("Load succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("want error containing %q, got %v", tt.wantIn, err)
			}
		})
	}
}

// A fresh clone copies the example file, so it must be refused until the token
// is replaced. This also keeps PlaceholderToken and the file in step.
func TestLoadRejectsTheShippedExampleUntouched(t *testing.T) {
	_, err := Load(filepath.Join("..", "..", "config.example.json"))
	if err == nil {
		t.Fatal("Load of config.example.json succeeded, want error")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error = %v, want it to mention the token", err)
	}
}

func TestSourceNamesAreSorted(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Map order is random, so the names must come back sorted.
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
		t.Fatalf("Load: %v", err)
	}
	if cfg.AccentColor != "#4e9eea" {
		t.Errorf("AccentColor = %q, want %q", cfg.AccentColor, "#4e9eea")
	}
}

func TestLoadAllowsAnAbsentAccentColor(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
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
			t.Errorf("Load accent_color %q succeeded, want error", bad)
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
		t.Fatalf("Load: %v", err)
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
		t.Fatal("Load with an unknown face succeeded, want error")
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
		t.Fatal("Load hiding the clock face succeeded, want error")
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
		t.Fatal("Load hiding every face succeeded, want error")
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
		t.Errorf("ClockStyle = %q, want %q", cfg.ClockStyle, "digital")
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
		t.Fatalf("Load: %v", err)
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
		t.Fatal("Load with an unknown clock_style succeeded, want error")
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
		t.Error("HideNextEvent = true, want false")
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
		t.Fatalf("Load: %v", err)
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
		t.Errorf("Listen/Token after Save: got %+v", reloaded)
	}
	clock, ok := reloaded.Sources["clock"]
	if !ok {
		t.Fatal(`clock source missing after Save`)
	}
	if !clock.Enabled || clock.IntervalMS != 1000 {
		t.Errorf("clock after Save = %+v, want enabled with interval 1000", clock)
	}
	// sleep_start has no field on Source and survives only in Settings.
	var clockSettings struct {
		SleepStart string `json:"sleep_start"`
	}
	if err := json.Unmarshal(clock.Settings, &clockSettings); err != nil {
		t.Fatalf("Unmarshal clock settings: %v", err)
	}
	if clockSettings.SleepStart != "23:30" {
		t.Errorf("sleep_start = %q, want %q", clockSettings.SleepStart, "23:30")
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
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file %q after Save", e.Name())
		}
	}
}

// A source resolves relative file settings against the config.json directory.
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
		t.Errorf("Dir = %q, want an absolute path", got)
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
		t.Errorf("Dir leaked into config.json:\n%s", data)
	}
}
