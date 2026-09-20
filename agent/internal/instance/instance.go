// Package instance keeps two agents from running against the same config. A
// second agent would otherwise fail at its port bind without saying why.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// lockName is the lock name for a config file. It sits in the Local namespace
// so another user's agent does not block this one. Case is ignored, as it is
// in Windows paths.
func lockName(configPath string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(configPath)))
	return `Local\spotdash-` + hex.EncodeToString(sum[:8])
}
