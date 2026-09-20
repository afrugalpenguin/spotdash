package spotify

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// newCodeVerifier returns a fresh RFC 7636 code verifier. 32 random bytes,
// base64url without padding, give 43 characters, the shortest allowed length.
func newCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// challengeFor derives the S256 code challenge from a verifier.
func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// newState returns an unguessable value that binds an authorization attempt to
// its callback and resists CSRF.
func newState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
