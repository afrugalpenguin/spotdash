//go:build windows

package instance

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Acquire takes the single-instance lock for the config at key. ok is false
// when another process already holds it. release must be called when the agent
// stops; the lock also goes when the process does, however it ends.
func Acquire(key string) (release func(), ok bool, err error) {
	name, err := windows.UTF16PtrFromString(lockName(key))
	if err != nil {
		return nil, false, err
	}

	// CreateMutex hands back a handle either way, and reports that the mutex
	// already existed through the error.
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, err
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(handle)
		return func() {}, false, nil
	}

	return func() { windows.CloseHandle(handle) }, true, nil
}
