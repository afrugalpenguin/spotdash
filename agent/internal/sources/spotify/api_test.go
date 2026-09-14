package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeAPIServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *apiClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := &apiClient{endpoint: srv.URL, http: srv.Client()}
	return srv, client
}

const trackResponse = `{
	"is_playing": true,
	"progress_ms": 30872,
	"currently_playing_type": "track",
	"item": {
		"id": "5f1a2b3c4d5e6f7a8b9c0d1e",
		"name": "Peacefield - Live from Mexico City",
		"duration_ms": 342000,
		"artists": [{"name": "Ghost"}],
		"album": {
			"name": "2 Big To Rig",
			"images": [
				{"url": "https://i.scdn.co/image/large", "width": 640, "height": 640},
				{"url": "https://i.scdn.co/image/medium", "width": 300, "height": 300},
				{"url": "https://i.scdn.co/image/small", "width": 64, "height": 64}
			]
		}
	}
}`

func TestFetchParsesAPlayingTrack(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(trackResponse))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "test-access-token")
	if err != nil {
		t.Fatalf("fetchCurrentlyPlaying returned an error: %v", err)
	}

	if np.Title != "Peacefield - Live from Mexico City" {
		t.Errorf("Title = %q", np.Title)
	}
	if np.Artist != "Ghost" {
		t.Errorf("Artist = %q", np.Artist)
	}
	if np.Album != "2 Big To Rig" {
		t.Errorf("Album = %q", np.Album)
	}
	if np.PositionMS != 30872 {
		t.Errorf("PositionMS = %d", np.PositionMS)
	}
	if np.DurationMS != 342000 {
		t.Errorf("DurationMS = %d", np.DurationMS)
	}
	if !np.Playing {
		t.Error("Playing should be true")
	}
	if np.TrackID != "5f1a2b3c4d5e6f7a8b9c0d1e" {
		t.Errorf("TrackID = %q", np.TrackID)
	}
}

func TestFetchJoinsMultipleArtists(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_playing":true,"progress_ms":0,"currently_playing_type":"track","item":{
			"id":"x","name":"A Collab","duration_ms":1000,
			"artists":[{"name":"Ghost"},{"name":"Someone Else"}],
			"album":{"name":"An Album","images":[]}
		}}`))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")
	if err != nil {
		t.Fatalf("fetchCurrentlyPlaying: %v", err)
	}

	if np.Artist != "Ghost, Someone Else" {
		t.Errorf("Artist = %q, want both artists joined", np.Artist)
	}
}

func TestFetchPicksAnImageCloseToPanelSize(t *testing.T) {
	// The panel is 480px. Pulling the largest available image every poll is
	// wasted bandwidth and decode time on a weak device for no visible gain.
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(trackResponse))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")
	if err != nil {
		t.Fatalf("fetchCurrentlyPlaying: %v", err)
	}

	if np.ArtImageURL != "https://i.scdn.co/image/medium" {
		t.Errorf("ArtImageURL = %q, want the 300px image", np.ArtImageURL)
	}
}

func TestFetchHandlesNoImages(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_playing":true,"progress_ms":0,"currently_playing_type":"track","item":{
			"id":"x","name":"No Art","duration_ms":1000,"artists":[{"name":"A"}],
			"album":{"name":"B","images":[]}
		}}`))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")
	if err != nil {
		t.Fatalf("fetchCurrentlyPlaying: %v", err)
	}
	if np.ArtImageURL != "" {
		t.Errorf("ArtImageURL = %q, want empty when there are no images", np.ArtImageURL)
	}
}

func TestFetchTreatsNoContentAsNothingPlaying(t *testing.T) {
	// 204 is Spotify's documented response when there is no active playback.
	// This is a normal state, not a failure.
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err != nil {
		t.Fatalf("204 should not be an error, got: %v", err)
	}
	if np != nil {
		t.Errorf("np = %+v, want nil when nothing is playing", np)
	}
}

func TestFetchTreatsANullItemAsNothingPlaying(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_playing":false,"item":null}`))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err != nil {
		t.Fatalf("a null item should not be an error, got: %v", err)
	}
	if np != nil {
		t.Errorf("np = %+v, want nil", np)
	}
}

func TestFetchTreatsAnAdAsNothingPlaying(t *testing.T) {
	// currently_playing_type can be "ad" or "episode". Only "track" is rendered
	// for now, so an ad reads as nothing playing rather than showing its title.
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_playing":true,"currently_playing_type":"ad","item":{"name":"Ad","duration_ms":30000}}`))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err != nil {
		t.Fatalf("an ad should not be an error, got: %v", err)
	}
	if np != nil {
		t.Errorf("np = %+v, want nil for a non-track item", np)
	}
}

func TestFetchReportsAnExpiredToken(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := client.fetchCurrentlyPlaying(context.Background(), "expired-token")

	if !errors.Is(err, errAccessTokenExpired) {
		t.Errorf("err = %v, want errAccessTokenExpired so the caller knows to refresh and retry", err)
	}
}

func TestFetchSurfacesAnUnexpectedStatus(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err == nil {
		t.Fatal("fetchCurrentlyPlaying should return an error on 503")
	}
}
