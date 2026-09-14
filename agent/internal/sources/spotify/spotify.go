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
}

type settings struct {
	Mode       string `json:"mode"`
	Track      string `json:"track"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int64  `json:"duration_ms"`
	ArtFile    string `json:"art_file"`
	Paused     bool   `json:"paused"`
}

// Source reports the currently playing track.
type Source struct {
	interval time.Duration
	settings settings

	// now is overridden in tests.
	now func() time.Time
}

// New builds the spotify source.
func New(cfg config.Source) (*Source, error) {
	var s settings
	if len(cfg.Settings) > 0 {
		if err := json.Unmarshal(cfg.Settings, &s); err != nil {
			return nil, fmt.Errorf("reading settings: %w", err)
		}
	}

	switch s.Mode {
	case "":
		return nil, fmt.Errorf(`"mode" is required: use "mock" to play a configured track, or "api" once that is built`)
	case "mock":
		if s.Track == "" {
			return nil, fmt.Errorf(`"mode" is "mock" but no "track" is configured, so there is nothing to play`)
		}
		if s.DurationMS <= 0 {
			return nil, fmt.Errorf(`"duration_ms" is %d, want a positive length for the configured track`, s.DurationMS)
		}
	case "api":
		return nil, fmt.Errorf(`"mode" is "api", which is not implemented yet: the account plumbing is phase 2 work`)
	default:
		return nil, fmt.Errorf(`"mode" is %q, want "mock" or "api"`, s.Mode)
	}

	return &Source{
		interval: cfg.Interval(),
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
