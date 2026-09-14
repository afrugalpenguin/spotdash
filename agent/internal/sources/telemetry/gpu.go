package telemetry

import "context"

// newGPUReader returns the platform GPU reader.
func newGPUReader() gpuReader { return newPlatformGPUReader() }

// unsupportedGPU stands in on platforms with no NVML binding. It degrades the
// source rather than failing it, which is the same path a machine with no
// NVIDIA card takes.
type unsupportedGPU struct{ reason error }

func (u unsupportedGPU) Read(context.Context) (*GPU, error) { return nil, u.reason }
func (unsupportedGPU) Close() error                         { return nil }
