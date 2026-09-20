// Package server exposes the agent over HTTP. Everything except /health needs a
// bearer token, and /health carries status only, never data or the token.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

// Options configures a Server.
type Options struct {
	Token   string
	Version string
	Started time.Time
	Store   *state.Store
	Logger  *slog.Logger
	// AccentColor, HiddenFaces, ClockStyle and HideNextEvent are the configured
	// display settings, carried on /health. See docs/architecture.md, "State
	// and transport". An empty AccentColor leaves the stylesheet default.
	AccentColor   string
	HiddenFaces   []string
	ClockStyle    string
	HideNextEvent bool
	// Now is the clock /health reports. Defaults to time.Now.
	Now func() time.Time
}

// Server routes HTTP requests for the agent.
type Server struct {
	opts      Options
	protected *http.ServeMux
	// open holds routes reachable without the bearer token, such as an OAuth
	// callback. It is a separate mux so an open route cannot open another
	// pattern.
	open *http.ServeMux
	// socket sits outside the token middleware because its token arrives as a
	// subprotocol, which the generic check cannot see.
	socket http.Handler
	log    *slog.Logger
}

// New builds a Server. Protected routes are added with Handle.
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Server{
		opts:      opts,
		protected: http.NewServeMux(),
		open:      http.NewServeMux(),
		log:       log,
	}
}

// Handle registers a route behind the bearer token check.
func (s *Server) Handle(pattern string, h http.Handler) {
	s.protected.Handle(pattern, h)
}

// HandleOpen registers a route reachable without the bearer token, for callers
// that cannot carry one, such as an OAuth redirect. The handler has to protect
// itself.
func (s *Server) HandleOpen(pattern string, h http.Handler) {
	s.open.Handle(pattern, h)
}

// Handler returns the complete HTTP handler for the agent.
func (s *Server) Handler() http.Handler {
	protected := s.requireToken(s.protected)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			s.handleHealth(w, r)
			return
		}
		if s.socket != nil && r.URL.Path == "/ws" {
			s.socket.ServeHTTP(w, r)
			return
		}

		// ServeMux.Handler reports which pattern matched, empty when none did, so
		// an open route can be tried without running the protected mux.
		if h, pattern := s.open.Handler(r); pattern != "" {
			h.ServeHTTP(w, r)
			return
		}

		// Paths that match nothing also get 401, so callers cannot map routes.
		protected.ServeHTTP(w, r)
	})
}

type healthSource struct {
	Status     string `json:"status"`
	LastUpdate string `json:"last_update,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

type healthResponse struct {
	Version       string  `json:"version"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	// Now is the agent's clock, RFC 3339 in UTC. The device has no
	// battery-backed RTC, so the panel corrects its own clock against this.
	Now           string                  `json:"now"`
	AccentColor   string                  `json:"accent_color,omitempty"`
	HiddenFaces   []string                `json:"hidden_faces,omitempty"`
	ClockStyle    string                  `json:"clock_style,omitempty"`
	HideNextEvent bool                    `json:"hide_next_event,omitempty"`
	Sources       map[string]healthSource `json:"sources"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Version:       s.opts.Version,
		UptimeSeconds: time.Since(s.opts.Started).Seconds(),
		Now:           s.opts.Now().UTC().Format(time.RFC3339),
		AccentColor:   s.opts.AccentColor,
		HiddenFaces:   s.opts.HiddenFaces,
		ClockStyle:    s.opts.ClockStyle,
		HideNextEvent: s.opts.HideNextEvent,
		Sources:       map[string]healthSource{},
	}
	for _, entry := range s.opts.Store.Snapshot() {
		hs := healthSource{
			Status:    string(entry.Status),
			LastError: entry.LastError,
		}
		if !entry.UpdatedAt.IsZero() {
			hs.LastUpdate = entry.UpdatedAt.UTC().Format(time.RFC3339)
		}
		resp.Sources[entry.Source] = hs
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Error("writing health response", "error", err)
	}
}

// sessionCookieName carries the token for a browser that has already presented
// it once.
const sessionCookieName = "spotdash_session"

// requireToken has no loopback exemption. The tray's Open UI URL works because
// App.tokenURL attaches the token.
func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A programmatic client sends the header.
		if s.tokenValid(r.Header.Get("Authorization")) {
			next.ServeHTTP(w, r)
			return
		}

		// A browser cannot set a header on navigation, so the panel URL carries
		// the token once and is answered with a session cookie.
		if s.secretEquals(r.URL.Query().Get("token")) {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    s.opts.Token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				// No Expires or MaxAge, so it is never written to disk.
			})
			next.ServeHTTP(w, r)
			return
		}

		if cookie, err := r.Cookie(sessionCookieName); err == nil && s.secretEquals(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="spotdash"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// tokenValid reports whether an Authorization header carries the shared token.
// The scheme is case-insensitive per RFC 7235.
func (s *Server) tokenValid(header string) bool {
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return false
	}
	return s.secretEquals(strings.TrimSpace(value))
}

// secretEquals compares a candidate against the configured token in constant
// time. An empty candidate never matches, even against an empty token.
func (s *Server) secretEquals(candidate string) bool {
	if candidate == "" || s.opts.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.opts.Token)) == 1
}
