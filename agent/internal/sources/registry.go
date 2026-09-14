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
