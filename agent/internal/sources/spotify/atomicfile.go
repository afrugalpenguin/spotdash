package spotify

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path via a temp file in the same directory,
// renamed into place. Used for anything a poll writes that a reader (the auth
// state, the cached art) must never see half-written: a process that dies
// mid-write must leave either the old contents or the new ones, never a
// corrupt mix.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".spotify_*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("saving %s: %w", path, err)
	}
	cleanup = false
	return nil
}
