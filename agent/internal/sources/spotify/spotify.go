// Package spotify reports what is playing.
//
// The real provider, which talks to the Spotify API, is not built yet. What is
// here is the shape of the reading, the face that renders it, and a mock
// provider that plays a configured track so the panel can be developed and
// judged before any of the account plumbing exists.
//
// The mode has to be stated in config. Running the mock by accident and
// believing it is real would be worse than refusing to start.
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

// artPath is where the agent serves the cover from.
//
// The device is on a LAN with an agent that will hold the credentials, so it
// loads one small image from the machine next to it rather than reaching a CDN
// on every track change. That keeps the device dumb and the token in one place.
const artPath = "/art/spotify"

// connectPath starts a fresh authorization attempt. Requires the bearer
// token, like everything else that is not the callback itself.
const connectPath = "/spotify/connect"

// callbackPath is where Spotify redirects back to after consent. This one
// cannot require the token: the browser tab Spotify opens is freshly
// navigated and carries none. See auth.go for what protects it instead.
const callbackPath = "/spotify/callback"

// controlPath runs one playback command: pause, resume, next, or previous.
// Requires the bearer token.
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
	// Layout is "fill" or "disc". Sent on every reading rather than left to a
	// URL query parameter, because the shell loads one fixed URL on the real
	// device with no way to attach one. Which layout reads better depends on
	// the room and the sleeve, and that can only be judged with the actual
	// panel in front of you, so it is a config setting you can flip without a
	// rebuild rather than a choice baked in here.
	Layout string `json:"layout"`
}

const (
	layoutFill = "fill"
	layoutDisc = "disc"
)

// normalizeLayout validates the configured layout, defaulting to "fill" when
// unset. An invalid value is rejected at construction, the same as everywhere
// else in this codebase a bad config value is: better to refuse to start than
// silently fall back to a default the user did not ask for.
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

// New builds the spotify source: a mock that plays a configured track, or the
// real provider talking to the Spotify API.
//
// Building sources.Source directly rather than *Source to accommodate the two
// concrete implementations.
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

	// Relative file settings mean "next to config.json". The agent may be
	// started from anywhere, such as at login, and the working directory is not
	// where anyone put these files.
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

// resolvePath makes a relative path relative to dir. An empty path, an absolute
// one, a rooted one with no drive, or an empty dir (nothing to resolve against)
// is returned as written.
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

// Assets declares files this source wants the agent to serve.
//
// Opt-in rather than something the registry knows about: a source that has
// nothing to serve implements nothing. When the real provider arrives this
// becomes a cache directory rather than one configured file.
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

	// The position is derived from the clock rather than from a stored start
	// time, so it advances, wraps at the end of the track, and survives a
	// reload without jumping back to zero. A still position would make the face
	// look frozen, which is exactly what a broken feed looks like.
	if s.settings.Paused {
		reading.PositionMS = 0
	} else {
		reading.PositionMS = s.now().UnixMilli() % s.settings.DurationMS
	}

	return reading, nil
}
