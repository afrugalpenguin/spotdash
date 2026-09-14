package spotify

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// authorizeEndpoint is Spotify's consent page.
const authorizeEndpoint = "https://accounts.spotify.com/authorize"

// scope requests read-only access to what is currently playing. No write
// scope: play and pause from the panel would be the first write path in the
// system, and that boundary is not crossed here.
const scope = "user-read-currently-playing user-read-playback-state"

// pendingAttemptTTL bounds how long a beginAuth attempt stays valid. Someone
// who opens the consent page and walks away should not leave a permanently
// live callback waiting for a code that may eventually arrive from somewhere
// else.
const pendingAttemptTTL = 10 * time.Minute

// accessTokenSkew refreshes the access token a little before Spotify's own
// expiry, so an in-flight poll does not race a token that just expired.
const accessTokenSkew = 60 * time.Second

// authManagerOptions configures an authManager.
type authManagerOptions struct {
	ClientID    string
	RedirectURI string
	StatePath   string
}

// pendingAuth is one in-flight authorization attempt: the PKCE verifier that
// only this process knows, and the state value that binds the eventual
// callback to this specific attempt.
type pendingAuth struct {
	verifier  string
	state     string
	createdAt time.Time
}

// authManager owns the PKCE flow, the persisted refresh token, and the
// in-memory access token cache. It is the whole of what the spotify source
// needs to get a valid access token on demand.
type authManager struct {
	clientID    string
	redirectURI string
	statePath   string
	tokens      *tokenClient

	mu                sync.Mutex
	pending           *pendingAuth
	refreshToken      string
	accessTokenCache  string
	accessTokenExpiry time.Time
}

// newAuthManager builds a manager and loads any previously saved
// authorization. A corrupt state file fails closed rather than silently
// starting unauthorized, the same as an invalid config.json does: silently
// discarding a real, working credential is worse than refusing to start.
func newAuthManager(opts authManagerOptions) (*authManager, error) {
	state, err := loadState(opts.StatePath)
	if err != nil {
		return nil, err
	}
	return &authManager{
		clientID:     opts.ClientID,
		redirectURI:  opts.RedirectURI,
		statePath:    opts.StatePath,
		tokens:       newTokenClient(opts.ClientID),
		refreshToken: state.RefreshToken,
	}, nil
}

// isAuthorized reports whether a refresh token is held, which is what makes
// this a working connection rather than one still waiting to be set up.
func (m *authManager) isAuthorized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshToken != ""
}

// beginAuth starts a fresh PKCE attempt and returns the URL to send the user
// to. Starting a new attempt discards any previous unfinished one: only the
// most recent beginAuth can be completed by a callback.
func (m *authManager) beginAuth() (string, error) {
	verifier, err := newCodeVerifier()
	if err != nil {
		return "", fmt.Errorf("generating a code verifier: %w", err)
	}
	state, err := newState()
	if err != nil {
		return "", fmt.Errorf("generating a state value: %w", err)
	}

	m.mu.Lock()
	m.pending = &pendingAuth{verifier: verifier, state: state, createdAt: time.Now()}
	m.mu.Unlock()

	q := url.Values{
		"client_id":             {m.clientID},
		"response_type":         {"code"},
		"redirect_uri":          {m.redirectURI},
		"code_challenge_method": {"S256"},
		"code_challenge":        {challengeFor(verifier)},
		"state":                 {state},
		"scope":                 {scope},
	}
	return authorizeEndpoint + "?" + q.Encode(), nil
}

// handleCallback completes a pending authorization attempt.
//
// This endpoint has to be reachable without the panel's bearer token: the
// browser tab Spotify redirects to is freshly opened and carries no token.
// The state parameter is what stands in for that, binding this request to one
// specific beginAuth call, single use, and expiring after pendingAttemptTTL.
func (m *authManager) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if reason := query.Get("error"); reason != "" {
		m.clearPending()
		writeAuthResult(w, http.StatusBadRequest, "Not connected", fmt.Sprintf("Spotify reported: %s", reason))
		return
	}

	state := query.Get("state")
	code := query.Get("code")

	attempt := m.takePending(state)
	if attempt == nil {
		writeAuthResult(w, http.StatusForbidden, "This link has expired",
			"Open the connect page again to start over.")
		return
	}
	if code == "" {
		writeAuthResult(w, http.StatusBadRequest, "Spotify did not send a code", "")
		return
	}

	tok, err := m.tokens.exchange(r.Context(), code, attempt.verifier, m.redirectURI)
	if err != nil {
		writeAuthResult(w, http.StatusBadGateway, "Could not complete the connection", err.Error())
		return
	}

	if err := saveState(m.statePath, authState{RefreshToken: tok.RefreshToken}); err != nil {
		writeAuthResult(w, http.StatusInternalServerError, "Connected, but could not save it",
			"The connection will not survive a restart: "+err.Error())
		return
	}

	m.mu.Lock()
	m.refreshToken = tok.RefreshToken
	m.accessTokenCache = tok.AccessToken
	m.accessTokenExpiry = tok.ExpiresAt
	m.mu.Unlock()

	writeAuthResult(w, http.StatusOK, "Connected", "You can close this tab.")
}

// takePending consumes the pending attempt if state matches and it has not
// expired, so a callback can never be replayed.
func (m *authManager) takePending(state string) *pendingAuth {
	m.mu.Lock()
	defer m.mu.Unlock()

	attempt := m.pending
	if attempt == nil || state == "" || attempt.state != state {
		return nil
	}
	if time.Since(attempt.createdAt) > pendingAttemptTTL {
		m.pending = nil
		return nil
	}
	m.pending = nil
	return attempt
}

func (m *authManager) clearPending() {
	m.mu.Lock()
	m.pending = nil
	m.mu.Unlock()
}

// accessToken returns a usable access token, refreshing it first if it is
// missing or close to expiry.
//
// A reauth-required error clears the held refresh token so the manager
// visibly stops claiming to be connected, rather than retrying a grant that
// is not coming back.
func (m *authManager) accessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	refreshToken := m.refreshToken
	cached := m.accessTokenCache
	expiry := m.accessTokenExpiry
	m.mu.Unlock()

	if refreshToken == "" {
		return "", fmt.Errorf("spotify: not connected yet")
	}
	if cached != "" && time.Now().Before(expiry.Add(-accessTokenSkew)) {
		return cached, nil
	}

	tok, err := m.tokens.refresh(ctx, refreshToken)
	if err != nil {
		if IsReauthRequired(err) {
			m.mu.Lock()
			m.refreshToken = ""
			m.accessTokenCache = ""
			m.mu.Unlock()
			// Best effort: clear the persisted token too, so a restart does not
			// keep trying the same dead grant. Failure to remove it is not fatal,
			// since it will fail the same way again and report the same error.
			_ = saveState(m.statePath, authState{})
		}
		return "", err
	}

	if err := saveState(m.statePath, authState{RefreshToken: tok.RefreshToken}); err != nil {
		return "", fmt.Errorf("saving the refreshed token: %w", err)
	}

	m.mu.Lock()
	m.refreshToken = tok.RefreshToken
	m.accessTokenCache = tok.AccessToken
	m.accessTokenExpiry = tok.ExpiresAt
	m.mu.Unlock()

	return tok.AccessToken, nil
}

// writeAuthResult renders a small, static confirmation page. This is shown in
// a real browser tab, on whatever machine the user happened to authorize from,
// so it carries no data beyond a short human-readable message.
func writeAuthResult(w http.ResponseWriter, status int, heading, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>spotdash</title>
<style>body{background:#000;color:#edf1ee;font-family:system-ui,sans-serif;
display:flex;align-items:center;justify-content:center;height:100vh;margin:0;text-align:center}
main{max-width:26rem;padding:0 1.5rem}h1{font-weight:500;font-size:1.3rem}
p{color:#76878a;font-size:0.95rem}</style></head>
<body><main><h1>%s</h1><p>%s</p></main></body></html>`,
		html.EscapeString(heading), html.EscapeString(detail))
}
