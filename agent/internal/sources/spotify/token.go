package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultTokenEndpoint is Spotify's token endpoint. Tests override it through
// tokenClient.endpoint.
const defaultTokenEndpoint = "https://accounts.spotify.com/api/token"

// tokenClient exchanges and refreshes tokens with the PKCE flow. There is no
// client secret, only the client ID and the code verifier.
type tokenClient struct {
	clientID string
	endpoint string
	http     *http.Client
}

func newTokenClient(clientID string) *tokenClient {
	return &tokenClient{
		clientID: clientID,
		endpoint: defaultTokenEndpoint,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// tokens holds an access token (about an hour) and a refresh token (until
// revoked).
type tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// tokenResponse is Spotify's wire format from the token endpoint.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	TokenType        string `json:"token_type"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// reauthRequiredError marks a refresh token that is no longer good. Retrying
// will not help, the user has to authorise again.
type reauthRequiredError struct {
	reason string
}

func (e *reauthRequiredError) Error() string {
	return fmt.Sprintf("spotify authorisation is no longer valid: %s", e.reason)
}

// IsReauthRequired reports whether err means the authorization must be redone.
func IsReauthRequired(err error) bool {
	var marked *reauthRequiredError
	return errors.As(err, &marked)
}

// exchange trades an authorization code for tokens. The request is form encoded
// per RFC 6749 and carries no secret.
func (c *tokenClient) exchange(ctx context.Context, code, verifier, redirectURI string) (tokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {c.clientID},
		"code_verifier": {verifier},
	}
	resp, err := c.post(ctx, form)
	if err != nil {
		return tokens{}, fmt.Errorf("exchanging the authorization code: %w", err)
	}
	return tokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
	}, nil
}

// refresh trades a refresh token for a new access token. Spotify does not
// always return a new refresh token, so the old one is kept then.
func (c *tokenClient) refresh(ctx context.Context, refreshToken string) (tokens, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.clientID},
	}
	resp, err := c.post(ctx, form)
	if err != nil {
		if resp := (*spotifyTokenError)(nil); errors.As(err, &resp) && resp.code == "invalid_grant" {
			return tokens{}, &reauthRequiredError{reason: resp.description}
		}
		return tokens{}, fmt.Errorf("refreshing the access token: %w", err)
	}

	newRefresh := resp.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken
	}
	return tokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: newRefresh,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
	}, nil
}

// spotifyTokenError is a non-2xx token response, per RFC 6749 section 5.2.
type spotifyTokenError struct {
	code        string
	description string
}

func (e *spotifyTokenError) Error() string {
	if e.description != "" {
		return fmt.Sprintf("spotify: %s: %s", e.code, e.description)
	}
	return fmt.Sprintf("spotify: %s", e.code)
}

func (c *tokenClient) post(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpResp, err := c.http.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer httpResp.Body.Close()

	var body tokenResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&body); err != nil {
		return tokenResponse{}, fmt.Errorf("decoding the token response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return tokenResponse{}, &spotifyTokenError{code: body.Error, description: body.ErrorDescription}
	}
	return body, nil
}
