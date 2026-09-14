package telemetry

import (
	"time"
)

// NewWithUnavailableGPU builds a telemetry source that reads the real machine
// but cannot reach the GPU, which is what a host with no NVIDIA card or a
// driver mid-update looks like.
//
// This file is only compiled into tests. It exists so the degraded path can be
// exercised through the whole agent, rather than only against a fake system
// reader, without renaming a system DLL to do it.
func NewWithUnavailableGPU(interval time.Duration, reason error) *Source {
	return newWithReaders(interval, newSystemReader(), unsupportedGPU{reason: reason})
}
