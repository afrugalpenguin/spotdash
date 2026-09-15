// Package config loads and validates the agent configuration.
//
// Validation is fail-closed. Anything the agent cannot make sense of is a
// startup failure naming the offending key, never a silently applied guess.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Defaults applied when a key is absent.
const (
	DefaultListen   = "0.0.0.0:8765"
	DefaultLogLevel = "info"
)

var validLogLevels = []string{"debug", "info", "warn", "error"}

// KnownFaces are the faces the panel can show, by the title each face
// module in agent/web/faces exports. Kept here, not just in app.js, so a
// typo or a stale name in hidden_faces is a startup error rather than a
// setting that silently does nothing.
var KnownFaces = []string{"overview", "clock", "calendar", "spotify", "telemetry", "status"}

// ClockStyles are the shapes the clock face (standalone or the clock portion
// of overview) is allowed to draw in.
var ClockStyles = []string{"digital", "analogue"}

// accentColorPattern is the only shape accent_color is allowed to take: a
// 6-digit hex colour with its leading #, exactly what an
// <input type="color"> produces. Anything else is rejected rather than
// guessed at.
var accentColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Source is the per-source configuration block. Enabled and IntervalMS are
// common to every source; anything else a source needs stays in Settings for
// that source to decode itself.
type Source struct {
	Enabled    bool
	IntervalMS int
	Settings   json.RawMessage
}

// Interval is the poll period for this source.
func (s Source) Interval() time.Duration {
	return time.Duration(s.IntervalMS) * time.Millisecond
}

// UnmarshalJSON decodes the common fields and keeps the whole object so that
// source-specific keys survive. This is what lets a new source add settings
// without touching this package.
func (s *Source) UnmarshalJSON(data []byte) error {
	var common struct {
		Enabled    bool `json:"enabled"`
		IntervalMS int  `json:"interval_ms"`
	}
	if err := json.Unmarshal(data, &common); err != nil {
		return err
	}
	s.Enabled = common.Enabled
	s.IntervalMS = common.IntervalMS
	s.Settings = append(json.RawMessage(nil), data...)
	return nil
}

// MarshalJSON writes back exactly what was read: Settings already holds the
// complete original object for this source, enabled/interval_ms and every
// source-specific key alike, captured verbatim by UnmarshalJSON. Marshalling
// the struct fields individually here instead would both duplicate them
// under the wrong (capitalised, untagged) names and lose anything
// source-specific, which is exactly what Save must not do.
func (s Source) MarshalJSON() ([]byte, error) {
	if s.Settings == nil {
		return []byte("{}"), nil
	}
	return s.Settings, nil
}

// Config is the validated contents of config.json.
type Config struct {
	Listen   string `json:"listen"`
	Token    string `json:"token"`
	LogLevel string `json:"log_level"`
	// AccentColor is the panel's one accent colour, "#rrggbb". Optional: an
	// absent value keeps the built-in default the stylesheet ships with.
	// Changed from the tray's settings page rather than usually hand-edited,
	// which is why it round-trips through Save rather than only Load.
	AccentColor string `json:"accent_color,omitempty"`
	// HiddenFaces are face titles (see KnownFaces) left out of the tap
	// rotation. Optional: an absent or empty list shows every face, the
	// same as before this setting existed. Changed from the settings page.
	HiddenFaces []string `json:"hidden_faces,omitempty"`
	// ClockStyle is "digital" or "analogue", how the clock face draws.
	// Defaults to "digital" when absent. Changed from the settings page.
	ClockStyle string            `json:"clock_style,omitempty"`
	Sources    map[string]Source `json:"sources"`
}

// SourceNames returns the configured source names in a stable order, so logs
// and /health do not reorder themselves between runs.
func (c *Config) SourceNames() []string {
	names := make([]string, 0, len(c.Sources))
	for name := range c.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Load reads and validates the config file at path. A non-nil error means the
// agent must not start.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found at %s: create it from config.example.json", path)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	cfg := &Config{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	// An ignored typo in a key is a silent misconfiguration, so reject it.
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := cfg.applyDefaults(); err != nil {
		return nil, fmt.Errorf("in %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("in %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes cfg back to path, atomically: a temp file in the same
// directory, renamed into place, so a process that dies mid-write leaves
// either the old contents or the new ones, never a corrupt mix. This is what
// lets the settings page change config.json without a person hand-editing
// it, the same file Load reads on every startup and every "Reload config".
func Save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config_*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("saving %s: %w", path, err)
	}
	cleanup = false
	return nil
}

func (c *Config) applyDefaults() error {
	if strings.TrimSpace(c.Listen) == "" {
		c.Listen = DefaultListen
	}
	if strings.TrimSpace(c.LogLevel) == "" {
		c.LogLevel = DefaultLogLevel
	}
	if c.Sources == nil {
		c.Sources = map[string]Source{}
	}
	if strings.TrimSpace(c.ClockStyle) == "" {
		c.ClockStyle = "digital"
	}
	return nil
}

// Validate checks a Config that was built in memory (e.g. by the settings
// route, after Load and a field change), the same checks Load runs after
// parsing.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Token) == "" {
		return errors.New(`"token" is required and must not be empty: the agent will not serve data without a shared secret`)
	}
	if err := validateListen(c.Listen); err != nil {
		return err
	}
	if !contains(validLogLevels, c.LogLevel) {
		return fmt.Errorf(`"log_level" is %q, want one of %s`, c.LogLevel, strings.Join(validLogLevels, ", "))
	}
	if c.AccentColor != "" && !accentColorPattern.MatchString(c.AccentColor) {
		return fmt.Errorf(`"accent_color" is %q, want a 6-digit hex colour such as "#4e9eea"`, c.AccentColor)
	}
	hidden := make(map[string]bool, len(c.HiddenFaces))
	for _, face := range c.HiddenFaces {
		if !contains(KnownFaces, face) {
			return fmt.Errorf(`"hidden_faces" names %q, want one of %s`, face, strings.Join(KnownFaces, ", "))
		}
		if face == "clock" {
			return errors.New(`"hidden_faces" cannot include "clock": it is the panel's non-negotiable fallback face`)
		}
		hidden[face] = true
	}
	if len(hidden) >= len(KnownFaces) {
		return errors.New(`"hidden_faces" cannot hide every face: the panel would have nothing to show`)
	}
	if c.ClockStyle != "" && !contains(ClockStyles, c.ClockStyle) {
		return fmt.Errorf(`"clock_style" is %q, want one of %s`, c.ClockStyle, strings.Join(ClockStyles, ", "))
	}
	for _, name := range c.SourceNames() {
		src := c.Sources[name]
		if src.Enabled && src.IntervalMS <= 0 {
			return fmt.Errorf(`source %q is enabled but its "interval_ms" is %d, want a positive number of milliseconds`, name, src.IntervalMS)
		}
	}
	return nil
}

func validateListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf(`"listen" is %q, want host:port such as %s`, listen, DefaultListen)
	}
	if host != "" && net.ParseIP(host) == nil {
		// A hostname is acceptable, an obviously broken host is not.
		if strings.ContainsAny(host, " \t") {
			return fmt.Errorf(`"listen" has an invalid host %q`, host)
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf(`"listen" port %q is not a number`, port)
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf(`"listen" port %d is out of range, want 1 to 65535`, n)
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
