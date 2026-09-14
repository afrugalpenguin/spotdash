package spotify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"
)

// accessTokenSource is satisfied by *authManager. A narrow interface here
// keeps apiSource testable without a real HTTP round trip.
type accessTokenSource interface {
	accessToken(ctx context.Context) (string, error)
}

// currentlyPlayingFetcher is satisfied by *apiClient.
type currentlyPlayingFetcher interface {
	fetchCurrentlyPlaying(ctx context.Context, accessToken string) (*nowPlaying, error)
}

// artDownloader fetches raw image bytes from a URL.
type artDownloader interface {
	download(ctx context.Context, url string) ([]byte, error)
}

// httpArtDownloader is the real downloader, used outside tests.
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

// apiSource is the real Spotify provider: OAuth via authManager, polling via
// apiClient, with album art fetched and cached locally so the device never
// reaches a CDN directly.
type apiSource struct {
	interval time.Duration
	layout   string

	tokens   accessTokenSource
	playback currentlyPlayingFetcher
	art      artDownloader

	artCachePath string
	lastTrackID  string
	hasArt       bool

	// Exposed so the app wiring can mount the OAuth routes.
	auth *authManager
}

// newAPISourceFromSettings validates api-mode settings and builds the real
// source. Failing here, before the agent starts, is what "fail closed" means
// for this mode: a missing client_id is a startup error, not a source that
// silently never authorises.
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

	return &apiSource{
		interval:     interval,
		layout:       s.Layout,
		tokens:       auth,
		playback:     newAPIClient(),
		art:          &httpArtDownloader{http: &http.Client{Timeout: 10 * time.Second}},
		artCachePath: filepath.Join(filepath.Dir(s.StateFile), "spotify_art.jpg"),
		auth:         auth,
	}, nil
}

// Name identifies the source.
func (s *apiSource) Name() string { return Name }

// Interval is the configured poll period.
func (s *apiSource) Interval() time.Duration { return s.interval }

// Assets declares the cached art file, using the same path resolution the
// mock uses: the source points the agent at wherever the current cover
// happens to be.
func (s *apiSource) Assets() map[string]string {
	return map[string]string{artPath: s.artCachePath}
}

// Routes registers the page that starts a fresh authorization. Requires the
// bearer token, the same as everything else on the agent that is not
// /health or the OAuth callback itself.
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
	}
}

// OpenRoutes registers the OAuth callback, which must be reachable without
// the bearer token: the browser tab Spotify redirects to is freshly opened
// and carries none. The state parameter authManager checks is what protects
// it instead.
func (s *apiSource) OpenRoutes() map[string]http.Handler {
	return map[string]http.Handler{
		callbackPath: http.HandlerFunc(s.auth.handleCallback),
	}
}

// Poll delegates to the unexported poll, which is what the tests drive
// directly against fakes.
func (s *apiSource) Poll(ctx context.Context) (any, error) {
	reading, err := s.poll(ctx)
	if err != nil {
		return nil, err
	}
	return reading, nil
}

// poll fetches what is playing and refreshes the cached art if the track
// changed.
//
// Not connected, or a reauthorisation requirement, is a plain failure: there
// is nothing at all to publish. Nothing currently playing is success with an
// empty reading, because it is a normal state, not a problem.
func (s *apiSource) poll(ctx context.Context) (Reading, error) {
	token, err := s.tokens.accessToken(ctx)
	if err != nil {
		return Reading{}, s.connectionError(err)
	}

	np, err := s.playback.fetchCurrentlyPlaying(ctx, token)
	if err != nil {
		if err == errAccessTokenExpired {
			// The cache believed the token was still good but Spotify
			// disagreed, most likely a revocation mid-session or clock skew.
			// One retry after a forced refresh is worth it before giving up.
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

	if np == nil {
		return Reading{Layout: s.layout}, nil
	}

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
			// A cover that fails to download is not worth losing the rest of
			// the reading over. The face already renders without art.
			s.hasArt = false
		} else {
			reading.ArtURL = artPath
		}
	}

	return reading, nil
}

// ensureArtCached downloads the cover only when the track actually changed.
// Re-fetching on every poll, at a five second interval, would be constant
// unnecessary network and disk work for an image that has not changed.
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

// connectionError turns a token failure into a message a person reads on the
// status face, since that is the only place any of this surfaces
// automatically. Whether this is the first connection or a revoked one, the
// fix is the same, so both get the same actionable hint.
func (s *apiSource) connectionError(err error) error {
	return fmt.Errorf("%w: visit %s on this agent to connect your spotify account", err, connectPath)
}
