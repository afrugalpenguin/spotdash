//go:build windows

package instance

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Acquire takes the single-instance lock for the config at key. ok is false
// when another process holds it. The lock also goes when the process exits.
func Acquire(key string) (release func(), ok bool, err error) {
	name, err := windows.UTF16PtrFromString(lockName(key))
	if err != nil {
		return nil, false, err
	}

	// CreateMutex returns a handle even when the mutex already exists.
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
