package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// defaultAPIEndpoint is Spotify's real Web API base. Tests override it via
// apiClient.endpoint.
const defaultAPIEndpoint = "https://api.spotify.com"

// targetArtSize is roughly the panel's own pixel size. Spotify returns several
// image sizes; pulling the largest one every poll costs bandwidth and decode
// time on a weak device for no visible gain over one close to what is shown.
const targetArtSize = 300

// errAccessTokenExpired means the access token was rejected and the caller
// should refresh it and retry once, not treat this as a hard failure.
var errAccessTokenExpired = errors.New("spotify: access token expired")

// nowPlaying is what one poll of the Spotify Web API found.
type nowPlaying struct {
	Title       string
	Artist      string
	Album       string
	ArtImageURL string
	TrackID     string
	PositionMS  int64
	DurationMS  int64
	Playing     bool
}

type apiClient struct {
	endpoint string
	http     *http.Client
}

func newAPIClient() *apiClient {
	return &apiClient{
		endpoint: defaultAPIEndpoint,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// wire shapes for GET /v1/me/player/currently-playing.
type currentlyPlayingResponse struct {
	IsPlaying  bool              `json:"is_playing"`
	ProgressMS int64             `json:"progress_ms"`
	ItemType   string            `json:"currently_playing_type"`
	Item       *currentlyPlaying `json:"item"`
}

type currentlyPlaying struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	DurationMS int64           `json:"duration_ms"`
	Artists    []spotifyArtist `json:"artists"`
	Album      spotifyAlbum    `json:"album"`
}

type spotifyArtist struct {
	Name string `json:"name"`
}

type spotifyAlbum struct {
	Name   string         `json:"name"`
	Images []spotifyImage `json:"images"`
}

type spotifyImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// fetchCurrentlyPlaying returns what is playing, or nil when nothing is: no
// active device, a paused-with-nothing-queued session, or content that is not
// a track (an ad or, for now, a podcast episode).
func (c *apiClient) fetchCurrentlyPlaying(ctx context.Context, accessToken string) (*nowPlaying, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/me/player/currently-playing", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching spotify: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		// Documented behaviour for no active playback.
		return nil, nil
	case http.StatusUnauthorized:
		return nil, errAccessTokenExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("spotify returned HTTP %d", resp.StatusCode)
	}

	var body currentlyPlayingResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding the currently-playing response: %w", err)
	}

	if body.Item == nil || body.ItemType != "track" {
		return nil, nil
	}

	names := make([]string, 0, len(body.Item.Artists))
	for _, a := range body.Item.Artists {
		names = append(names, a.Name)
	}

	return &nowPlaying{
		Title:       body.Item.Name,
		Artist:      strings.Join(names, ", "),
		Album:       body.Item.Album.Name,
		ArtImageURL: pickArt(body.Item.Album.Images),
		TrackID:     body.Item.ID,
		PositionMS:  body.ProgressMS,
		DurationMS:  body.Item.DurationMS,
		Playing:     body.IsPlaying,
	}, nil
}

// pickArt returns the image closest to targetArtSize without going under it,
// falling back to the smallest available when every image is smaller.
func pickArt(images []spotifyImage) string {
	if len(images) == 0 {
		return ""
	}
	best := images[0]
	for _, img := range images {
		if img.Width >= targetArtSize && img.Width < best.Width {
			best = img
		} else if best.Width < targetArtSize && img.Width > best.Width {
			best = img
		}
	}
	return best.URL
}
