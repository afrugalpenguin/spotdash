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
	log       *slog.Logger
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
		log:       log,
	}
}

// Handle registers a route behind the bearer token check.
func (s *Server) Handle(pattern string, h http.Handler) {
	s.protected.Handle(pattern, h)
}

// Handler returns the complete HTTP handler for the agent.
func (s *Server) Handler() http.Handler {
	root := http.NewServeMux()
	root.HandleFunc("/health", s.handleHealth)
	// Everything else sits behind auth, including paths that match nothing, so
	// an unauthenticated caller cannot map which routes exist.
	root.Handle("/", s.requireToken(s.protected))
	return root
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

func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.tokenValid(r.Header.Get("Authorization")) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="spotdash"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
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
	value = strings.TrimSpace(value)
	if value == "" || s.opts.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(s.opts.Token)) == 1
}
