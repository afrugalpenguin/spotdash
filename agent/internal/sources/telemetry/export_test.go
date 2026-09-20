package telemetry

import (
	"time"
)

// NewWithUnavailableGPU builds a source that reads the real machine but cannot
// reach the GPU. It lets the degraded path run through the whole agent without
// renaming a system DLL.
func NewWithUnavailableGPU(interval time.Duration, reason error) *Source {
	return newWithReaders(interval, newSystemReader(), unsupportedGPU{reason: reason})
}
