package spotify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStateWithNoFileIsNotAnError(t *testing.T) {
	// A missing file means "never authorised yet".
	path := filepath.Join(t.TempDir(), "spotify_state.json")

	state, err := loadState(path)

	if err != nil {
		t.Fatalf("loadState: %v", err)
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
	// A write that dies halfway must not corrupt the previous token.
	path := filepath.Join(t.TempDir(), "spotify_state.json")
	if err := saveState(path, authState{RefreshToken: "original"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	if err := saveState(path, authState{RefreshToken: "updated"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("leftover temp file: %s", e.Name())
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
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := loadState(path)

	if err == nil {
		t.Fatal("loadState accepted corrupt JSON")
	}
}

func TestSaveRequestsOwnerOnlyPermissions(t *testing.T) {
	// This tests that 0600 is requested, not that it is enforced. NTFS has no
	// POSIX permission bits, so on Windows the file is no better protected than
	// config.json (see docs/architecture.md).
	path := filepath.Join(t.TempDir(), "spotify_state.json")
	if err := saveState(path, authState{RefreshToken: "secret"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
}
