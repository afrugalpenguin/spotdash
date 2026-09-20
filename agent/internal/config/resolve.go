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
	// UserDirName is the folder under the user config directory (%APPDATA% on
	// Windows) that a first run creates.
	UserDirName = "spotdash"
)

// tokenAlphabet is letters and digits only. A token is typed on the device and
// pushed through adb, and punctuation survives neither (docs/device.md).
const (
	tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	tokenLength   = 32
)

// Location is where the config lives, and whether it is there yet.
type Location struct {
	Path   string
	Exists bool
}

// Resolve decides which config file the agent uses: the flag, then config.json
// beside the executable, in the working directory, then under userDir. With
// none present it returns the userDir path with Exists false. A flag path is
// never created.
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

// exists counts anything other than a clear "does not exist" as present. An
// unreadable file is Load's error and must never be taken for a missing one.
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
		// rand.Int rejects out-of-range draws, so every character is uniform.
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generating a token: %w", err)
		}
		out[i] = tokenAlphabet[n.Int64()]
	}
	return string(out), nil
}

// CreateDefault writes a new config at path from example, with a generated
// token. It fails with an error wrapping os.ErrExist if the file is already
// there. See docs/architecture.md, "Configuration".
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
	// 0700: the config holds the token.
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
