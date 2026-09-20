package spotify

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data through a temp file in the same directory and
// renames it into place, so a crash leaves the old contents or the new, never a
// mix.
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
