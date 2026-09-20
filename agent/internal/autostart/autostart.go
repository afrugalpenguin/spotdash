// Package autostart turns "start with Windows" on and off for the current user.
//
// It is one value under HKCU\Software\Microsoft\Windows\CurrentVersion\Run: no
// admin rights, nothing machine wide, and removing the value removes it. The
// value lives behind a Store so the logic can be tested without the registry.
package autostart

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValueName is the name of the value under the Run key. Also what someone
// removes by hand: reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v spotdash /f
const ValueName = "spotdash"

// Store is the one registry value autostart is kept in.
type Store interface {
	// Get returns the value's data, and whether the value exists.
	Get() (data string, exists bool, err error)
	// Set writes the value, creating it if needed.
	Set(data string) error
	// Delete removes the value. Removing one that is not there is not an error.
	Delete() error
}

// Manager turns autostart on and off for one executable.
type Manager struct {
	store       Store
	command     string
	unavailable string
}

// New returns a Manager for the executable at exe. tempDir is where temporary
// files live (os.TempDir), which is where go run puts what it builds.
func New(store Store, exe, tempDir string) *Manager {
	m := &Manager{store: store, command: `"` + exe + `"`}
	if inside(tempDir, exe) {
		m.unavailable = "this copy is running from a temporary folder, which will not exist at next login"
	}
	return m
}

// Unavailable is the reason autostart cannot be turned on for this executable,
// or "" when it can.
func (m *Manager) Unavailable() string { return m.unavailable }

// Enabled reports whether autostart is on for this executable, read from the
// registry each time so it reflects what is really there. A value that points at
// some other path is not this executable being enabled: if the binary has moved
// the old value launches nothing, and enabling again is what puts it right.
func (m *Manager) Enabled() (bool, error) {
	data, exists, err := m.store.Get()
	if err != nil {
		return false, fmt.Errorf("reading the autostart setting: %w", err)
	}
	return exists && strings.EqualFold(data, m.command), nil
}

// Set turns autostart on or off.
func (m *Manager) Set(on bool) error {
	if !on {
		if err := m.store.Delete(); err != nil {
			return fmt.Errorf("removing the autostart setting: %w", err)
		}
		return nil
	}
	if m.unavailable != "" {
		return fmt.Errorf("cannot start with Windows: %s", m.unavailable)
	}
	if err := m.store.Set(m.command); err != nil {
		return fmt.Errorf("writing the autostart setting: %w", err)
	}
	return nil
}

// inside reports whether path is dir or somewhere below it. Compared as whole
// path elements, so a sibling directory that merely shares a prefix does not
// match, and filepath.Rel applies the platform's case rules.
func inside(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
