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
		// No real delay in the retry tests.
		sleep: func(context.Context, time.Duration) {},
	}
}

func TestPollWithNoConnectionIsAPlainFailure(t *testing.T) {
	tokens := &fakeTokenSource{err: errors.New("spotify: not connected yet")}
	src := newTestAPISource(t, tokens, &fakePlaybackFetcher{}, &fakeDownloader{})

	_, err := src.poll(context.Background())

	if err == nil {
		t.Fatal("poll succeeded with no access token")
	}
	if partial.Is(err) {
		t.Error("not connected marked partial, want a plain failure")
	}
}

func TestPollReportsNothingPlayingAsSuccess(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	playback := &fakePlaybackFetcher{result: nil, err: nil}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{})

	reading, err := src.poll(context.Background())

	if err != nil {
		t.Fatalf("poll with nothing playing: %v", err)
	}
	if reading.Title != "" {
		t.Errorf("Title = %q, want empty", reading.Title)
	}
	if reading.Playing {
		t.Error("Playing = true, want false")
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
		t.Fatalf("poll: %v", err)
	}

	if reading.Title != "Peacefield - Live from Mexico City" {
		t.Errorf("Title = %q", reading.Title)
	}
	if reading.PositionMS != 30000 || reading.DurationMS != 342000 {
		t.Errorf("position/duration = %d/%d", reading.PositionMS, reading.DurationMS)
	}
	if !reading.Playing {
		t.Error("Playing = false, want true")
	}
}

func TestPollServesArtFromTheAgentNotSpotify(t *testing.T) {
	// The device must never be sent a CDN URL.
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
		t.Fatal("ArtURL is empty, want a path")
	}
	if strings.HasPrefix(reading.ArtURL, "http") {
		t.Errorf("ArtURL = %q, want a path on the agent", reading.ArtURL)
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
		t.Errorf("downloads for one track = %d, want 1", art.calls)
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
		t.Errorf("downloads for two tracks = %d, want 2", art.calls)
	}
	if art.urls[1] != "https://i.scdn.co/image/second" {
		t.Errorf("second download URL = %q", art.urls[1])
	}
}

// TestPollArtURLChangesWithTheTrack checks the URL differs per track. The
// client swaps its image only when the URL changes.
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
		t.Errorf("ArtURL = %q for both tracks, want it to change", first.ArtURL)
	}
	if !strings.HasPrefix(second.ArtURL, artPath) {
		t.Errorf("ArtURL = %q, want prefix %s", second.ArtURL, artPath)
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
		t.Fatalf("ReadFile: %v", err)
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
		t.Errorf("ArtURL = %q, want empty", reading.ArtURL)
	}
	if art.calls != 0 {
		t.Error("downloader called with no art to fetch")
	}
}

func TestPollRetriesOnceOnAnExpiredAccessToken(t *testing.T) {
	// Spotify rejects a token the cache thought valid: revocation or clock skew.
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
		t.Fatalf("poll: %v", err)
	}
	if reading.Title != "Recovered" {
		t.Errorf("Title = %q, want the retried reading", reading.Title)
	}
	if calls != 2 {
		t.Errorf("fetchCurrentlyPlaying called %d times, want 2", calls)
	}
}

// TestPollRetriesUntilTheTrackActuallyChanges covers a skip that Spotify's
// currently-playing endpoint has not reflected yet.
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
		t.Errorf("Title = %q, want the changed track", reading.Title)
	}
	if calls != 3 {
		t.Errorf("fetchCurrentlyPlaying called %d times, want 3 (1 initial + 2 retries)", calls)
	}
	if src.expectingChange.Load() {
		t.Error("expectingChange still set after the poll")
	}
}

// TestPollGivesUpAfterTheRetryBudget checks that a track that never changes
// still yields a reading once the retries run out.
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
		t.Fatalf("poll: %v", err)
	}
	if reading.Title != "Stuck Track" {
		t.Errorf("Title = %q, want the last reading", reading.Title)
	}
	if calls != 1+consistencyRetries {
		t.Errorf("fetchCurrentlyPlaying called %d times, want %d (1 initial + %d retries)", calls, 1+consistencyRetries, consistencyRetries)
	}
}

// TestPollDoesNotRetryWithoutAPendingControlAction checks that an unchanged
// track alone does not trigger retries.
func TestPollDoesNotRetryWithoutAPendingControlAction(t *testing.T) {
	tokens := &fakeTokenSource{token: "access-1"}
	calls := 0
	playback := &fakePlaybackFetcherFunc{fn: func(context.Context, string) (*nowPlaying, error) {
		calls++
		return &nowPlaying{Title: "Same Track", TrackID: "track-1", DurationMS: 1000}, nil
	}}
	src := newTestAPISource(t, tokens, playback, &fakeDownloader{})
	src.previousTrackID = "track-1"
	// expectingChange stays false.

	if _, err := src.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetchCurrentlyPlaying called %d times, want 1", calls)
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
				t.Errorf("expectingChange not set after %q", action)
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
				t.Errorf("expectingChange set after %q", action)
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
		t.Errorf("status = %d, want 400", rec.Code)
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
		t.Fatal("no active device reported success")
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
		t.Fatal("control action succeeded with no connection")
	}
}

func TestControlIsRegisteredAsAnAuthenticatedRoute(t *testing.T) {
	src := &apiSource{tokens: &fakeTokenSource{}, control: &fakeControlAPI{}}

	routes := src.Routes()

	if _, ok := routes[controlPath]; !ok {
		t.Errorf("Routes() = %v, want %s registered", routes, controlPath)
	}
	// It must require the bearer token, unlike the OAuth callback.
	if _, ok := src.OpenRoutes()[controlPath]; ok {
		t.Error("control route registered as open")
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
		t.Errorf("repoll called %d times after a failure, want 0", repolled)
	}
}

func TestControlRouteToleratesNoRepollRegistered(t *testing.T) {
	// repoll is nil before the runner exists.
	src := &apiSource{tokens: &fakeTokenSource{token: "t"}, control: &fakeControlAPI{}}

	src.handleControl(httptest.NewRecorder(), newControlRequest(t, "pause"))
}

// An unconnected source has backed off, so the callback must ask for a poll.
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
