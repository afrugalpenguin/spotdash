//go:build !windows

package telemetry

import "errors"

// The agent targets Windows. The NVML binding is written against nvml.dll, so
// everywhere else the GPU is simply unavailable and telemetry runs degraded.
func newPlatformGPUReader() gpuReader {
	return unsupportedGPU{reason: errors.New("gpu telemetry is only implemented on windows")}
}
