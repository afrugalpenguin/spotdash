package telemetry

import (
	"context"
	"fmt"
	"sort"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// gopsutilReader reads CPU, memory and disks.
type gopsutilReader struct{}

func newSystemReader() systemReader { return gopsutilReader{} }

func (gopsutilReader) Read(ctx context.Context) (CPU, Memory, []Disk, error) {
	// A zero interval means "since the last call" and returns immediately. A
	// real interval would block the poll for its whole duration.
	totals, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return CPU{}, Memory{}, nil, fmt.Errorf("reading cpu: %w", err)
	}
	perCore, err := cpu.PercentWithContext(ctx, 0, true)
	if err != nil {
		return CPU{}, Memory{}, nil, fmt.Errorf("reading per core cpu: %w", err)
	}

	load := CPU{PerCore: perCore}
	if len(totals) > 0 {
		load.Percent = totals[0]
	}
	if load.PerCore == nil {
		load.PerCore = []float64{}
	}

	virtual, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return CPU{}, Memory{}, nil, fmt.Errorf("reading memory: %w", err)
	}
	memory := Memory{
		UsedBytes:  virtual.Used,
		TotalBytes: virtual.Total,
		Percent:    percentOf(virtual.Used, virtual.Total),
	}

	disks, err := readDisks(ctx)
	if err != nil {
		return CPU{}, Memory{}, nil, err
	}

	return load, memory, disks, nil
}

func readDisks(ctx context.Context) ([]Disk, error) {
	// false means physical devices only, which drops network and virtual mounts.
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("listing disks: %w", err)
	}

	out := make([]Disk, 0, len(partitions))
	for _, partition := range partitions {
		usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
		if err != nil {
			// An empty card reader or removable must not lose the other drives.
			continue
		}
		if usage.Total == 0 {
			continue
		}
		out = append(out, Disk{
			Mount:      partition.Mountpoint,
			UsedBytes:  usage.Used,
			TotalBytes: usage.Total,
			Percent:    percentOf(usage.Used, usage.Total),
		})
	}

	// Stable order, so rows do not jump between polls.
	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out, nil
}
