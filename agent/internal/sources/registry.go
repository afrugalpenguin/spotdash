// Package sources defines what a data source is and runs the configured ones.
//
// A source knows how to produce one value and how often. Everything else,
// scheduling, panic recovery, backoff, and writing to the state store, belongs
// to the runner in this package. Adding a source means one new package plus one
// line in the factory table below.
package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources/clock"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources/spotify"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources/telemetry"
)

// Source produces one value on demand.
type Source interface {
	Name() string
	Poll(ctx context.Context) (any, error)
	Interval() time.Duration
}

// AssetProvider is an optional extra a source may implement when it wants the
// agent to serve files on its behalf, such as album art.
//
// Opt-in rather than part of the Source contract: a source with nothing to
// serve implements nothing, and the registry stays unaware of what any
// particular source needs.
type AssetProvider interface {
	Assets() map[string]string
}

// RouteProvider is an optional extra a source may implement to register its
// own authenticated HTTP routes on the agent, such as the page that starts a
// Spotify authorization attempt.
type RouteProvider interface {
	Routes() map[string]http.Handler
}

// OpenRouteProvider is like RouteProvider, but for routes that must be
// reachable without the bearer token, because whatever calls them cannot
// carry it. An OAuth callback is the case this exists for: the browser tab an
// external provider redirects to is freshly opened and holds no token. A
// source using this is responsible for protecting the route itself.
type OpenRouteProvider interface {
	OpenRoutes() map[string]http.Handler
}

// RepollRegistrar is an optional extra a source may implement to ask for an
// immediate re-poll after it changes something itself, such as a playback
// control action. A source has no reference to the runner that schedules it,
// so the registry hands it a function rather than the source reaching for the
// runner directly.
type RepollRegistrar interface {
	SetRepoll(fn func())
}

// Factory builds a source from its config block.
type Factory func(cfg config.Source) (Source, error)

// errTestConstruction is used by the tests in this package.
var errTestConstruction = errors.New("construction failed")

// factories is the one place a new source has to be mentioned.
var factories = map[string]Factory{
	clock.Name:     adapt(clock.New),
	spotify.Name:   adapt(spotify.New),
	telemetry.Name: adapt(telemetry.New),
}

// adapt turns a constructor returning a concrete source type into a Factory.
// Source packages cannot import this one without a cycle, so they return their
// own type and are adapted here.
func adapt[T Source](construct func(config.Source) (T, error)) Factory {
	return func(cfg config.Source) (Source, error) {
		src, err := construct(cfg)
		if err != nil {
			return nil, err
		}
		return src, nil
	}
}

// Build constructs every enabled source in the config.
func Build(cfg *config.Config) ([]Source, error) {
	return build(cfg, factories)
}

func build(cfg *config.Config, factories map[string]Factory) ([]Source, error) {
	// SourceNames is sorted, so the result order is stable between runs.
	var built []Source
	for _, name := range cfg.SourceNames() {
		factory, known := factories[name]
		if !known {
			// Checked for disabled sources too: a typo is the likeliest way a
			// working source gets silently switched off.
			return nil, fmt.Errorf("config names an unknown source %q", name)
		}
		if !cfg.Sources[name].Enabled {
			continue
		}
		src, err := factory(cfg.Sources[name])
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", name, err)
		}
		built = append(built, src)
	}
	return built, nil
}
