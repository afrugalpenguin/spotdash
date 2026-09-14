// Package server exposes the agent over HTTP.
//
// Everything except /health requires a bearer token. /health is deliberately
// open so that it stays usable when auth itself is what is broken, and it
// therefore reports status only, never source data and never the token.
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
}

// Server routes HTTP requests for the agent.
type Server struct {
	opts      Options
	protected *http.ServeMux
	// open holds routes a source has explicitly asked to be reachable without
	// the bearer token, such as an OAuth callback. Separate from protected
	// rather than a flag on the same mux, so registering one open route can
	// never accidentally open another pattern too.
	open *http.ServeMux
	// socket is mounted outside the token middleware because it authenticates
	// differently: its token arrives as a WebSocket subprotocol, which the
	// generic check cannot see.
	socket http.Handler
	log    *slog.Logger
}

// New builds a Server. Protected routes are added with Handle.
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
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

// HandleOpen registers a route reachable without the bearer token.
//
// This exists for the rare case where whatever calls the route cannot carry
// the token at all, such as a browser following an external OAuth redirect
// into a freshly opened tab. The handler is responsible for protecting itself
// by whatever means fits, since the bearer token cannot be that means here.
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

		// ServeMux.Handler looks up a route without running it and reports back
		// which pattern matched, empty when nothing did. That is what lets an
		// open route be tried without a bare "/" on this mux swallowing every
		// request meant for the protected one below.
		if h, pattern := s.open.Handler(r); pattern != "" {
			h.ServeHTTP(w, r)
			return
		}

		// Everything else sits behind auth, including paths that match nothing,
		// so an unauthenticated caller cannot map which routes exist.
		protected.ServeHTTP(w, r)
	})
}

type healthSource struct {
	Status     string `json:"status"`
	LastUpdate string `json:"last_update,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

type healthResponse struct {
	Version       string                  `json:"version"`
	UptimeSeconds float64                 `json:"uptime_seconds"`
	Sources       map[string]healthSource `json:"sources"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Version:       s.opts.Version,
		UptimeSeconds: time.Since(s.opts.Started).Seconds(),
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

func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A programmatic client sends the header. Nothing else is needed.
		if s.tokenValid(r.Header.Get("Authorization")) {
			next.ServeHTTP(w, r)
			return
		}

		// A browser cannot set a header when it is navigating, so the panel URL
		// carries the token once. Everything the page then requests, its own
		// stylesheet and modules included, arrives with neither, which is why
		// that first request is exchanged for a session cookie.
		if s.secretEquals(r.URL.Query().Get("token")) {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    s.opts.Token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				// No Expires and no MaxAge: the cookie dies with the browser
				// session rather than being written to disk.
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
// The scheme is case-insensitive per RFC 7235; the token is compared in
// constant time.
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
