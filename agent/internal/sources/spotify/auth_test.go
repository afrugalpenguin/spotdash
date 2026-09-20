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
		t.Error("a fresh manager with no saved state should not be authorized")
	}
}

func TestNewAuthManagerLoadsAPreviouslySavedToken(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "spotify_state.json")
	if err := saveState(statePath, authState{RefreshToken: "already-authorised"}); err != nil {
		t.Fatalf("seeding saved state: %v", err)
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
		t.Error("a manager reading a saved refresh token should be authorized")
	}
}

func TestNewAuthManagerRejectsCorruptState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "spotify_state.json")
	writeCorruptFile(t, statePath)

	_, err := newAuthManager(authManagerOptions{ClientID: "id", RedirectURI: "r", StatePath: statePath})

	if err == nil {
		t.Fatal("newAuthManager should fail closed on a corrupt state file rather than silently starting unauthorised")
	}
}

func writeCorruptFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
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
		t.Fatalf("beginAuth produced an unparseable URL: %v", err)
	}
	if u.Host != "accounts.spotify.com" || u.Path != "/authorize" {
		t.Errorf("URL = %q, want the Spotify authorize endpoint", raw)
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
	// Read scope, plus the one write scope the transport controls need.
	// Nothing broader: no playlist, library or account scopes.
	scope := q.Get("scope")
	for _, want := range []string{"user-read-currently-playing", "user-modify-playback-state"} {
		if !strings.Contains(scope, want) {
			t.Errorf("scope = %q, missing %q", scope, want)
		}
	}
	for _, forbidden := range []string{"playlist", "library", "user-read-email", "streaming"} {
		if strings.Contains(scope, forbidden) {
			t.Errorf("scope = %q contains an unrequested scope %q", scope, forbidden)
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
		t.Error("the manager should be authorized after a successful callback")
	}

	saved, err := loadState(mgr.statePath)
	if err != nil {
		t.Fatalf("loadState after callback: %v", err)
	}
	if saved.RefreshToken != "refresh-1" {
		t.Errorf("persisted RefreshToken = %q, want refresh-1", saved.RefreshToken)
	}
}

func TestCallbackRejectsAWrongState(t *testing.T) {
	// This is the CSRF protection for an endpoint that has to be unauthenticated
	// because a freshly opened browser tab carries no bearer token.
	mgr, _ := newTestAuthManager(t, nil)

	if _, err := mgr.beginAuth(); err != nil {
		t.Fatalf("beginAuth: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/spotify/callback?code=auth-code&state=not-the-real-state", nil)
	rec := httptest.NewRecorder()
	mgr.handleCallback(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("a mismatched state should not be accepted")
	}
	if mgr.isAuthorized() {
		t.Error("the manager should not become authorized from a forged callback")
	}
}

func TestCallbackWithNoPendingAttemptIsRejected(t *testing.T) {
	mgr, _ := newTestAuthManager(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/spotify/callback?code=auth-code&state=anything", nil)
	rec := httptest.NewRecorder()
	mgr.handleCallback(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("a callback with no beginAuth in progress should not succeed")
	}
}

func TestCallbackSurfacesSpotifyDenial(t *testing.T) {
	// The user clicked "Cancel" on Spotify's consent screen.
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
		t.Error("a denied consent should not report success")
	}
	if mgr.isAuthorized() {
		t.Error("a denied consent should not authorize the manager")
	}
}

func TestCallbackIsSingleUse(t *testing.T) {
	// The pending attempt must be consumed so the same authorization code
	// cannot be replayed against the callback.
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
		t.Fatalf("first callback should succeed, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	mgr.handleCallback(second, httptest.NewRequest(http.MethodGet, url, nil))
	if second.Code == http.StatusOK {
		t.Error("replaying the same callback should not succeed a second time")
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
		t.Fatalf("seeding state: %v", err)
	}
	mgr.refreshToken = "already-authorised"
	// Simulate an access token that is already stale.
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
		t.Errorf("token = %q, want the cached one reused", token)
	}
	if calls != 0 {
		t.Errorf("token endpoint called %d times, want 0", calls)
	}
}

func TestAccessTokenClearsAuthorizationOnAReauthRequiredError(t *testing.T) {
	// A revoked grant should make the manager visibly unauthorized again,
	// rather than continuing to claim it holds a working connection.
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
		t.Error("the manager should no longer report itself authorized")
	}
}

func mustQueryFromAuthorizeURL(t *testing.T, raw, key string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing authorize URL: %v", err)
	}
	return u.Query().Get(key)
}

// completeCallback runs beginAuth and a callback carrying the given code, and
// returns the recorder.
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
		t.Error("onConnected ran before the tokens were stored, so a poll started from it would still fail")
	}
}

func TestCallbackDoesNotNotifyWhenTheExchangeFails(t *testing.T) {
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	})
	notified := 0
	mgr.setOnConnected(func() { notified++ })

	if rec := completeCallback(t, mgr); rec.Code == http.StatusOK {
		t.Fatal("a failed token exchange should not report success")
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
