//go:build !windows

package telemetry

import "errors"

// The NVML binding is written against nvml.dll, so elsewhere the GPU is
// unavailable and telemetry runs degraded.
func newPlatformGPUReader() gpuReader {
	return unsupportedGPU{reason: errors.New("gpu telemetry is only implemented on windows")}
}
