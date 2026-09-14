package spotify

import (
	"regexp"
	"testing"
)

func TestVerifierIsURLSafeAndLongEnough(t *testing.T) {
	// RFC 7636: 43 to 128 characters from [A-Z a-z 0-9 - . _ ~].
	verifier, err := newCodeVerifier()
	if err != nil {
		t.Fatalf("newCodeVerifier returned an error: %v", err)
	}

	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("verifier length = %d, want 43 to 128", len(verifier))
	}
	if !regexp.MustCompile(`^[A-Za-z0-9\-._~]+$`).MatchString(verifier) {
		t.Errorf("verifier %q contains characters outside the unreserved set", verifier)
	}
}

func TestVerifiersAreNotReused(t *testing.T) {
	first, err := newCodeVerifier()
	if err != nil {
		t.Fatalf("newCodeVerifier: %v", err)
	}
	second, err := newCodeVerifier()
	if err != nil {
		t.Fatalf("newCodeVerifier: %v", err)
	}

	if first == second {
		t.Error("two calls produced the same verifier, which defeats the point of PKCE")
	}
}

func TestChallengeIsTheSHA256OfTheVerifier(t *testing.T) {
	// A fixed test vector from RFC 7636 appendix B, so this checks the actual
	// transform rather than just that something deterministic came out.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	if got := challengeFor(verifier); got != want {
		t.Errorf("challengeFor(%q) = %q, want %q", verifier, got, want)
	}
}

func TestStateIsUnpredictableAndURLSafe(t *testing.T) {
	first, err := newState()
	if err != nil {
		t.Fatalf("newState: %v", err)
	}
	second, err := newState()
	if err != nil {
		t.Fatalf("newState: %v", err)
	}

	if first == second {
		t.Error("two calls produced the same state value")
	}
	if len(first) < 16 {
		t.Errorf("state %q is too short to resist guessing", first)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9\-_]+$`).MatchString(first) {
		t.Errorf("state %q is not URL safe", first)
	}
}
