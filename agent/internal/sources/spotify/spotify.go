// Package spotify reports what is playing. Mode "mock" plays a configured
// track. Mode "api" polls the Spotify Web API. The mode is required, so the
// mock never runs by accident. See docs/architecture.md, "Spotify".
package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

// Name is the key this source is configured under.
const Name = "spotify"

// artPath is where the agent serves the cover from. The device loads it from
// the agent and never reaches a CDN.
const artPath = "/art/spotify"

// connectPath starts a fresh authorization attempt. It requires the bearer
// token.
const connectPath = "/spotify/connect"

// callbackPath is where Spotify redirects after consent. It cannot require the
// token, because the tab carries none. See auth.go for what protects it.
const callbackPath = "/spotify/callback"

// controlPath runs one playback command: pause, resume, next or previous. It
// requires the bearer token.
const controlPath = "/spotify/control"

// Reading is what the spotify source publishes.
type Reading struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	// ArtURL is a path on this agent, or empty when there is no cover.
	ArtURL     string `json:"art_url"`
	PositionMS int64  `json:"position_ms"`
	DurationMS int64  `json:"duration_ms"`
	Playing    bool   `json:"playing"`
	// Layout is "fill" or "disc". It rides on every reading because the shell
	// loads one fixed URL and cannot carry a query parameter.
	Layout string `json:"layout"`
}

const (
	layoutFill = "fill"
	layoutDisc = "disc"
)

// normalizeLayout validates the configured layout, defaulting to "fill". An
// invalid value stops the agent at construction.
func normalizeLayout(value string) (string, error) {
	switch value {
	case "":
		return layoutFill, nil
	case layoutFill, layoutDisc:
		return value, nil
	default:
		return "", fmt.Errorf(`"layout" is %q, want "fill" or "disc"`, value)
	}
}

type settings struct {
	Mode   string `json:"mode"`
	Layout string `json:"layout"`

	// mode: "mock"
	Track      string `json:"track"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int64  `json:"duration_ms"`
	ArtFile    string `json:"art_file"`
	Paused     bool   `json:"paused"`

	// mode: "api"
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
	StateFile   string `json:"state_file"`
}

// Source reports the currently playing track.
type Source struct {
	interval time.Duration
	settings settings

	// now is overridden in tests.
	now func() time.Time
}

// New builds the spotify source. It returns an interface because mock and api
// modes are different concrete types.
func New(cfg config.Source) (interface {
	Name() string
	Poll(context.Context) (any, error)
	Interval() time.Duration
}, error) {
	var s settings
	if len(cfg.Settings) > 0 {
		if err := json.Unmarshal(cfg.Settings, &s); err != nil {
			return nil, fmt.Errorf("reading settings: %w", err)
		}
	}

	// Relative paths are relative to config.json. The agent may start from any
	// working directory.
	s.StateFile = resolvePath(cfg.Dir, s.StateFile)
	s.ArtFile = resolvePath(cfg.Dir, s.ArtFile)

	layout, err := normalizeLayout(s.Layout)
	if err != nil {
		return nil, err
	}
	s.Layout = layout

	switch s.Mode {
	case "":
		return nil, fmt.Errorf(`"mode" is required: use "mock" to play a configured track, or "api" to connect a real account`)
	case "mock":
		return newMockSource(cfg.Interval(), s)
	case "api":
		return newAPISourceFromSettings(cfg.Interval(), s)
	default:
		return nil, fmt.Errorf(`"mode" is %q, want "mock" or "api"`, s.Mode)
	}
}

// resolvePath joins a relative path onto dir. Empty, absolute and drive-less
// rooted paths, and an empty dir, are returned as written.
func resolvePath(dir, path string) string {
	if path == "" || dir == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return path
	}
	return filepath.Join(dir, path)
}

func newMockSource(interval time.Duration, s settings) (*Source, error) {
	if s.Track == "" {
		return nil, fmt.Errorf(`"mode" is "mock" but no "track" is configured, so there is nothing to play`)
	}
	if s.DurationMS <= 0 {
		return nil, fmt.Errorf(`"duration_ms" is %d, want a positive length for the configured track`, s.DurationMS)
	}
	return &Source{
		interval: interval,
		settings: s,
		now:      time.Now,
	}, nil
}

// Name identifies the source.
func (s *Source) Name() string { return Name }

// Interval is the configured poll period.
func (s *Source) Interval() time.Duration { return s.interval }

// Assets declares the mock's art file for the agent to serve.
func (s *Source) Assets() map[string]string {
	if s.settings.ArtFile == "" {
		return nil
	}
	return map[string]string{artPath: s.settings.ArtFile}
}

// Poll returns what is playing.
func (s *Source) Poll(_ context.Context) (any, error) {
	reading := Reading{
		Title:      s.settings.Track,
		Artist:     s.settings.Artist,
		Album:      s.settings.Album,
		DurationMS: s.settings.DurationMS,
		Playing:    !s.settings.Paused,
		Layout:     s.settings.Layout,
	}
	if s.settings.ArtFile != "" {
		reading.ArtURL = artPath
	}

	// Derived from the clock so it advances, wraps at the track end and survives
	// a reload. A still position looks like a broken feed.
	if s.settings.Paused {
		reading.PositionMS = 0
	} else {
		reading.PositionMS = s.now().UnixMilli() % s.settings.DurationMS
	}

	return reading, nil
}
