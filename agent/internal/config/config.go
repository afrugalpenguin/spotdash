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

// Config is the validated contents of config.json.
type Config struct {
	Listen   string            `json:"listen"`
	Token    string            `json:"token"`
	LogLevel string            `json:"log_level"`
	Sources  map[string]Source `json:"sources"`
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
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("in %s: %w", path, err)
	}
	return cfg, nil
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
	return nil
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.Token) == "" {
		return errors.New(`"token" is required and must not be empty: the agent will not serve data without a shared secret`)
	}
	if err := validateListen(c.Listen); err != nil {
		return err
	}
	if !contains(validLogLevels, c.LogLevel) {
		return fmt.Errorf(`"log_level" is %q, want one of %s`, c.LogLevel, strings.Join(validLogLevels, ", "))
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
