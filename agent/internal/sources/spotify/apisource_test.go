package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/sources/partial"
)

// fakeTokenSource stands in for *authManager.
type fakeTokenSource struct {
	token string
	err   error
	calls int
}

func (f *fakeTokenSource) accessToken(context.Context) (string, error) {
	f.calls++
	return f.token, f.err
}

// fakePlaybackFetcher stands in for *apiClient.
type fakePlaybackFetcher struct {
	result *nowPlaying
	err    error
	calls  int
}

func (f *fakePlaybackFetcher) fetchCurrentlyPlaying(context.Context, string) (*nowPlaying, error) {
	f.calls++
	return f.result, f.err
}

// fakeDownloader stands in for the art fetcher.
type fakeDownloader struct {
	bytes []byte
	err   error
	calls int
	urls  []string
}

func (f *fakeDownloader) download(_ context.Context, url string) ([]byte, error) {
	f.calls++
	f.urls = append(f.urls, url)
	return f.bytes, f.err
}

func newTestAPISource(t *testing.T, tokens accessTokenSource, playback currentlyPlayingFetcher, art artDownloader) *apiSource {
	t.Helper()
	return &apiSource{
		tokens:       tokens,
		playback:     playback,
		art:          art,
		artCachePath: filepath.Join(t.TempDir(), "spotify_art.jpg"),
		// A no-op rather than ctxSleep: the consistency-retry tests exercise
		// the retry loop itself, not real wall-clock delay.
		sleep: func(context.Context, time.Duration) {},
	}
}

func TestPollWithNoConnectionIsAPlainFailure(t *testing.T) {
	tokens := &fakeTokenSource{err: errors.New("spotify: not connected yet")}
	src := newTestAPISource(t, tokens, &fakePlaybackFetcher{}, &fakeDownloader{})

	_, err := src.poll(context.Background())

	if err == nil {
		t.Fatal("poll should fail when there is no access token to use")
	}
	if partial.Is(err) {
		t.Error("not connected is a plain failure, not a partial result: there is nothing to show at all")
	}
}

func TestPollReportsNothingPlayingAsSuccess(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: nil, err: nil}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{})

	reading, err := src.poll(context.Background())

	if err != nil {
		t.Fatalf("nothing playing should not be an error, got: %v", err)
	}
	if reading.Title != "" {
		t.Errorf("Title = %q, want empty when nothing is playing", reading.Title)
	}
	if reading.Playing {
		t.Error("Playing should be false")
	}
}

func TestPollMapsANowPlayingReading(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: &nowPlaying{
		Title: "Peacefield - Live from Mexico City", Artist: "Ghost", Album: "2 Big To Rig",
		TrackID: "track-1", PositionMS: 30000, DurationMS: 342000, Playing: true,
		ArtImageURL: "https://i.scdn.co/image/medium",
	}}
	art := &fakeDownloader{bytes: []byte("fake jpeg bytes")}
	src := newTestAPISource(t, tokens, playback, art)

	reading, err := src.poll(context.Background())
	if err != nil {
		t.Fatalf("poll returned an error: %v", err)
	}

	if reading.Title != "Peacefield - Live from Mexico City" {
		t.Errorf("Title = %q", reading.Title)
	}
	if reading.PositionMS != 30000 || reading.DurationMS != 342000 {
		t.Errorf("position/duration = %d/%d", reading.PositionMS, reading.DurationMS)
	}
	if !reading.Playing {
		t.Error("Playing should be true")
	}
}

func TestPollServesArtFromTheAgentNotSpotify(t *testing.T) {
	// The device is on a LAN with an agent that holds the credentials. It
	// should never be told to reach a Spotify CDN URL directly.
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: &nowPlaying{
		Title: "A Track", TrackID: "track-1", DurationMS: 1000,
		ArtImageURL: "https://i.scdn.co/image/medium",
	}}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{bytes: []byte("art")})

	reading, err := src.poll(context.Background())
	if err != nil {
		t.Fatalf("poll: %v", err)
	}

	if reading.ArtURL == "" {
		t.Fatal("no art URL was published even though art is available")
	}
	if strings.HasPrefix(reading.ArtURL, "http") {
		t.Errorf("ArtURL = %q, want a path served by the agent, not the raw Spotify URL", reading.ArtURL)
	}
}

func TestPollDownloadsArtOnlyOncePerTrack(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: &nowPlaying{
		Title: "A Track", TrackID: "track-1", DurationMS: 1000,
		ArtImageURL: "https://i.scdn.co/image/medium",
	}}
	art := &fakeDownloader{bytes: []byte("art bytes")}
	src := newTestAPISource(t, tokens, playback, art)

	if _, err := src.poll(context.Background()); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if _, err := src.poll(context.Background()); err != nil {
		t.Fatalf("second poll: %v", err)
	}

	if art.calls != 1 {
		t.Errorf("art was downloaded %d times for the same track, want 1", art.calls)
	}
}

func TestPollRedownloadsArtWhenTheTrackChanges(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: &nowPlaying{
		Title: "First Track", TrackID: "track-1", DurationMS: 1000,
		ArtImageURL: "https://i.scdn.co/image/first",
	}}
	art := &fakeDownloader{bytes: []byte("art bytes")}
	src := newTestAPISource(t, tokens, playback, art)

	if _, err := src.poll(context.Background()); err != nil {
		t.Fatalf("first poll: %v", err)
	}

	playback.result = &nowPlaying{
		Title: "Second Track", TrackID: "track-2", DurationMS: 1000,
		ArtImageURL: "https://i.scdn.co/image/second",
	}
	if _, err := src.poll(context.Background()); err != nil {
		t.Fatalf("second poll: %v", err)
	}

	if art.calls != 2 {
		t.Errorf("art was downloaded %d times across two different tracks, want 2", art.calls)
	}
	if art.urls[1] != "https://i.scdn.co/image/second" {
		t.Errorf("second download URL = %q", art.urls[1])
	}
}

// TestPollArtURLChangesWithTheTrack is what actually makes a new cover show
// up on the panel: the client only swaps its <img> when the URL differs from
// what it is already showing, and a browser will not re-fetch an unchanged
// URL either way. A fixed "/art/spotify" for every track, even though the
// file underneath is correctly re-downloaded, means the cover freezes on
// whatever track first set it.
func TestPollArtURLChangesWithTheTrack(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: &nowPlaying{
		Title: "First Track", TrackID: "track-1", DurationMS: 1000,
		ArtImageURL: "https://i.scdn.co/image/first",
	}}
	art := &fakeDownloader{bytes: []byte("art bytes")}
	src := newTestAPISource(t, tokens, playback, art)

	first, err := src.poll(context.Background())
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}

	playback.result = &nowPlaying{
		Title: "Second Track", TrackID: "track-2", DurationMS: 1000,
		ArtImageURL: "https://i.scdn.co/image/second",
	}
	second, err := src.poll(context.Background())
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}

	if first.ArtURL == second.ArtURL {
		t.Errorf("ArtURL was %q for both tracks, want it to change so the client actually reloads the image", first.ArtURL)
	}
	if !strings.HasPrefix(second.ArtURL, artPath) {
		t.Errorf("ArtURL = %q, want it to still be served from %s", second.ArtURL, artPath)
	}
}

func TestPollWritesArtToTheCachePath(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: &nowPlaying{
		Title: "A Track", TrackID: "track-1", DurationMS: 1000,
		ArtImageURL: "https://i.scdn.co/image/x",
	}}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{bytes: []byte("the actual bytes")})

	if _, err := src.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	got, err := os.ReadFile(src.artCachePath)
	if err != nil {
		t.Fatalf("reading the cached art: %v", err)
	}
	if string(got) != "the actual bytes" {
		t.Errorf("cached art = %q", got)
	}
}

func TestPollWithNoArtDoesNotTouchTheCache(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: &nowPlaying{Title: "No Art", TrackID: "t", DurationMS: 1000}}
	art := &fakeDownloader{}
	src := newTestAPISource(t, tokens, playback, art)

	reading, err := src.poll(context.Background())
	if err != nil {
		t.Fatalf("poll: %v", err)
	}

	if reading.ArtURL != "" {
		t.Errorf("ArtURL = %q, want empty when the track has no art", reading.ArtURL)
	}
	if art.calls != 0 {
		t.Error("the downloader should not be called when there is no art to fetch")
	}
}

func TestPollRetriesOnceOnAnExpiredAccessToken(t *testing.T) {
	// The cached token looked valid but Spotify rejected it anyway, which can
	// happen on revocation mid-session or clock skew. One retry after a forced
	// refresh is worth it before giving up.
	tokens := &fakeTokenSource{token: "stale-access"}
	calls := 0
	playback := &fakePlaybackFetcherFunc{fn: func(context.Context, string) (*nowPlaying, error) {
		calls++
		if calls == 1 {
			return nil, errAccessTokenExpired
		}
		return &nowPlaying{Title: "Recovered", TrackID: "t", DurationMS: 1000}, nil
	}}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{})

	reading, err := src.poll(context.Background())

	if err != nil {
		t.Fatalf("poll should recover after one retry, got: %v", err)
	}
	if reading.Title != "Recovered" {
		t.Errorf("Title = %q, want the reading from the retried call", reading.Title)
	}
	if calls != 2 {
		t.Errorf("fetchCurrentlyPlaying called %d times, want 2", calls)
	}
}

// TestPollRetriesUntilTheTrackActuallyChanges covers the case measured
// live: a control action succeeds, but Spotify's own currently-playing
// endpoint still reports the track from before it for a beat. The poll
// immediately after should not settle for that stale read when it knows a
// change is expected.
func TestPollRetriesUntilTheTrackActuallyChanges(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	calls := 0
	playback := &fakePlaybackFetcherFunc{fn: func(context.Context, string) (*nowPlaying, error) {
		calls++
		if calls < 3 {
			return &nowPlaying{Title: "Old Track", TrackID: "track-1", DurationMS: 1000}, nil
		}
		return &nowPlaying{Title: "New Track", TrackID: "track-2", DurationMS: 1000}, nil
	}}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{})
	src.previousTrackID = "track-1"
	src.expectingChange.Store(true)

	reading, err := src.poll(context.Background())

	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if reading.Title != "New Track" {
		t.Errorf("Title = %q, want the reading was retried until it actually changed", reading.Title)
	}
	if calls != 3 {
		t.Errorf("fetchCurrentlyPlaying called %d times, want 3 (1 initial + 2 retries)", calls)
	}
	if src.expectingChange.Load() {
		t.Error("expectingChange should be consumed by the poll that acted on it")
	}
}

// TestPollGivesUpAfterTheRetryBudget checks the retry loop is bounded: a
// track that never changes across the whole budget still produces a
// reading, not an error, once the budget runs out.
func TestPollGivesUpAfterTheRetryBudget(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	calls := 0
	playback := &fakePlaybackFetcherFunc{fn: func(context.Context, string) (*nowPlaying, error) {
		calls++
		return &nowPlaying{Title: "Stuck Track", TrackID: "track-1", DurationMS: 1000}, nil
	}}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{})
	src.previousTrackID = "track-1"
	src.expectingChange.Store(true)

	reading, err := src.poll(context.Background())

	if err != nil {
		t.Fatalf("a still-stale reading after the retry budget should not be an error: %v", err)
	}
	if reading.Title != "Stuck Track" {
		t.Errorf("Title = %q, want the last reading even though it never changed", reading.Title)
	}
	if calls != 1+consistencyRetries {
		t.Errorf("fetchCurrentlyPlaying called %d times, want %d (1 initial + %d retries)", calls, 1+consistencyRetries, consistencyRetries)
	}
}

// TestPollDoesNotRetryWithoutAPendingControlAction is the common case: most
// polls are not immediately after a skip, and must not pay the retry cost
// just because the track happens to be unchanged, which is the ordinary,
// expected state most of the time.
func TestPollDoesNotRetryWithoutAPendingControlAction(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	calls := 0
	playback := &fakePlaybackFetcherFunc{fn: func(context.Context, string) (*nowPlaying, error) {
		calls++
		return &nowPlaying{Title: "Same Track", TrackID: "track-1", DurationMS: 1000}, nil
	}}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{})
	src.previousTrackID = "track-1"
	// expectingChange left false: no control action is pending.

	if _, err := src.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetchCurrentlyPlaying called %d times, want 1 (no retry without a pending control action)", calls)
	}
}

func TestHandleControlFlagsExpectingChangeOnNextAndPrevious(t *testing.T) {
	for _, action := range []string{"next", "previous"} {
		t.Run(action, func(t *testing.T) {
			src := &apiSource{tokens: &fakeTokenSource{token: "t"}, control: &fakeControlAPI{}}

			rec := httptest.NewRecorder()
			src.handleControl(rec, newControlRequest(t, action))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !src.expectingChange.Load() {
				t.Errorf("expectingChange should be set after a successful %q", action)
			}
		})
	}
}

func TestHandleControlDoesNotFlagExpectingChangeOnPauseOrResume(t *testing.T) {
	for _, action := range []string{"pause", "resume"} {
		t.Run(action, func(t *testing.T) {
			src := &apiSource{tokens: &fakeTokenSource{token: "t"}, control: &fakeControlAPI{}}

			rec := httptest.NewRecorder()
			src.handleControl(rec, newControlRequest(t, action))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if src.expectingChange.Load() {
				t.Errorf("expectingChange should not be set after %q, there is no track ambiguity to retry", action)
			}
		})
	}
}

// fakePlaybackFetcherFunc lets a test vary its response across calls.
type fakePlaybackFetcherFunc struct {
	fn func(context.Context, string) (*nowPlaying, error)
}

func (f *fakePlaybackFetcherFunc) fetchCurrentlyPlaying(ctx context.Context, token string) (*nowPlaying, error) {
	return f.fn(ctx, token)
}

// --- Playback control route ---

type fakeControlAPI struct {
	fakePlaybackFetcher
	pauseErr, resumeErr, nextErr, prevErr         error
	pauseCalls, resumeCalls, nextCalls, prevCalls int
}

func (f *fakeControlAPI) pause(context.Context, string) error    { f.pauseCalls++; return f.pauseErr }
func (f *fakeControlAPI) resume(context.Context, string) error   { f.resumeCalls++; return f.resumeErr }
func (f *fakeControlAPI) next(context.Context, string) error     { f.nextCalls++; return f.nextErr }
func (f *fakeControlAPI) previous(context.Context, string) error { f.prevCalls++; return f.prevErr }

func newControlRequest(t *testing.T, action string) *http.Request {
	t.Helper()
	body := strings.NewReader(`{"action":"` + action + `"}`)
	return httptest.NewRequest(http.MethodPost, "/spotify/control", body)
}

func TestControlRouteDispatchesEachAction(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	api := &fakeControlAPI{}
	src := &apiSource{tokens: tokens, control: api}

	for action, calls := range map[string]func() int{
		"pause":    func() int { return api.pauseCalls },
		"resume":   func() int { return api.resumeCalls },
		"next":     func() int { return api.nextCalls },
		"previous": func() int { return api.prevCalls },
	} {
		rec := httptest.NewRecorder()
		src.handleControl(rec, newControlRequest(t, action))
		if rec.Code != http.StatusOK {
			t.Errorf("action %q: status = %d, body %s", action, rec.Code, rec.Body.String())
		}
		if got := calls(); got != 1 {
			t.Errorf("action %q: dispatched %d times, want 1", action, got)
		}
	}
}

func TestControlRouteRejectsAnUnknownAction(t *testing.T) {
	src := &apiSource{tokens: &fakeTokenSource{token: "t"}, control: &fakeControlAPI{}}

	rec := httptest.NewRecorder()
	src.handleControl(rec, newControlRequest(t, "shuffle"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an action outside pause/resume/next/previous", rec.Code)
	}
}

func TestControlRouteReportsNoActiveDeviceClearly(t *testing.T) {
	src := &apiSource{
		tokens:  &fakeTokenSource{token: "t"},
		control: &fakeControlAPI{pauseErr: errNoActiveDevice},
	}

	rec := httptest.NewRecorder()
	src.handleControl(rec, newControlRequest(t, "pause"))

	if rec.Code == http.StatusOK {
		t.Fatal("no active device should not report success")
	}
	if !strings.Contains(rec.Body.String(), "no active device") {
		t.Errorf("body = %q, want the specific reason", rec.Body.String())
	}
}

func TestControlRouteFailsWhenNotConnected(t *testing.T) {
	src := &apiSource{
		tokens:  &fakeTokenSource{err: errors.New("spotify: not connected yet")},
		control: &fakeControlAPI{},
	}

	rec := httptest.NewRecorder()
	src.handleControl(rec, newControlRequest(t, "pause"))

	if rec.Code == http.StatusOK {
		t.Fatal("a control action with no connection should not succeed")
	}
}

func TestControlIsRegisteredAsAnAuthenticatedRoute(t *testing.T) {
	src := &apiSource{tokens: &fakeTokenSource{}, control: &fakeControlAPI{}}

	routes := src.Routes()

	if _, ok := routes[controlPath]; !ok {
		t.Errorf("Routes() = %v, want %s registered", routes, controlPath)
	}
	// Confirm it is not also an open route: a control action must require the
	// bearer token, unlike the OAuth callback.
	if _, ok := src.OpenRoutes()[controlPath]; ok {
		t.Error("the control route must not be registered as open")
	}
}

func TestControlRouteTriggersARepollOnSuccess(t *testing.T) {
	repolled := 0
	src := &apiSource{
		tokens:  &fakeTokenSource{token: "t"},
		control: &fakeControlAPI{},
	}
	src.SetRepoll(func() { repolled++ })

	rec := httptest.NewRecorder()
	src.handleControl(rec, newControlRequest(t, "pause"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if repolled != 1 {
		t.Errorf("repoll called %d times, want 1", repolled)
	}
}

func TestControlRouteDoesNotRepollOnFailure(t *testing.T) {
	repolled := 0
	src := &apiSource{
		tokens:  &fakeTokenSource{token: "t"},
		control: &fakeControlAPI{pauseErr: errNoActiveDevice},
	}
	src.SetRepoll(func() { repolled++ })

	src.handleControl(httptest.NewRecorder(), newControlRequest(t, "pause"))

	if repolled != 0 {
		t.Errorf("repoll called %d times on a failed action, want 0", repolled)
	}
}

func TestControlRouteToleratesNoRepollRegistered(t *testing.T) {
	// Before the runner exists, repoll is nil. Must not panic.
	src := &apiSource{tokens: &fakeTokenSource{token: "t"}, control: &fakeControlAPI{}}

	src.handleControl(httptest.NewRecorder(), newControlRequest(t, "pause"))
}

// A source that is not connected yet fails every poll and backs off, so the
// callback finishing has to ask for a poll itself or the first reading waits
// out the backoff.
func TestConnectingRepollsImmediately(t *testing.T) {
	mgr, _ := newTestAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600})
	})
	src := &apiSource{auth: mgr}
	repolled := 0
	src.SetRepoll(func() { repolled++ })

	if rec := completeCallback(t, mgr); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if repolled != 1 {
		t.Errorf("repoll called %d times after connecting, want 1", repolled)
	}
}
