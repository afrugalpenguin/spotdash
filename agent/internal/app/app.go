// Package app owns the agent's lifecycle: start, reload and stop.
//
// It is separate from the tray so that the lifecycle can be driven, and tested,
// without a desktop session. The tray is one caller of this; a signal handler
// is another.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/server"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources"
	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

const shutdownGrace = 5 * time.Second

// App is the running agent.
type App struct {
	configPath string
	version    string
	log        *slog.Logger

	mu      sync.Mutex
	current *session
}

// session is one configuration's worth of running agent: a listener, a server,
// a store and a set of source goroutines. A reload replaces one wholesale,
// because listen, token and the source set can all change.
type session struct {
	cfg      *config.Config
	store    *state.Store
	runner   *sources.Runner
	server   *http.Server
	listener net.Listener
	cancel   context.CancelFunc
	started  time.Time
}

// New returns an agent that reads its configuration from configPath.
func New(configPath, version string, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}
	return &App{configPath: configPath, version: version, log: log}
}

// Start loads the configuration and begins serving.
func (a *App) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.current != nil {
		return errors.New("already started")
	}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return err
	}

	started, err := a.startSession(cfg)
	if err != nil {
		return err
	}
	a.current = started
	return nil
}

// Reload re-reads the configuration and applies it.
//
// It is fail closed in the same way startup is, and then some: an invalid
// config leaves the running agent exactly as it was. Someone mistyping a key
// while the agent is running should be told, not have the panel go dark.
func (a *App) Reload() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.current == nil {
		return errors.New("not started, so there is nothing to reload")
	}

	// Validate before touching anything that is working.
	cfg, err := config.Load(a.configPath)
	if err != nil {
		a.log.Error("reload refused, keeping the running configuration", "error", err)
		return err
	}
	// Building the sources is part of validation: an unknown source name or an
	// unparseable clock window has to fail here rather than after the running
	// agent has been torn down.
	if _, err := sources.Build(cfg); err != nil {
		a.log.Error("reload refused, keeping the running configuration", "error", err)
		return err
	}

	previous := a.current
	a.stopSession(previous)

	started, err := a.startSession(cfg)
	if err != nil {
		// The new configuration validated but could not be served, most likely
		// because something else took the port in between. Put the previous one
		// back rather than leaving the agent down.
		a.log.Error("reload failed to start, restoring the previous configuration", "error", err)
		restored, restoreErr := a.startSession(previous.cfg)
		if restoreErr != nil {
			a.current = nil
			return fmt.Errorf("reload failed (%w) and the previous configuration could not be restored: %v", err, restoreErr)
		}
		a.current = restored
		return err
	}

	a.current = started
	a.log.Info("configuration reloaded", "listen", started.listener.Addr().String())
	return nil
}

// Stop shuts everything down. It is safe to call more than once.
func (a *App) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.current == nil {
		return nil
	}
	a.stopSession(a.current)
	a.current = nil
	return nil
}

// Addr is the address actually being listened on, which is not always the
// configured one: a configured port of zero, or any port, resolves here.
func (a *App) Addr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current == nil {
		return ""
	}
	return a.current.listener.Addr().String()
}

// OpenURL is the address to point a browser at.
//
// It rewrites a wildcard host to loopback, because a browser cannot open
// 0.0.0.0, and carries the token, because the page authenticates with it on
// first load and would otherwise show nothing but a 401.
func (a *App) OpenURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current == nil {
		return ""
	}

	host, port, err := net.SplitHostPort(a.current.listener.Addr().String())
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}

	return fmt.Sprintf("http://%s/?token=%s", net.JoinHostPort(host, port), url.QueryEscape(a.current.cfg.Token))
}

// baseURL is the address a test or a local client can reach.
func (a *App) baseURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current == nil {
		return ""
	}
	host, port, err := net.SplitHostPort(a.current.listener.Addr().String())
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (a *App) startSession(cfg *config.Config) (*session, error) {
	built, err := sources.Build(cfg)
	if err != nil {
		return nil, err
	}

	store := state.New()
	for _, name := range cfg.SourceNames() {
		if cfg.Sources[name].Enabled {
			store.Register(name, state.StatusDegraded, "awaiting first poll")
		} else {
			store.Register(name, state.StatusDisabled, "")
		}
	}

	// Bind before anything else starts, so a port already in use fails here
	// rather than after source goroutines are running.
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Listen, err)
	}

	srv := server.New(server.Options{
		Token:   cfg.Token,
		Version: a.version,
		Started: time.Now(),
		Store:   store,
		Logger:  a.log,
	})
	srv.HandleWebSocket()

	// A source may ask the agent to serve files for it, which is how album art
	// reaches the panel from the machine next to it rather than from a CDN.
	// Registered before the static handler, which takes the root.
	for _, src := range built {
		if provider, ok := src.(sources.AssetProvider); ok {
			for urlPath, filePath := range provider.Assets() {
				path := filePath
				srv.HandleFile(urlPath, func() string { return path })
				a.log.Debug("serving source asset", "source", src.Name(), "path", urlPath, "file", path)
			}
		}

		// A source may register its own authenticated routes, such as the page
		// that starts a Spotify authorization attempt.
		if provider, ok := src.(sources.RouteProvider); ok {
			for urlPath, handler := range provider.Routes() {
				srv.Handle(urlPath, handler)
				a.log.Debug("serving source route", "source", src.Name(), "path", urlPath)
			}
		}

		// And, separately, routes that must be reachable without the bearer
		// token, because whatever calls them cannot carry it. An OAuth
		// callback is the case this exists for.
		if provider, ok := src.(sources.OpenRouteProvider); ok {
			for urlPath, handler := range provider.OpenRoutes() {
				srv.HandleOpen(urlPath, handler)
				a.log.Debug("serving source open route", "source", src.Name(), "path", urlPath)
			}
		}
	}

	srv.HandleStatic()

	httpServer := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	runner := sources.NewRunner(store, a.log)
	runner.Start(ctx, built)

	// A source may want to trigger its own immediate re-poll, such as after a
	// playback control action, rather than wait out its normal interval. The
	// runner has to exist first, which is why this is wired here rather than
	// in the loop above that builds routes and assets.
	for _, src := range built {
		if registrar, ok := src.(sources.RepollRegistrar); ok {
			name := src.Name()
			registrar.SetRepoll(func() { runner.PollNow(name) })
		}
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("http server stopped", "error", err)
		}
	}()

	a.log.Info("serving",
		"listen", listener.Addr().String(),
		"sources", len(built),
		"version", a.version,
	)

	return &session{
		cfg:      cfg,
		store:    store,
		runner:   runner,
		server:   httpServer,
		listener: listener,
		cancel:   cancel,
		started:  time.Now(),
	}, nil
}

// stopSession shuts one session down in the order that avoids surprises: stop
// accepting and close open connections first, then stop the sources that were
// feeding them, then wait for their goroutines.
func (a *App) stopSession(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		a.log.Warn("http server did not shut down cleanly", "error", err)
		_ = s.server.Close()
	}

	s.cancel()
	s.runner.Wait()
}
