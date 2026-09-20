// Package partial marks a poll that produced a usable value despite reduced
// capability. It is separate so sources need not import the registry. See
// docs/architecture.md, "A third outcome: partial results".
package partial

import (
	"errors"
	"fmt"
)

type partialError struct {
	cause error
}

func (e *partialError) Error() string {
	return fmt.Sprintf("partial result: %v", e.cause)
}

func (e *partialError) Unwrap() error {
	return e.cause
}

// New wraps a reason so a source can return a value alongside it.
func New(cause error) error {
	if cause == nil {
		return nil
	}
	return &partialError{cause: cause}
}

// Is reports whether err marks a partial result.
func Is(err error) bool {
	var marked *partialError
	return errors.As(err, &marked)
}
