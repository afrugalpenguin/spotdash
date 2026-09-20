package spotify

import (
	"context"
	"encoding/json"
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
		t.Fatalf("fetchCurrentlyPlaying: %v", err)
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
		t.Error("Playing = false, want true")
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
		t.Errorf("Artist = %q, want both artists", np.Artist)
	}
}

func TestFetchPicksAnImageCloseToPanelSize(t *testing.T) {
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
		t.Errorf("ArtImageURL = %q, want empty", np.ArtImageURL)
	}
}

func TestFetchTreatsNoContentAsNothingPlaying(t *testing.T) {
	// 204 means no active playback, which is not a failure.
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err != nil {
		t.Fatalf("fetchCurrentlyPlaying on 204: %v", err)
	}
	if np != nil {
		t.Errorf("np = %+v, want nil", np)
	}
}

func TestFetchTreatsANullItemAsNothingPlaying(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_playing":false,"item":null}`))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err != nil {
		t.Fatalf("fetchCurrentlyPlaying on null item: %v", err)
	}
	if np != nil {
		t.Errorf("np = %+v, want nil", np)
	}
}

func TestFetchTreatsAnAdAsNothingPlaying(t *testing.T) {
	// currently_playing_type can be "ad" or "episode". Only "track" is shown.
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_playing":true,"currently_playing_type":"ad","item":{"name":"Ad","duration_ms":30000}}`))
	})

	np, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err != nil {
		t.Fatalf("fetchCurrentlyPlaying on ad: %v", err)
	}
	if np != nil {
		t.Errorf("np = %+v, want nil", np)
	}
}

func TestFetchReportsAnExpiredToken(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := client.fetchCurrentlyPlaying(context.Background(), "expired-token")

	if !errors.Is(err, errAccessTokenExpired) {
		t.Errorf("err = %v, want errAccessTokenExpired", err)
	}
}

func TestFetchSurfacesAnUnexpectedStatus(t *testing.T) {
	_, client := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := client.fetchCurrentlyPlaying(context.Background(), "token")

	if err == nil {
		t.Fatal("fetchCurrentlyPlaying error = nil on 503")
	}
}

// --- Playback control ---

func fakeControlServer(t *testing.T, handler http.HandlerFunc) *apiClient {
	t.Helper()
	srv, client := fakeAPIServer(t, handler)
	_ = srv
	return client
}

func TestPauseSendsTheRightRequest(t *testing.T) {
	var method, path, auth string
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})

	err := client.pause(context.Background(), "test-token")

	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if method != http.MethodPut {
		t.Errorf("method = %q, want PUT", method)
	}
	if path != "/v1/me/player/pause" {
		t.Errorf("path = %q", path)
	}
	if auth != "Bearer test-token" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestResumeSendsTheRightRequest(t *testing.T) {
	var method, path string
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.resume(context.Background(), "test-token"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if method != http.MethodPut || path != "/v1/me/player/play" {
		t.Errorf("method/path = %s %s, want PUT /v1/me/player/play", method, path)
	}
}

func TestNextSendsTheRightRequest(t *testing.T) {
	var method, path string
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.next(context.Background(), "test-token"); err != nil {
		t.Fatalf("next: %v", err)
	}
	if method != http.MethodPost || path != "/v1/me/player/next" {
		t.Errorf("method/path = %s %s, want POST /v1/me/player/next", method, path)
	}
}

func TestPreviousSendsTheRightRequest(t *testing.T) {
	var method, path string
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.previous(context.Background(), "test-token"); err != nil {
		t.Fatalf("previous: %v", err)
	}
	if method != http.MethodPost || path != "/v1/me/player/previous" {
		t.Errorf("method/path = %s %s, want POST /v1/me/player/previous", method, path)
	}
}

func TestControlReportsNoActiveDevice(t *testing.T) {
	// Nothing is playing anywhere, so no device can take the command.
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"status": 404, "message": "Device not found", "reason": "NO_ACTIVE_DEVICE"},
		})
	})

	err := client.pause(context.Background(), "token")

	if !errors.Is(err, errNoActiveDevice) {
		t.Errorf("err = %v, want errNoActiveDevice", err)
	}
}

func TestControlReportsPremiumRequired(t *testing.T) {
	// A free account gets a 403 with this reason.
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"status": 403, "message": "Player command failed: Premium required", "reason": "PREMIUM_REQUIRED"},
		})
	})

	err := client.resume(context.Background(), "token")

	if !errors.Is(err, errPremiumRequired) {
		t.Errorf("err = %v, want errPremiumRequired", err)
	}
}

func TestControlReportsExpiredToken(t *testing.T) {
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	err := client.next(context.Background(), "token")

	if !errors.Is(err, errAccessTokenExpired) {
		t.Errorf("err = %v, want errAccessTokenExpired", err)
	}
}

func TestControlSurfacesAnUnrecognisedFailure(t *testing.T) {
	client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"status": 403, "message": "Restricted", "reason": "SOMETHING_NEW"},
		})
	})

	err := client.previous(context.Background(), "token")

	if err == nil {
		t.Fatal("previous error = nil on an unrecognised 403")
	}
	if errors.Is(err, errNoActiveDevice) || errors.Is(err, errPremiumRequired) || errors.Is(err, errAccessTokenExpired) {
		t.Error("unrecognised reason matched a known error")
	}
}

func TestControlTreatsAny2xxAsSuccess(t *testing.T) {
	// /v1/me/player/play has been seen returning a bare 200 instead of 204.
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		client := fakeControlServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})

		if err := client.resume(context.Background(), "token"); err != nil {
			t.Errorf("resume with status %d: %v", status, err)
		}
	}
}
