// Package partial marks a poll that produced a usable value while running with
// reduced capability.
//
// The runner has two natural outcomes, a value or a failure, and some sources
// have a third. Telemetry with no usable NVML has a complete, useful reading
// for CPU, RAM and disk and is also genuinely degraded. Forcing that into
// either outcome loses something: a failure throws away a good reading, a
// success hides a real problem.
//
// This lives in its own package so a source can mark a result without importing
// the registry that runs it, which would be an import cycle.
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
