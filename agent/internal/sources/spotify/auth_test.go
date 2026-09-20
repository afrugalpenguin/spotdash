package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestAuthManager(t *testing.T, tokenHandler http.HandlerFunc) (*authManager, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(tokenHandler)
	t.Cleanup(srv.Close)

	statePath := filepath.Join(t.TempDir(), "spotify_state.json")
	mgr, err := newAuthManager(authManagerOptions{
		ClientID:    "test-client-id",
		RedirectURI: "http://127.0.0.1:8765/spotify/callback",
		StatePath:   statePath,
	})
	if err != nil {
		t.Fatalf("newAuthManager: %v", err)
	}
	mgr.tokens.endpoint = srv.URL
	mgr.tokens.http = srv.Client()
	return mgr, srv
}

func TestNewAuthManagerStartsUnauthorized(t *testing.T) {
	mgr, _ := newTestAuthManager(t, nil)

	if mgr.isAuthorized() {
		t.Error("isAuthorized = true with no saved state")
	}
}

func TestNewAuthManagerLoadsAPreviouslySavedToken(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "spotify_state.json")
	if err := saveState(statePath, authState{RefreshToken: "already-authorised"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	mgr, err := newAuthManager(authManagerOptions{
		ClientID:    "id",
		RedirectURI: "http://127.0.0.1:8765/spotify/callback",
		StatePath:   statePath,
	})
	if err != nil {
		t.Fatalf("newAuthManager: %v", err)
	}

	if !mgr.isAuthorized() {
		t.Error("isAuthorized = false with a saved token")
	}
}

func TestNewAuthManagerRejectsCorruptState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "spotify_state.json")
	writeCorruptFile(t, statePath)

	_, err := newAuthManager(authManagerOptions{ClientID: "id", RedirectURI: "r", StatePath: statePath})

	if err == nil {
		t.Fatal("newAuthManager accepted a corrupt state file")
	}
}

func writeCorruptFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestBeginAuthBuildsTheAuthorizeURL(t *testing.T) {
	mgr, _ := newTestAuthManager(t, nil)

	raw, err := mgr.beginAuth()
	if err != nil {
		t.Fatalf("beginAuth: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if u.Host != "accounts.spotify.com" || u.Path != "/authorize" {
		t.Errorf("URL = %q, want the authorize endpoint", raw)
	}

	q := u.Query()
	if q.Get("client_id") != "test-client-id" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:8765/spotify/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge is empty")
	}
	if q.Get("state") == "" {
		t.Error("state is empty")
	}
	// Read scope plus the transport write scope. No playlist, library or
	// account scopes.
	scope := q.Get("scope")
	for _, want := range []string{"user-read-currently-playing", "user-modify-playback-state"} {
		if !strings.Contains(scope, want) {
			t.Errorf("scope = %q, missing %q", scope, want)
		}
	}
	for _, forbidden := range []string{"playlist", "library", "user-read-email", "streaming"} {
		if strings.Contains(scope, forbidden) {
			t.Errorf("scope = %q, has forbidden %q", scope, forbidden)
		}
	}
}

func TestCallbackCompletesAndPersistsTheRefreshToken(t *testing.T) {
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresIn: 3600,
		})
	})

	raw, err := mgr.beginAuth()
	if err != nil {
		t.Fatalf("beginAuth: %v", err)
	}
	state := mustQueryFromAuthorizeURL(t, raw, "state")

	req := httptest.NewRequest(http.MethodGet, "/spotify/callback?code=auth-code&state="+state, nil)
	rec := httptest.NewRecorder()
	mgr.handleCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if !mgr.isAuthorized() {
		t.Error("isAuthorized = false after a callback")
	}

	saved, err := loadState(mgr.statePath)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if saved.RefreshToken != "refresh-1" {
		t.Errorf("persisted RefreshToken = %q, want refresh-1", saved.RefreshToken)
	}
}

func TestCallbackRejectsAWrongState(t *testing.T) {
	// CSRF protection for the unauthenticated callback.
	mgr, _ := newTestAuthManager(t, nil)

	if _, err := mgr.beginAuth(); err != nil {
		t.Fatalf("beginAuth: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/spotify/callback?code=auth-code&state=not-the-real-state", nil)
	rec := httptest.NewRecorder()
	mgr.handleCallback(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("mismatched state accepted")
	}
	if mgr.isAuthorized() {
		t.Error("isAuthorized = true after a forged callback")
	}
}

func TestCallbackWithNoPendingAttemptIsRejected(t *testing.T) {
	mgr, _ := newTestAuthManager(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/spotify/callback?code=auth-code&state=anything", nil)
	rec := httptest.NewRecorder()
	mgr.handleCallback(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("callback with no beginAuth succeeded")
	}
}

func TestCallbackSurfacesSpotifyDenial(t *testing.T) {
	// The user cancelled on Spotify's consent screen.
	mgr, _ := newTestAuthManager(t, nil)

	raw, err := mgr.beginAuth()
	if err != nil {
		t.Fatalf("beginAuth: %v", err)
	}
	state := mustQueryFromAuthorizeURL(t, raw, "state")

	req := httptest.NewRequest(http.MethodGet, "/spotify/callback?error=access_denied&state="+state, nil)
	rec := httptest.NewRecorder()
	mgr.handleCallback(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("denied consent reported success")
	}
	if mgr.isAuthorized() {
		t.Error("isAuthorized = true after denied consent")
	}
}

func TestCallbackIsSingleUse(t *testing.T) {
	// The pending attempt is consumed, so the code cannot be replayed.
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600})
	})

	raw, err := mgr.beginAuth()
	if err != nil {
		t.Fatalf("beginAuth: %v", err)
	}
	state := mustQueryFromAuthorizeURL(t, raw, "state")
	url := "/spotify/callback?code=auth-code&state=" + state

	first := httptest.NewRecorder()
	mgr.handleCallback(first, httptest.NewRequest(http.MethodGet, url, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first callback status = %d, want 200", first.Code)
	}

	second := httptest.NewRecorder()
	mgr.handleCallback(second, httptest.NewRequest(http.MethodGet, url, nil))
	if second.Code == http.StatusOK {
		t.Error("replayed callback succeeded")
	}
}

func TestAccessTokenRefreshesWhenNearExpiry(t *testing.T) {
	calls := 0
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "fresh-access", RefreshToken: "still-good", ExpiresIn: 3600,
		})
	})
	if err := saveState(mgr.statePath, authState{RefreshToken: "already-authorised"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	mgr.refreshToken = "already-authorised"
	// A stale access token.
	mgr.accessTokenExpiry = time.Now().Add(-1 * time.Minute)

	token, err := mgr.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if token != "fresh-access" {
		t.Errorf("token = %q, want fresh-access", token)
	}
	if calls != 1 {
		t.Errorf("token endpoint called %d times, want 1", calls)
	}
}

func TestAccessTokenReusesAnUnexpiredToken(t *testing.T) {
	calls := 0
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "should-not-be-used", ExpiresIn: 3600})
	})
	mgr.refreshToken = "already-authorised"
	mgr.accessTokenCache = "still-fresh"
	mgr.accessTokenExpiry = time.Now().Add(30 * time.Minute)

	token, err := mgr.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if token != "still-fresh" {
		t.Errorf("token = %q, want the cached token", token)
	}
	if calls != 0 {
		t.Errorf("token endpoint called %d times, want 0", calls)
	}
}

func TestAccessTokenClearsAuthorizationOnAReauthRequiredError(t *testing.T) {
	// A revoked grant makes the manager unauthorized again.
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "revoked"})
	})
	mgr.refreshToken = "revoked-token"
	mgr.accessTokenExpiry = time.Now().Add(-1 * time.Minute)

	_, err := mgr.accessToken(context.Background())

	if !IsReauthRequired(err) {
		t.Fatalf("err = %v, want a reauth-required error", err)
	}
	if mgr.isAuthorized() {
		t.Error("isAuthorized = true after a revoked grant")
	}
}

func mustQueryFromAuthorizeURL(t *testing.T, raw, key string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return u.Query().Get(key)
}

// completeCallback runs beginAuth, then a callback with the given code.
func completeCallback(t *testing.T, mgr *authManager) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := mgr.beginAuth()
	if err != nil {
		t.Fatalf("beginAuth: %v", err)
	}
	state := mustQueryFromAuthorizeURL(t, raw, "state")
	rec := httptest.NewRecorder()
	mgr.handleCallback(rec, httptest.NewRequest(http.MethodGet, "/spotify/callback?code=auth-code&state="+state, nil))
	return rec
}

func TestCallbackNotifiesOnceConnected(t *testing.T) {
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600})
	})
	notified := 0
	authorizedAtNotify := false
	mgr.setOnConnected(func() {
		notified++
		authorizedAtNotify = mgr.isAuthorized()
	})

	if rec := completeCallback(t, mgr); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if notified != 1 {
		t.Errorf("onConnected called %d times, want 1", notified)
	}
	if !authorizedAtNotify {
		t.Error("onConnected ran before the tokens were stored")
	}
}

func TestCallbackDoesNotNotifyWhenTheExchangeFails(t *testing.T) {
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	})
	notified := 0
	mgr.setOnConnected(func() { notified++ })

	if rec := completeCallback(t, mgr); rec.Code == http.StatusOK {
		t.Fatal("failed token exchange reported success")
	}

	if notified != 0 {
		t.Errorf("onConnected called %d times after a failed exchange, want 0", notified)
	}
}

func TestCallbackDoesNotNotifyOnAWrongStateOrADenial(t *testing.T) {
	mgr, _ := newTestAuthManager(t, nil)
	notified := 0
	mgr.setOnConnected(func() { notified++ })

	if _, err := mgr.beginAuth(); err != nil {
		t.Fatalf("beginAuth: %v", err)
	}
	mgr.handleCallback(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/spotify/callback?code=c&state=forged", nil))
	mgr.handleCallback(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/spotify/callback?error=access_denied", nil))

	if notified != 0 {
		t.Errorf("onConnected called %d times, want 0", notified)
	}
}

func TestCallbackWithNoHookStillSucceeds(t *testing.T) {
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600})
	})

	if rec := completeCallback(t, mgr); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
