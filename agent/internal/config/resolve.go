package config

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
)

const (
	// FileName is the config file's name wherever it lives.
	FileName = "config.json"
	// UserDirName is the folder under the user's config directory
	// (%APPDATA% on Windows) that a first run creates.
	UserDirName = "spotdash"
)

// tokenAlphabet is letters and digits only, because a token gets typed on a
// device keyboard and pushed through adb, and punctuation does not survive
// either (docs/device.md).
const (
	tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	tokenLength   = 32
)

// Location is where the config lives, and whether it is there yet.
type Location struct {
	Path   string
	Exists bool
}

// Resolve decides which config file the agent uses.
//
// Order: an explicit flag; config.json beside the executable; config.json in
// the working directory; then the per-user file under userDir. The first two
// after the flag are what the agent has always searched, so an existing setup
// keeps working untouched. When none exist, the per-user path is returned with
// Exists false, which is where a first run creates one.
//
// A flag is never replaced by a search, and is never created: naming a file
// that is not there is a mistake to report, not a request for a new one.
func Resolve(flagValue, exeDir, workDir, userDir string) (Location, error) {
	if flagValue != "" {
		return Location{Path: flagValue, Exists: exists(flagValue)}, nil
	}

	for _, dir := range []string{exeDir, workDir} {
		if dir == "" {
			continue
		}
		if path := filepath.Join(dir, FileName); exists(path) {
			return Location{Path: path, Exists: true}, nil
		}
	}

	if userDir == "" {
		return Location{}, errors.New("no config.json found beside the executable or in the working directory, and this account has no per-user config folder to create one in: pass -config")
	}
	path := filepath.Join(userDir, UserDirName, FileName)
	return Location{Path: path, Exists: exists(path)}, nil
}

// exists is deliberately generous: anything other than a clear "does not
// exist" counts as present. A file that cannot be read is Load's error to
// report, and must never be mistaken for a missing one and replaced.
func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// GenerateToken returns a random token of tokenLength letters and digits,
// about 190 bits, drawn from the operating system's random source.
func GenerateToken() (string, error) {
	out := make([]byte, tokenLength)
	limit := big.NewInt(int64(len(tokenAlphabet)))
	for i := range out {
		// rand.Int rejects rather than reduces, so every character is uniform.
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generating a token: %w", err)
		}
		out[i] = tokenAlphabet[n.Int64()]
	}
	return string(out), nil
}

// CreateDefault writes a new config at path from example, with the example's
// placeholder token replaced by a generated one. It fails with an error
// wrapping os.ErrExist if the file is already there, and never changes one.
//
// The file is written whole under a temporary name and then linked into place.
// A hard link fails if the target exists, so two first runs racing each other
// cannot overwrite one another, and a crash halfway leaves no truncated
// config.json for the next start to trip over.
func CreateDefault(path string, example []byte) error {
	cfg := &Config{}
	dec := json.NewDecoder(bytes.NewReader(example))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return fmt.Errorf("reading the built-in example config: %w", err)
	}
	token, err := GenerateToken()
	if err != nil {
		return err
	}
	cfg.Token = token
	if err := cfg.applyDefaults(); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("the built-in example config is not valid: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	dir := filepath.Dir(path)
	// 0700: the config holds the token, and a per-user folder has no reason to
	// be readable by anyone else.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config_*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s: %w", path, os.ErrExist)
		}
		return fmt.Errorf("creating %s: %w", path, err)
	}
	return nil
}
