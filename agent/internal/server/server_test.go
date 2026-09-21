package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

const testToken = "test-token-value"

func newTestServer(t *testing.T) (*Server, *state.Store) {
	t.Helper()
	store := state.New()
	srv := New(Options{
		Token:   testToken,
		Version: "test-version",
		Started: time.Now().Add(-90 * time.Second),
		Store:   store,
	})
	return srv, store
}

func do(t *testing.T, srv *Server, method, target, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealthNeedsNoToken(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/health", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
}

func TestHealthReportsVersionAndUptime(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		Version       string  `json:"version"`
		UptimeSeconds float64 `json:"uptime_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if body.Version != "test-version" {
		t.Errorf("version = %q, want %q", body.Version, "test-version")
	}
	if body.UptimeSeconds < 90 {
		t.Errorf("uptime_seconds = %v, want at least 90", body.UptimeSeconds)
	}
}

func TestProtocolHealthReportsTheVersion(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		Protocol *int `json:"protocol"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if body.Protocol == nil {
		t.Fatalf("/health has no protocol\nbody: %s", rec.Body.String())
	}
	if *body.Protocol != ProtocolVersion {
		t.Errorf("protocol = %d, want %d", *body.Protocol, ProtocolVersion)
	}
}

func TestHealthReportsEverySourceStatus(t *testing.T) {
	srv, store := newTestServer(t)
	store.Register("clock", state.StatusDegraded, "awaiting first poll")
	store.Register("telemetry", state.StatusDisabled, "")
	store.Update("clock", map[string]any{"time": "10:00"})

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		Sources map[string]struct {
			Status     string `json:"status"`
			LastUpdate string `json:"last_update"`
			LastError  string `json:"last_error"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if got := body.Sources["clock"].Status; got != "ok" {
		t.Errorf("clock status = %q, want ok", got)
	}
	if body.Sources["clock"].LastUpdate == "" {
		t.Error("clock last_update is empty, want a time")
	}
	if got := body.Sources["telemetry"].Status; got != "disabled" {
		t.Errorf("telemetry status = %q, want disabled", got)
	}
	if body.Sources["telemetry"].LastUpdate != "" {
		t.Error("last_update set for a source that never polled, want empty")
	}
}

func TestHealthReportsTheConfiguredAccentColor(t *testing.T) {
	store := state.New()
	srv := New(Options{
		Token:       testToken,
		Version:     "test-version",
		Started:     time.Now(),
		Store:       store,
		AccentColor: "#7c83fd",
	})

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		AccentColor string `json:"accent_color"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if body.AccentColor != "#7c83fd" {
		t.Errorf("accent_color = %q, want %q", body.AccentColor, "#7c83fd")
	}
}

func TestHealthReportsTheConfiguredHiddenFaces(t *testing.T) {
	store := state.New()
	srv := New(Options{
		Token:       testToken,
		Version:     "test-version",
		Started:     time.Now(),
		Store:       store,
		HiddenFaces: []string{"clock", "telemetry"},
	})

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		HiddenFaces []string `json:"hidden_faces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if len(body.HiddenFaces) != 2 || body.HiddenFaces[0] != "clock" || body.HiddenFaces[1] != "telemetry" {
		t.Errorf("hidden_faces = %v, want [clock telemetry]", body.HiddenFaces)
	}
}

func TestHealthReportsTheConfiguredClockStyle(t *testing.T) {
	store := state.New()
	srv := New(Options{
		Token:      testToken,
		Version:    "test-version",
		Started:    time.Now(),
		Store:      store,
		ClockStyle: "analogue",
	})

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		ClockStyle string `json:"clock_style"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if body.ClockStyle != "analogue" {
		t.Errorf("clock_style = %q, want %q", body.ClockStyle, "analogue")
	}
}

func TestHealthReportsHideNextEvent(t *testing.T) {
	store := state.New()
	srv := New(Options{
		Token:         testToken,
		Version:       "test-version",
		Started:       time.Now(),
		Store:         store,
		HideNextEvent: true,
	})

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		HideNextEvent bool `json:"hide_next_event"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if !body.HideNextEvent {
		t.Error("hide_next_event = false, want true")
	}
}

func TestHealthOmitsAccentColorWhenUnconfigured(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/health", "")

	if strings.Contains(rec.Body.String(), "accent_color") {
		t.Errorf("accent_color present when unconfigured:\n%s", rec.Body.String())
	}
}

func TestHealthNeverLeaksTokenOrSourceData(t *testing.T) {
	srv, store := newTestServer(t)
	store.Register("clock", state.StatusOK, "")
	store.Update("clock", map[string]any{"secret_reading": "sensitive-payload-marker"})

	rec := do(t, srv, http.MethodGet, "/health", "")

	body := rec.Body.String()
	if strings.Contains(body, testToken) {
		t.Errorf("/health leaked the token:\n%s", body)
	}
	// /health is unauthenticated, so it must not carry readings.
	if strings.Contains(body, "sensitive-payload-marker") {
		t.Errorf("/health leaked source data:\n%s", body)
	}
}

func TestProtectedRoutesRejectBadAuth(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"empty bearer", "Bearer "},
		{"wrong token", "Bearer not-the-token"},
		{"token without scheme", testToken},
		{"wrong scheme", "Basic " + testToken},
		{"token as a prefix of the header", "Bearer " + testToken + "extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newTestServer(t)
			srv.Handle("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			rec := do(t, srv, http.MethodGet, "/probe", tt.header)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestProtectedRouteAcceptsValidToken(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.Handle("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := do(t, srv, http.MethodGet, "/probe", "Bearer "+testToken)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestAuthSchemeIsCaseInsensitive(t *testing.T) {
	// RFC 7235 makes the auth scheme case-insensitive, and clients differ.
	srv, _ := newTestServer(t)
	srv.Handle("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := do(t, srv, http.MethodGet, "/probe", "bearer "+testToken)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 200 for a lowercase scheme", rec.Code)
	}
}

func TestUnknownPathRequiresAuth(t *testing.T) {
	// A 404 would let an unauthenticated caller map which routes exist.
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/does-not-exist", "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// /health carries the agent clock because the panel cannot trust its own.
func TestHealthReportsTheAgentsCurrentTime(t *testing.T) {
	fixed := time.Date(2026, 9, 14, 10, 30, 0, 0, time.FixedZone("BST", 3600))
	srv := New(Options{
		Token:   testToken,
		Version: "test-version",
		Started: fixed.Add(-90 * time.Second),
		Store:   state.New(),
		Now:     func() time.Time { return fixed },
	})

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		Now string `json:"now"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v\nbody: %s", err, rec.Body.String())
	}
	if want := "2026-09-14T09:30:00Z"; body.Now != want {
		t.Errorf("now = %q, want %q", body.Now, want)
	}
}

func TestHealthTimeDefaultsToTheRealClock(t *testing.T) {
	srv, _ := newTestServer(t)
	before := time.Now().Add(-time.Second)

	rec := do(t, srv, http.MethodGet, "/health", "")

	var body struct {
		Now string `json:"now"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	got, err := time.Parse(time.RFC3339, body.Now)
	if err != nil {
		t.Fatalf("now = %q, want RFC 3339: %v", body.Now, err)
	}
	if got.Before(before) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("now = %v, want about the current time", got)
	}
}
