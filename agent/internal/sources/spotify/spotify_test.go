package spotify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

func source(t *testing.T, settings string) *Source {
	t.Helper()
	built, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(settings)})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	src, ok := built.(*Source)
	if !ok {
		t.Fatalf("New returned %T, want *Source (a mock)", built)
	}
	return src
}

func poll(t *testing.T, src *Source) Reading {
	t.Helper()
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll returned an error: %v", err)
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
		t.Error("the mock should report something playing")
	}
}

func TestMockPositionAdvancesWithTime(t *testing.T) {
	// A still position would make the face look frozen, which is exactly the
	// symptom of a broken feed. The mock has to move.
	src := source(t, mockSettings)
	base := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	src.now = func() time.Time { return base }
	first := poll(t, src)

	src.now = func() time.Time { return base.Add(7 * time.Second) }
	second := poll(t, src)

	if second.PositionMS <= first.PositionMS {
		t.Errorf("position did not advance: %d then %d", first.PositionMS, second.PositionMS)
	}
	if got := second.PositionMS - first.PositionMS; got != 7000 {
		t.Errorf("position advanced by %dms over 7s, want 7000", got)
	}
}

func TestMockPositionWrapsAtTheEndOfTheTrack(t *testing.T) {
	src := source(t, mockSettings)
	base := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	src.now = func() time.Time { return base.Add(400 * time.Second) }

	reading := poll(t, src)

	if reading.PositionMS >= reading.DurationMS {
		t.Errorf("position %d is past the duration %d", reading.PositionMS, reading.DurationMS)
	}
	if reading.PositionMS < 0 {
		t.Errorf("position %d is negative", reading.PositionMS)
	}
}

func TestArtURLIsServedByTheAgentWhenAFileIsConfigured(t *testing.T) {
	// The device is on a LAN with an agent that will hold the credentials. It
	// should load one small image from the machine next to it rather than
	// reaching a CDN on every track change.
	src := source(t, `{"mode":"mock","art_file":"cover.png","track":"a","artist":"b","duration_ms":1000}`)

	reading := poll(t, src)

	if reading.ArtURL == "" {
		t.Fatal("an art file was configured but no art URL was published")
	}
	if strings.HasPrefix(reading.ArtURL, "http") {
		t.Errorf("ArtURL = %q, want a path served by the agent itself", reading.ArtURL)
	}
}

func TestNoArtURLWithoutAFile(t *testing.T) {
	src := source(t, `{"mode":"mock","track":"a","artist":"b","duration_ms":1000}`)

	reading := poll(t, src)

	if reading.ArtURL != "" {
		t.Errorf("ArtURL = %q, want empty when no art is configured", reading.ArtURL)
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
			t.Errorf("asset URL %q should be a path", url)
		}
		if path != "C:/covers/cover.png" {
			t.Errorf("asset path = %q", path)
		}
	}
}

func TestModeIsRequired(t *testing.T) {
	// Running the mock by accident and believing it is real would be worse than
	// refusing to start, so the mode has to be stated.
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"enabled":true}`)})

	if err == nil {
		t.Fatal("New should require an explicit mode")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error should name the missing setting, got: %v", err)
	}
}

func TestApiModeRequiresAClientID(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"mode":"api"}`)})

	if err == nil {
		t.Fatal("New should refuse api mode with no client_id")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Errorf("error should name the missing setting, got: %v", err)
	}
}

func TestUnknownModeIsRefused(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"mode":"spotifyish"}`)})

	if err == nil {
		t.Fatal("New should refuse an unknown mode")
	}
	if !strings.Contains(err.Error(), "spotifyish") {
		t.Errorf("error should name the offending value, got: %v", err)
	}
}

func TestMockRequiresATrack(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"mode":"mock"}`)})

	if err == nil {
		t.Fatal("the mock should refuse to run with nothing to play")
	}
	if !strings.Contains(err.Error(), "track") {
		t.Errorf("error should name the missing setting, got: %v", err)
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
			t.Errorf("encoded reading is missing %s\n%s", key, encoded)
		}
	}
}

func TestLayoutDefaultsToFill(t *testing.T) {
	src := source(t, mockSettings)

	reading := poll(t, src)

	if reading.Layout != "fill" {
		t.Errorf("Layout = %q, want fill when nothing is configured", reading.Layout)
	}
}

func TestLayoutIsConfigurable(t *testing.T) {
	src := source(t, `{"mode":"mock","track":"a","artist":"b","duration_ms":1000,"layout":"disc"}`)

	reading := poll(t, src)

	if reading.Layout != "disc" {
		t.Errorf("Layout = %q, want disc as configured", reading.Layout)
	}
}

func TestInvalidLayoutIsRejected(t *testing.T) {
	_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(
		`{"mode":"mock","track":"a","duration_ms":1000,"layout":"sideways"}`,
	)})

	if err == nil {
		t.Fatal("New should reject an unrecognised layout")
	}
	if !strings.Contains(err.Error(), "layout") {
		t.Errorf("error should name the offending setting, got: %v", err)
	}
}
