package spotify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// fakePlaybackFetcherFunc lets a test vary its response across calls.
type fakePlaybackFetcherFunc struct {
	fn func(context.Context, string) (*nowPlaying, error)
}

func (f *fakePlaybackFetcherFunc) fetchCurrentlyPlaying(ctx context.Context, token string) (*nowPlaying, error) {
	return f.fn(ctx, token)
}
