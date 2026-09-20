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

// defaultAPIEndpoint is the Spotify Web API base. Tests override it through
// apiClient.endpoint.
const defaultAPIEndpoint = "https://api.spotify.com"

// targetArtSize is roughly the panel's pixel size. The largest image would cost
// bandwidth and decode time on a weak device for no visible gain.
const targetArtSize = 300

// errAccessTokenExpired means the access token was rejected. The caller should
// refresh and retry once.
var errAccessTokenExpired = errors.New("spotify: access token expired")

// errNoActiveDevice and errPremiumRequired are the two common control failures.
// Playback control is a Premium-only part of the API.
var (
	errNoActiveDevice  = errors.New("spotify: no active device")
	errPremiumRequired = errors.New("spotify: playback control requires spotify premium")
)

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
// active device, nothing queued, or content that is not a track (an ad or a
// podcast episode).
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
		// Spotify's response when nothing is playing.
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

// controlErrorResponse is the error body of the player control endpoints:
// {"error": {"status": 404, "message": "...", "reason": "..."}}.
type controlErrorResponse struct {
	Error struct {
		Reason string `json:"reason"`
	} `json:"error"`
}

// pause, resume, next and previous send a playback command with no body.
func (c *apiClient) pause(ctx context.Context, accessToken string) error {
	return c.control(ctx, http.MethodPut, "/v1/me/player/pause", accessToken)
}

func (c *apiClient) resume(ctx context.Context, accessToken string) error {
	return c.control(ctx, http.MethodPut, "/v1/me/player/play", accessToken)
}

func (c *apiClient) next(ctx context.Context, accessToken string) error {
	return c.control(ctx, http.MethodPost, "/v1/me/player/next", accessToken)
}

func (c *apiClient) previous(ctx context.Context, accessToken string) error {
	return c.control(ctx, http.MethodPost, "/v1/me/player/previous", accessToken)
}

func (c *apiClient) control(ctx context.Context, method, path, accessToken string) error {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("reaching spotify: %w", err)
	}
	defer resp.Body.Close()

	// Documented success is 204, but /v1/me/player/play has returned a bare 200.
	// Accept any 2xx.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errAccessTokenExpired
	}

	var body controlErrorResponse
	// Best effort. On a decode failure the generic error below keeps the status.
	_ = json.NewDecoder(resp.Body).Decode(&body)

	switch body.Error.Reason {
	case "NO_ACTIVE_DEVICE":
		return errNoActiveDevice
	case "PREMIUM_REQUIRED":
		return errPremiumRequired
	}
	return fmt.Errorf("spotify returned HTTP %d for %s %s", resp.StatusCode, method, path)
}

// pickArt returns the smallest image at least targetArtSize wide, or the
// largest available when every image is smaller.
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
