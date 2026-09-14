// Package telemetry reports machine load: CPU, memory, disks and, where one is
// available, the GPU.
//
// The GPU is optional by design. A machine with no NVIDIA card, or one whose
// driver is mid-update, still has a CPU and a disk worth watching, so a missing
// GPU degrades this source rather than failing it.
package telemetry

import (
	"context"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources/partial"
)

// Name is the key this source is configured under.
const Name = "telemetry"

// CPU load, as a percentage.
type CPU struct {
	Percent float64   `json:"percent"`
	PerCore []float64 `json:"per_core"`
}

// Memory in use.
type Memory struct {
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percent    float64 `json:"percent"`
}

// Disk is one fixed drive.
type Disk struct {
	Mount      string  `json:"mount"`
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percent    float64 `json:"percent"`
}

// GPU is the graphics card, when one can be read.
type GPU struct {
	Name           string  `json:"name"`
	Percent        float64 `json:"percent"`
	VRAMUsedBytes  uint64  `json:"vram_used_bytes"`
	VRAMTotalBytes uint64  `json:"vram_total_bytes"`
	VRAMPercent    float64 `json:"vram_percent"`
	TemperatureC   float64 `json:"temperature_c"`
	PowerWatts     float64 `json:"power_watts"`
}

// Reading is what the telemetry source publishes. GPU is a pointer so that an
// unavailable card serialises as null: an empty object would have the panel
// render real-looking zeroes for every gauge.
type Reading struct {
	CPU   CPU    `json:"cpu"`
	RAM   Memory `json:"ram"`
	Disks []Disk `json:"disks"`
	GPU   *GPU   `json:"gpu"`
}

// systemReader reads the parts that are always present.
type systemReader interface {
	Read(ctx context.Context) (CPU, Memory, []Disk, error)
}

// gpuReader reads the part that may not be.
type gpuReader interface {
	Read(ctx context.Context) (*GPU, error)
	Close() error
}

// Source reports machine load.
type Source struct {
	interval time.Duration
	system   systemReader
	gpu      gpuReader
}

// New builds the telemetry source.
//
// It never fails on account of the GPU. A machine with no NVIDIA card must run
// the agent, not refuse to start.
func New(cfg config.Source) (*Source, error) {
	return newWithReaders(cfg.Interval(), newSystemReader(), newGPUReader()), nil
}

func newWithReaders(interval time.Duration, system systemReader, gpu gpuReader) *Source {
	return &Source{interval: interval, system: system, gpu: gpu}
}

// Name identifies the source.
func (s *Source) Name() string { return Name }

// Interval is the configured poll period.
func (s *Source) Interval() time.Duration { return s.interval }

// Close releases the GPU handle.
func (s *Source) Close() error {
	if s.gpu == nil {
		return nil
	}
	return s.gpu.Close()
}

// Poll reads the machine.
//
// A GPU that cannot be read returns the rest of the reading with a partial
// error, so the runner stores it and marks the source degraded. A failure to
// read CPU, memory or disks leaves nothing worth publishing, so that is an
// ordinary error.
func (s *Source) Poll(ctx context.Context) (any, error) {
	cpu, memory, disks, err := s.system.Read(ctx)
	if err != nil {
		return nil, err
	}

	reading := Reading{CPU: cpu, RAM: memory, Disks: disks}

	gpu, gpuErr := s.gpu.Read(ctx)
	if gpuErr != nil {
		return reading, partial.New(gpuErr)
	}
	reading.GPU = gpu
	return reading, nil
}

// percentOf is used by every reader here. A zero total must produce zero rather
// than NaN, which would serialise as invalid JSON and break the whole message
// for every source, not just this one.
func percentOf(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}
