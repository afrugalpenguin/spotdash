//go:build !windows

package instance

// Acquire is a no-op off Windows, where nothing starts the agent at login.
func Acquire(key string) (release func(), ok bool, err error) {
	return func() {}, true, nil
}
