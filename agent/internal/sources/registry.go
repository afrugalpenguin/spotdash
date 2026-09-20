// Package sources defines what a data source is and runs the configured ones.
// The runner owns scheduling, panic recovery, backoff and state writes. A new
// source is one package plus one line in the factory table.
package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources/calendar"
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

// AssetProvider is an optional extra for a source that wants the agent to
// serve files on its behalf, such as album art.
type AssetProvider interface {
	Assets() map[string]string
}

// RouteProvider is an optional extra a source may implement to register its
// own authenticated HTTP routes on the agent, such as the page that starts a
// Spotify authorization attempt.
type RouteProvider interface {
	Routes() map[string]http.Handler
}

// OpenRouteProvider is like RouteProvider, but the routes are reachable
// without the bearer token. An OAuth callback needs this: the tab the provider
// redirects to holds no token. The source must protect the route itself.
type OpenRouteProvider interface {
	OpenRoutes() map[string]http.Handler
}

// RepollRegistrar is an optional extra for a source that needs an immediate
// re-poll after it changes something itself, such as a playback control. The
// registry hands it a function because it has no reference to the runner.
type RepollRegistrar interface {
	SetRepoll(fn func())
}

// Factory builds a source from its config block.
type Factory func(cfg config.Source) (Source, error)

var errTestConstruction = errors.New("construction failed")

// factories is the one place a new source has to be mentioned.
var factories = map[string]Factory{
	calendar.Name:  adapt(calendar.New),
	clock.Name:     adapt(clock.New),
	spotify.Name:   adapt(spotify.New),
	telemetry.Name: adapt(telemetry.New),
}

// adapt turns a constructor returning a concrete source type into a Factory.
// Source packages cannot import this one (cycle), so they return their own type.
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
	// SourceNames is sorted, so the order is stable.
	var built []Source
	for _, name := range cfg.SourceNames() {
		factory, known := factories[name]
		if !known {
			// Disabled sources are checked too. A typo is the likeliest way to
			// switch a working source off silently.
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
