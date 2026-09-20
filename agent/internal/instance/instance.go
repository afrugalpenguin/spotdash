// Package instance keeps two agents from running against the same config.
//
// Started at login and again by hand, a second agent would only find out when
// its port bind failed, with an error that says nothing about why. Taking a
// lock first turns that into one clear line and a clean exit.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// lockName is the name of the lock for a config file. In the Local namespace,
// so another user's agent on the same machine does not block this one, and
// derived from the config path, so a development copy with its own config
// still runs. Case is ignored because Windows paths are.
func lockName(configPath string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(configPath)))
	return `Local\spotdash-` + hex.EncodeToString(sum[:8])
}
