package sources

import "github.com/afrugalpenguin/spotdash/agent/internal/sources/partial"

// Partial wraps a reason so a source can return a value alongside it. See the
// partial package for why this distinction exists.
func Partial(cause error) error { return partial.New(cause) }

// IsPartial reports whether err marks a partial result.
func IsPartial(err error) bool { return partial.Is(err) }
