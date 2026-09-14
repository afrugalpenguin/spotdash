package spotify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// authState is the one thing the agent persists for itself: a refresh token,
// which is a long-lived, read-scoped credential for the user's Spotify
// account. Separate from config.json, which is hand-edited and read-only;
// this file is written by the agent and must never be edited by hand.
type authState struct {
	RefreshToken string `json:"refresh_token"`
}

// loadState reads the saved state. A missing file is not an error: it means
// nobody has authorised yet, which is an ordinary starting state.
func loadState(path string) (authState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return authState{}, nil
		}
		return authState{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var state authState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt rather than absent. Silently discarding a real token on a
		// parse error would force an unnecessary reauthorisation, so this is
		// surfaced rather than swallowed.
		return authState{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return state, nil
}

// saveState writes the state atomically. A refresh token is the one thing the
// agent cannot recover on its own, so a write that dies halfway must not
// corrupt whatever was there before.
//
// 0600: this holds a credential, and the desk this runs on is shared by
// definition. See the note on TestSaveRequestsOwnerOnlyPermissions for what
// this does and does not guarantee on Windows.
func saveState(path string, state authState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o600)
}
