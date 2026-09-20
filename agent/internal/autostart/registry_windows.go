//go:build windows

package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// runKeyPath is the per-user list of programs started at login.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// SystemStore is the real value: spotdash under the current user's Run key.
func SystemStore() Store { return newRegistryStore(runKeyPath, ValueName) }

// registryStore is one string value under a key in HKEY_CURRENT_USER.
type registryStore struct {
	path string
	name string
}

func newRegistryStore(path, name string) Store {
	return registryStore{path: path, name: name}
}

func (s registryStore) Get() (string, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, s.path, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer key.Close()

	data, _, err := key.GetStringValue(s.name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return data, true, nil
}

func (s registryStore) Set(data string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, s.path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(s.name, data)
}

func (s registryStore) Delete() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, s.path, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()

	if err := key.DeleteValue(s.name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
