//go:build !windows

package instance

// Acquire takes the single-instance lock for the config at key. Only Windows
// starts the agent at login, so elsewhere there is nothing to guard and every
// caller is the only instance.
func Acquire(key string) (release func(), ok bool, err error) {
	return func() {}, true, nil
}
