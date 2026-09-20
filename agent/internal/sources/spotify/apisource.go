package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"time"
)

// accessTokenSource is satisfied by *authManager. It keeps apiSource testable
// without HTTP.
type accessTokenSource interface {
	accessToken(ctx context.Context) (string, error)
}

// currentlyPlayingFetcher is satisfied by *apiClient.
type currentlyPlayingFetcher interface {
	fetchCurrentlyPlaying(ctx context.Context, accessToken string) (*nowPlaying, error)
}

// playbackController is satisfied by *apiClient. These four methods are the
// whole control surface. See docs/architecture.md, "Spotify".
type playbackController interface {
	pause(ctx context.Context, accessToken string) error
	resume(ctx context.Context, accessToken string) error
	next(ctx context.Context, accessToken string) error
	previous(ctx context.Context, accessToken string) error
}

// artDownloader fetches raw image bytes from a URL.
type artDownloader interface {
	download(ctx context.Context, url string) ([]byte, error)
}

type httpArtDownloader struct {
	http *http.Client
}

func (d *httpArtDownloader) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching album art: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetching album art: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// apiSource is the api-mode provider: OAuth through authManager, polling
// through apiClient, art cached locally.
type apiSource struct {
	interval time.Duration
	layout   string

	tokens   accessTokenSource
	playback currentlyPlayingFetcher
	control  playbackController
	art      artDownloader

	artCachePath string
	lastTrackID  string
	hasArt       bool

	// previousTrackID is the track ID at the end of the last poll. Unlike
	// lastTrackID it updates even when the art download fails. awaitTrackChange
	// compares against it.
	previousTrackID string

	// expectingChange is set by handleControl after a next or previous and
	// consumed by the next poll, which retries until the track changes. Atomic
	// because the handler and the poller run on different goroutines.
	expectingChange atomic.Bool

	// sleep waits out one retry delay, or returns early if ctx is done. Tests
	// override it.
	sleep func(ctx context.Context, d time.Duration)

	auth *authManager

	// repoll asks the runner for an immediate poll. Set by SetRepoll, nil until
	// then.
	repoll func()
}

// consistencyRetries and consistencyDelay bound how long poll chases Spotify's
// lag after a skip, measured at 200ms to over 1s. Three tries 400ms apart cap
// the wait near 1.2s, under the 2s minPollTimeout.
const (
	consistencyRetries = 3
	consistencyDelay   = 400 * time.Millisecond
)

// ctxSleep waits d, or until ctx ends.
func ctxSleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// SetRepoll implements sources.RepollRegistrar. It also fires when a connection
// completes, because until then polls fail and the runner backs off to 30s.
func (s *apiSource) SetRepoll(fn func()) {
	s.repoll = fn
	if s.auth != nil {
		s.auth.setOnConnected(fn)
	}
}

// newAPISourceFromSettings validates api-mode settings and builds the source. A
// missing setting is a startup error.
func newAPISourceFromSettings(interval time.Duration, s settings) (*apiSource, error) {
	if s.ClientID == "" {
		return nil, fmt.Errorf(`"mode" is "api" but no "client_id" is configured: create an app at https://developer.spotify.com/dashboard and paste its client ID here`)
	}
	if s.RedirectURI == "" {
		return nil, fmt.Errorf(`"mode" is "api" but no "redirect_uri" is configured: it must exactly match what is registered in the Spotify app`)
	}
	if s.StateFile == "" {
		return nil, fmt.Errorf(`"mode" is "api" but no "state_file" is configured: this is where the connection is remembered between restarts`)
	}

	auth, err := newAuthManager(authManagerOptions{
		ClientID:    s.ClientID,
		RedirectURI: s.RedirectURI,
		StatePath:   s.StateFile,
	})
	if err != nil {
		return nil, fmt.Errorf("loading the saved spotify connection: %w", err)
	}

	api := newAPIClient()
	return &apiSource{
		interval:     interval,
		layout:       s.Layout,
		tokens:       auth,
		playback:     api,
		control:      api,
		art:          &httpArtDownloader{http: &http.Client{Timeout: 10 * time.Second}},
		artCachePath: filepath.Join(filepath.Dir(s.StateFile), "spotify_art.jpg"),
		auth:         auth,
		sleep:        ctxSleep,
	}, nil
}

// Name identifies the source.
func (s *apiSource) Name() string { return Name }

// Interval is the configured poll period.
func (s *apiSource) Interval() time.Duration { return s.interval }

// Assets declares the cached art file.
func (s *apiSource) Assets() map[string]string {
	return map[string]string{artPath: s.artCachePath}
}

// Routes registers the connect page and the playback control endpoint. Both
// require the bearer token.
func (s *apiSource) Routes() map[string]http.Handler {
	return map[string]http.Handler{
		connectPath: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorizeURL, err := s.auth.beginAuth()
			if err != nil {
				http.Error(w, "could not start spotify authorization: "+err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, authorizeURL, http.StatusFound)
		}),
		controlPath: http.HandlerFunc(s.handleControl),
	}
}

// controlRequest is the body of POST /spotify/control.
type controlRequest struct {
	Action string `json:"action"`
}

// handleControl runs one of four playback commands: pause, resume, next or
// previous.
func (s *apiSource) handleControl(w http.ResponseWriter, r *http.Request) {
	var body controlRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	token, err := s.tokens.accessToken(r.Context())
	if err != nil {
		http.Error(w, s.connectionError(err).Error(), http.StatusServiceUnavailable)
		return
	}

	var action func(context.Context, string) error
	switch body.Action {
	case "pause":
		action = s.control.pause
	case "resume":
		action = s.control.resume
	case "next":
		action = s.control.next
	case "previous":
		action = s.control.previous
	default:
		http.Error(w, `"action" must be one of pause, resume, next, previous`, http.StatusBadRequest)
		return
	}

	if err := action(r.Context(), token); err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, errNoActiveDevice):
			status = http.StatusConflict
		case errors.Is(err, errPremiumRequired):
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}

	// After a skip the next poll can still read the old track, so flag it for
	// poll to retry. Pause and resume show up immediately.
	if body.Action == "next" || body.Action == "previous" {
		s.expectingChange.Store(true)
	}

	// Poll now so the panel shows the change at once.
	if s.repoll != nil {
		s.repoll()
	}

	w.WriteHeader(http.StatusOK)
}

// OpenRoutes registers the OAuth callback, reachable without the bearer token.
// The state parameter checked by authManager protects it.
func (s *apiSource) OpenRoutes() map[string]http.Handler {
	return map[string]http.Handler{
		callbackPath: http.HandlerFunc(s.auth.handleCallback),
	}
}

// Poll delegates to poll, which tests drive against fakes.
func (s *apiSource) Poll(ctx context.Context) (any, error) {
	reading, err := s.poll(ctx)
	if err != nil {
		return nil, err
	}
	return reading, nil
}

// poll fetches what is playing and refreshes the cached art if the track
// changed. Not connected is a failure. Nothing playing is an empty reading.
func (s *apiSource) poll(ctx context.Context) (Reading, error) {
	token, err := s.tokens.accessToken(ctx)
	if err != nil {
		return Reading{}, s.connectionError(err)
	}

	np, err := s.playback.fetchCurrentlyPlaying(ctx, token)
	if err != nil {
		if err == errAccessTokenExpired {
			// Spotify rejected a token the cache thought good (revocation or
			// clock skew). Retry once.
			token, err = s.tokens.accessToken(ctx)
			if err != nil {
				return Reading{}, s.connectionError(err)
			}
			np, err = s.playback.fetchCurrentlyPlaying(ctx, token)
			if err != nil {
				return Reading{}, fmt.Errorf("spotify: %w", err)
			}
		} else {
			return Reading{}, fmt.Errorf("spotify: %w", err)
		}
	}

	// Consumed whatever np is, so a stale flag cannot leak into a later poll.
	expecting := s.expectingChange.Swap(false)
	if expecting && np != nil {
		np = s.awaitTrackChange(ctx, token, np)
	}

	if np == nil {
		s.previousTrackID = ""
		return Reading{Layout: s.layout}, nil
	}
	s.previousTrackID = np.TrackID

	reading := Reading{
		Title:      np.Title,
		Artist:     np.Artist,
		Album:      np.Album,
		PositionMS: np.PositionMS,
		DurationMS: np.DurationMS,
		Playing:    np.Playing,
		Layout:     s.layout,
	}

	if np.ArtImageURL != "" {
		if err := s.ensureArtCached(ctx, np.TrackID, np.ArtImageURL); err != nil {
			// The face renders without art, so keep the rest of the reading.
			s.hasArt = false
		} else {
			// The track ID busts the cache. With a bare artPath the client's
			// same-URL guard in setArt() would keep the first cover.
			reading.ArtURL = artPath + "?track=" + url.QueryEscape(np.TrackID)
		}
	}

	return reading, nil
}

// awaitTrackChange retries a fetch that still shows the pre-skip track, up to
// consistencyRetries times. It is best effort. A stale reading or a fetch
// error keeps the last good np, so an already successful poll stays one.
func (s *apiSource) awaitTrackChange(ctx context.Context, token string, np *nowPlaying) *nowPlaying {
	for attempt := 0; attempt < consistencyRetries; attempt++ {
		if s.previousTrackID == "" || np.TrackID != s.previousTrackID {
			return np
		}
		s.sleep(ctx, consistencyDelay)
		if ctx.Err() != nil {
			return np
		}
		next, err := s.playback.fetchCurrentlyPlaying(ctx, token)
		if err != nil || next == nil {
			return np
		}
		np = next
	}
	return np
}

// ensureArtCached downloads the cover only when the track changed.
func (s *apiSource) ensureArtCached(ctx context.Context, trackID, imageURL string) error {
	if s.hasArt && trackID == s.lastTrackID {
		return nil
	}

	data, err := s.art.download(ctx, imageURL)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(s.artCachePath, data, 0o600); err != nil {
		return err
	}

	s.lastTrackID = trackID
	s.hasArt = true
	return nil
}

// connectionError adds a connect hint to a token failure. The status face
// shows it, and the fix is the same for a first connection or a revoked one.
func (s *apiSource) connectionError(err error) error {
	return fmt.Errorf("%w: visit %s on this agent to connect your spotify account", err, connectPath)
}
