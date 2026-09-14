package spotify

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// newCodeVerifier returns a fresh PKCE code verifier per RFC 7636: 43 to 128
// characters from the unreserved URL character set.
//
// 32 random bytes, base64url-encoded without padding, produces exactly 43
// characters, at the shortest end of the allowed range and comfortably above
// the entropy RFC 7636 asks for.
func newCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// challengeFor derives the PKCE code challenge from a verifier: the S256
// method, base64url(sha256(verifier)) with no padding.
func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// newState returns a fresh, unguessable value to bind one authorization
// attempt to its callback and resist CSRF against the callback endpoint.
func newState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
