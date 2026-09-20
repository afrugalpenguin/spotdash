package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

func source(t *testing.T, settings string) *Source {
	t.Helper()
	built, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(settings)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	src, ok := built.(*Source)
	if !ok {
		t.Fatalf("New returned %T, want *Source", built)
	}
	return src
}

func poll(t *testing.T, src *Source) Reading {
	t.Helper()
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading, ok := value.(Reading)
	if !ok {
		t.Fatalf("Poll returned %T, want Reading", value)
	}
	return reading
}

const mockSettings = `{
	"enabled": true,
	"interval_ms": 1000,
	"mode": "mock",
	"track": "Peacefield - Live from Mexico City",
	"artist": "Ghost",
	"album": "2 Big To Rig",
	"duration_ms": 342000
}`

func TestNameAndInterval(t *testing.T) {
	src := source(t, mockSettings)

	if src.Name() != "spotify" {
		t.Errorf("Name() = %q, want spotify", src.Name())
	}
	if got, want := src.Interval(), time.Second; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestMockReportsTheConfiguredTrack(t *testing.T) {
	src := source(t, mockSettings)

	reading := poll(t, src)

	if reading.Title != "Peacefield - Live from Mexico City" {
		t.Errorf("Title = %q", reading.Title)
	}
	if reading.Artist != "Ghost" {
		t.Errorf("Artist = %q", reading.Artist)
	}
	if reading.Album != "2 Big To Rig" {
		t.Errorf("Album = %q", reading.Album)
	}
	if reading.DurationMS != 342000 {
		t.Errorf("DurationMS = %d", reading.DurationMS)
	}
	if !reading.Playing {
		t.Error("Playing = false, want true")
	}
}

func TestMockPositionAdvancesWithTime(t *testing.T) {
	// A still position looks like a broken feed.
	src := source(t, mockSettings)
	base := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	src.now = func() time.Time { return base }
	first := poll(t, src)

	src.now = func() time.Time { return base.Add(7 * time.Second) }
	second := poll(t, src)

	if second.PositionMS <= first.PositionMS {
		t.Errorf("position %d then %d, want an advance", first.PositionMS, second.PositionMS)
	}
	if got := second.PositionMS - first.PositionMS; got != 7000 {
		t.Errorf("advance over 7s = %dms, want 7000", got)
	}
}

func TestMockPositionWrapsAtTheEndOfTheTrack(t *testing.T) {
	src := source(t, mockSettings)
	base := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	src.now = func() time.Time { return base.Add(400 * time.Second) }

	reading := poll(t, src)

	if reading.PositionMS >= reading.DurationMS {
		t.Errorf("position %d past duration %d", reading.PositionMS, reading.DurationMS)
	}
	if reading.PositionMS < 0 {
		t.Errorf("position = %d, want non-negative", reading.PositionMS)
	}
}

func TestArtURLIsServedByTheAgentWhenAFileIsConfigured(t *testing.T) {
	src := source(t, `{"mode":"mock","art_file":"cover.png","track":"a","artist":"b","duration_ms":1000}`)

	reading := poll(t, src)

	if reading.ArtURL == "" {
		t.Fatal("ArtURL is empty, want a path")
	}
	if strings.HasPrefix(reading.ArtURL, "http") {
		t.Errorf("ArtURL = %q, want a path on the agent", reading.ArtURL)
	}
}

func TestNoArtURLWithoutAFile(t *testing.T) {
	src := source(t, `{"mode":"mock","track":"a","artist":"b","duration_ms":1000}`)

	reading := poll(t, src)

	if reading.ArtURL != "" {
		t.Errorf("ArtURL = %q, want empty", reading.ArtURL)
	}
}

func TestAssetsExposesTheArtFile(t *testing.T) {
	src := source(t, `{"mode":"mock","art_file":"C:/covers/cover.png","track":"a","artist":"b","duration_ms":1000}`)

	assets := src.Assets()

	if len(assets) != 1 {
		t.Fatalf("Assets() = %v, want one entry", assets)
	}
	for url, path := range assets {
		if !strings.HasPrefix(url, "/") {
			t.Errorf("asset URL = %q, want a path", url)
		}
		if path != "C:/covers/cover.png" {
			t.Errorf("asset path = %q", path)
		}
	}
}

func TestModeIsRequired(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"enabled":true}`)})

	if err == nil {
		t.Fatal("New accepted a missing mode")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error = %v, want it to name mode", err)
	}
}

func TestApiModeRequiresAClientID(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"mode":"api"}`)})

	if err == nil {
		t.Fatal("New accepted api mode with no client_id")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Errorf("error = %v, want it to name client_id", err)
	}
}

func TestUnknownModeIsRefused(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"mode":"spotifyish"}`)})

	if err == nil {
		t.Fatal("New accepted an unknown mode")
	}
	if !strings.Contains(err.Error(), "spotifyish") {
		t.Errorf("error = %v, want it to name spotifyish", err)
	}
}

func TestMockRequiresATrack(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"mode":"mock"}`)})

	if err == nil {
		t.Fatal("New accepted a mock with no track")
	}
	if !strings.Contains(err.Error(), "track") {
		t.Errorf("error = %v, want it to name track", err)
	}
}

func TestReadingEncodesTheFieldsTheFaceNeeds(t *testing.T) {
	src := source(t, mockSettings)

	encoded, err := json.Marshal(poll(t, src))
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	for _, key := range []string{
		`"title"`, `"artist"`, `"album"`, `"art_url"`,
		`"position_ms"`, `"duration_ms"`, `"playing"`,
	} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("encoded reading lacks %s:\n%s", key, encoded)
		}
	}
}

func TestLayoutDefaultsToFill(t *testing.T) {
	src := source(t, mockSettings)

	reading := poll(t, src)

	if reading.Layout != "fill" {
		t.Errorf("Layout = %q, want fill", reading.Layout)
	}
}

func TestLayoutIsConfigurable(t *testing.T) {
	src := source(t, `{"mode":"mock","track":"a","artist":"b","duration_ms":1000,"layout":"disc"}`)

	reading := poll(t, src)

	if reading.Layout != "disc" {
		t.Errorf("Layout = %q, want disc", reading.Layout)
	}
}

func TestInvalidLayoutIsRejected(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(
		`{"mode":"mock","track":"a","duration_ms":1000,"layout":"sideways"}`,
	)})

	if err == nil {
		t.Fatal("New accepted an unknown layout")
	}
	if !strings.Contains(err.Error(), "layout") {
		t.Errorf("error = %v, want it to name layout", err)
	}
}

func sourceIn(t *testing.T, dir, settings string) any {
	t.Helper()
	built, err := New(config.Source{Enabled: true, IntervalMS: 1000, Dir: dir, Settings: []byte(settings)})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	return built
}

const apiSettings = `{"mode":"api","client_id":"id","redirect_uri":"http://127.0.0.1:8765/spotify/callback","state_file":"%s"}`

// A relative state_file is relative to config.json, not the working directory.
func TestARelativeStateFileResolvesAgainstTheConfigDirectory(t *testing.T) {
	dir := t.TempDir()

	built := sourceIn(t, dir, fmt.Sprintf(apiSettings, "spotify_state.json"))

	api := built.(*apiSource)
	if want := filepath.Join(dir, "spotify_state.json"); api.auth.statePath != want {
		t.Errorf("state path = %q, want %q", api.auth.statePath, want)
	}
	if want := filepath.Join(dir, "spotify_art.jpg"); api.artCachePath != want {
		t.Errorf("art cache path = %q, want %q", api.artCachePath, want)
	}
}

func TestAnAbsoluteStateFileIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(t.TempDir(), "elsewhere", "state.json")

	built := sourceIn(t, dir, fmt.Sprintf(apiSettings, filepath.ToSlash(abs)))

	if got := built.(*apiSource).auth.statePath; filepath.Clean(got) != filepath.Clean(abs) {
		t.Errorf("state path = %q, want %q", got, abs)
	}
}

func TestWithNoConfigDirectoryAPathIsLeftAsWritten(t *testing.T) {
	built := sourceIn(t, "", fmt.Sprintf(apiSettings, "spotify_state.json"))

	if got := built.(*apiSource).auth.statePath; got != "spotify_state.json" {
		t.Errorf("state path = %q, want spotify_state.json", got)
	}
}

func TestARelativeMockArtFileResolvesAgainstTheConfigDirectory(t *testing.T) {
	dir := t.TempDir()

	built := sourceIn(t, dir, `{"mode":"mock","track":"t","duration_ms":1000,"art_file":"cover.png"}`)

	assets := built.(*Source).Assets()
	if want := filepath.Join(dir, "cover.png"); assets[artPath] != want {
		t.Errorf("art file = %q, want %q", assets[artPath], want)
	}
}
