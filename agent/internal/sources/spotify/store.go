package spotify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// authState is the one thing the agent persists: a refresh token. It lives
// apart from config.json, which is hand-edited. Nobody should edit this file.
type authState struct {
	RefreshToken string `json:"refresh_token"`
}

// loadState reads the saved state. A missing file means nobody has authorised
// yet and is not an error.
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
		// Surface corruption. Discarding a real token would force a needless
		// reauthorisation.
		return authState{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return state, nil
}

// saveState writes the state atomically, mode 0600 because it holds a
// credential. See TestSaveRequestsOwnerOnlyPermissions for the Windows caveat.
func saveState(path string, state authState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o600)
}
