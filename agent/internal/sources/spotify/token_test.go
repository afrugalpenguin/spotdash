package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fakeTokenServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *tokenClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := &tokenClient{
		clientID: "test-client-id",
		endpoint: srv.URL,
		http:     srv.Client(),
	}
	return srv, client
}

func TestExchangeSendsThePKCEVerifierNotASecret(t *testing.T) {
	var got url.Values
	_, client := fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		got = r.PostForm
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "access-1",
			RefreshToken: "refresh-1",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		})
	})

	_, err := client.exchange(context.Background(), "auth-code", "the-verifier", "http://127.0.0.1:8765/spotify/callback")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	if got.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", got.Get("grant_type"))
	}
	if got.Get("code") != "auth-code" {
		t.Errorf("code = %q", got.Get("code"))
	}
	if got.Get("code_verifier") != "the-verifier" {
		t.Errorf("code_verifier = %q", got.Get("code_verifier"))
	}
	if got.Get("redirect_uri") != "http://127.0.0.1:8765/spotify/callback" {
		t.Errorf("redirect_uri = %q", got.Get("redirect_uri"))
	}
	if got.Get("client_id") != "test-client-id" {
		t.Errorf("client_id = %q", got.Get("client_id"))
	}
	if got.Has("client_secret") {
		t.Error("client_secret was sent")
	}
}

func TestExchangeReturnsTheTokens(t *testing.T) {
	_, client := fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "access-1",
			RefreshToken: "refresh-1",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		})
	})

	tok, err := client.exchange(context.Background(), "auth-code", "verifier", "redirect")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	if tok.AccessToken != "access-1" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
	if tok.RefreshToken != "refresh-1" {
		t.Errorf("RefreshToken = %q", tok.RefreshToken)
	}
	if tok.ExpiresAt.Before(time.Now().Add(59 * time.Minute)) {
		t.Errorf("ExpiresAt = %v, want about one hour out", tok.ExpiresAt)
	}
}

func TestExchangeSurfacesASpotifyError(t *testing.T) {
	_, client := fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "Authorization code expired",
		})
	})

	_, err := client.exchange(context.Background(), "stale-code", "verifier", "redirect")

	if err == nil {
		t.Fatal("exchange accepted an error response")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("error = %v, want Spotify's reason", err)
	}
}

func TestRefreshSendsTheRefreshToken(t *testing.T) {
	var got url.Values
	_, client := fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "access-2",
			RefreshToken: "refresh-2",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		})
	})

	tok, err := client.refresh(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if got.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q", got.Get("grant_type"))
	}
	if got.Get("refresh_token") != "refresh-1" {
		t.Errorf("refresh_token = %q", got.Get("refresh_token"))
	}
	if tok.AccessToken != "access-2" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
}

func TestRefreshKeepsTheOldRefreshTokenWhenNoneIsReturned(t *testing.T) {
	// Spotify does not always rotate the refresh token.
	_, client := fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "access-2",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		})
	})

	tok, err := client.refresh(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if tok.RefreshToken != "refresh-1" {
		t.Errorf("RefreshToken = %q, want refresh-1", tok.RefreshToken)
	}
}

func TestRefreshSurfacesARevokedGrant(t *testing.T) {
	_, client := fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "Refresh token revoked",
		})
	})

	_, err := client.refresh(context.Background(), "revoked-token")

	if err == nil {
		t.Fatal("refresh accepted a revoked grant")
	}
	if !IsReauthRequired(err) {
		t.Errorf("IsReauthRequired(%v) = false, want true", err)
	}
}

func TestExchangeFailureIsNotMistakenForReauth(t *testing.T) {
	// A stale code is not a revoked refresh token.
	_, client := fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	})

	_, err := client.exchange(context.Background(), "stale-code", "verifier", "redirect")

	if IsReauthRequired(err) {
		t.Error("IsReauthRequired = true for an exchange failure")
	}
}
