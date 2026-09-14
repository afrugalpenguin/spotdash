package spotify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStateWithNoFileIsNotAnError(t *testing.T) {
	// Absence means "never authorised yet", which is an ordinary starting
	// state, not a failure to report.
	path := filepath.Join(t.TempDir(), "spotify_state.json")

	state, err := loadState(path)

	if err != nil {
		t.Fatalf("loadState returned an error for a missing file: %v", err)
	}
	if state.RefreshToken != "" {
		t.Errorf("RefreshToken = %q, want empty", state.RefreshToken)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotify_state.json")

	if err := saveState(path, authState{RefreshToken: "refresh-abc"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	got, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if got.RefreshToken != "refresh-abc" {
		t.Errorf("RefreshToken = %q, want refresh-abc", got.RefreshToken)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	// The refresh token is the one thing the agent cannot recover on its own:
	// losing it means the user has to authorise again. A write that dies
	// halfway must not corrupt whatever was there before.
	path := filepath.Join(t.TempDir(), "spotify_state.json")
	if err := saveState(path, authState{RefreshToken: "original"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	if err := saveState(path, authState{RefreshToken: "updated"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}

	got, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if got.RefreshToken != "updated" {
		t.Errorf("RefreshToken = %q, want updated", got.RefreshToken)
	}
}

func TestLoadRejectsCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotify_state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing corrupt state: %v", err)
	}

	_, err := loadState(path)

	if err == nil {
		t.Fatal("loadState should refuse corrupt JSON rather than silently discard the token")
	}
}

func TestSaveRequestsOwnerOnlyPermissions(t *testing.T) {
	// The refresh token is a long-lived, read-scoped credential for the user's
	// Spotify account, so the file is written 0600.
	//
	// This only tests that the mode is requested, not that it is enforced.
	// Windows/NTFS does not implement POSIX permission bits: os.Chmod there
	// toggles only the read-only DOS attribute, not per-user access, so this
	// file has no stronger protection on Windows than config.json does, which
	// already accepts plain HTTP on a trusted LAN as documented in
	// docs/architecture.md. The chmod call still matters on a platform that
	// honours it.
	path := filepath.Join(t.TempDir(), "spotify_state.json")
	if err := saveState(path, authState{RefreshToken: "secret"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
}
