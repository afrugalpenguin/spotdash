package telemetry

import "context"

// newGPUReader returns the platform GPU reader.
func newGPUReader() gpuReader { return newPlatformGPUReader() }

// unsupportedGPU stands in where there is no NVML binding. It takes the same
// degraded path as a machine with no NVIDIA card.
type unsupportedGPU struct{ reason error }

func (u unsupportedGPU) Read(context.Context) (*GPU, error) { return nil, u.reason }
func (unsupportedGPU) Close() error                         { return nil }
