// Package app owns the agent's lifecycle: start, reload and stop.
//
// It is separate from the tray so tests can drive it without a desktop session.
package app

import (
	"context"
	"encoding/json"
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

// session is one configuration's worth of running agent. A reload replaces it
// wholesale because listen, token and the source set can all change.
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

// Reload re-reads the configuration and applies it. An invalid config leaves
// the running agent as it was.
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
	// Building the sources is part of validation. An unknown source name has to
	// fail before the running agent is torn down.
	if _, err := sources.Build(cfg); err != nil {
		a.log.Error("reload refused, keeping the running configuration", "error", err)
		return err
	}

	previous := a.current
	a.stopSession(previous)

	started, err := a.startSession(cfg)
	if err != nil {
		// Likely a port taken since validation. Put the previous config back.
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

// Addr is the address being listened on, which differs from the configured one
// when the port is zero.
func (a *App) Addr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current == nil {
		return ""
	}
	return a.current.listener.Addr().String()
}

// OpenURL is the address to point a browser at. A wildcard host becomes
// loopback and the token is attached, or the page would answer 401.
func (a *App) OpenURL() string {
	return a.tokenURL("/")
}

// SettingsURL is the address the tray "Options" item opens.
func (a *App) SettingsURL() string {
	return a.tokenURL("/settings.html")
}

// tokenURL builds a browser-openable URL for one page, token attached.
func (a *App) tokenURL(path string) string {
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

	return fmt.Sprintf("http://%s%s?token=%s", net.JoinHostPort(host, port), path, url.QueryEscape(a.current.cfg.Token))
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

	// Bind before anything else starts, so a busy port fails before any source
	// goroutine runs.
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Listen, err)
	}

	srv := server.New(server.Options{
		Token:         cfg.Token,
		Version:       a.version,
		Started:       time.Now(),
		Store:         store,
		Logger:        a.log,
		AccentColor:   cfg.AccentColor,
		HiddenFaces:   cfg.HiddenFaces,
		ClockStyle:    cfg.ClockStyle,
		HideNextEvent: cfg.HideNextEvent,
	})
	srv.HandleWebSocket()
	// Agent-level route, not from a source RouteProvider.
	srv.Handle("/settings", http.HandlerFunc(a.handleSettings))

	// A source may ask the agent to serve files for it, such as album art.
	// These go in before the static handler, which takes the root.
	for _, src := range built {
		if provider, ok := src.(sources.AssetProvider); ok {
			for urlPath, filePath := range provider.Assets() {
				path := filePath
				srv.HandleFile(urlPath, func() string { return path })
				a.log.Debug("serving source asset", "source", src.Name(), "path", urlPath, "file", path)
			}
		}

		// Authenticated routes, such as the page that starts a Spotify sign-in.
		if provider, ok := src.(sources.RouteProvider); ok {
			for urlPath, handler := range provider.Routes() {
				srv.Handle(urlPath, handler)
				a.log.Debug("serving source route", "source", src.Name(), "path", urlPath)
			}
		}

		// Routes reachable without the bearer token, such as an OAuth callback.
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

	// Wired after the runner exists. A source uses this to re-poll at once,
	// such as after a playback control.
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

// settingsBody is both the GET response and the POST request for /settings.
type settingsBody struct {
	AccentColor   string   `json:"accent_color"`
	HiddenFaces   []string `json:"hidden_faces"`
	ClockStyle    string   `json:"clock_style"`
	HideNextEvent bool     `json:"hide_next_event"`
}

// handleSettings backs the settings page. GET reports the configured values
// and POST saves them, then schedules a reload.
func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		var current settingsBody
		if a.current != nil {
			current = settingsBody{
				AccentColor:   a.current.cfg.AccentColor,
				HiddenFaces:   a.current.cfg.HiddenFaces,
				ClockStyle:    a.current.cfg.ClockStyle,
				HideNextEvent: a.current.cfg.HideNextEvent,
			}
		}
		a.mu.Unlock()
		writeJSON(w, current)

	case http.MethodPost:
		var body settingsBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := a.saveSettings(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, body)

		// Reload closes the listener this request came in on, so it cannot run
		// inline. See docs/architecture.md, "Lifecycle".
		time.AfterFunc(200*time.Millisecond, func() {
			if err := a.Reload(); err != nil {
				a.log.Error("reload after saving settings failed", "error", err)
			}
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// saveSettings validates and writes the new values to config.json. It starts
// from a fresh read of the file so a manual edit made since startup survives.
func (a *App) saveSettings(body settingsBody) error {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	cfg.AccentColor = body.AccentColor
	cfg.HiddenFaces = body.HiddenFaces
	cfg.ClockStyle = body.ClockStyle
	cfg.HideNextEvent = body.HideNextEvent
	if err := cfg.Validate(); err != nil {
		return err
	}
	return config.Save(a.configPath, cfg)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// stopSession shuts down the server, then the sources, then waits for them.
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
