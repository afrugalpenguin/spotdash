package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
	"github.com/afrugalpenguin/spotdash/agent/internal/sources/partial"
)

type fakeSystem struct {
	cpu    CPU
	memory Memory
	disks  []Disk
	err    error
}

func (f fakeSystem) Read(context.Context) (CPU, Memory, []Disk, error) {
	return f.cpu, f.memory, f.disks, f.err
}

type fakeGPU struct {
	gpu *GPU
	err error
}

func (f fakeGPU) Read(context.Context) (*GPU, error) { return f.gpu, f.err }
func (fakeGPU) Close() error                         { return nil }

func workingSystem() fakeSystem {
	return fakeSystem{
		cpu:    CPU{Percent: 12.5, PerCore: []float64{10, 15}},
		memory: Memory{UsedBytes: 8 << 30, TotalBytes: 32 << 30, Percent: 25},
		disks: []Disk{
			{Mount: "C:", UsedBytes: 400 << 30, TotalBytes: 1000 << 30, Percent: 40},
		},
	}
}

func workingGPU() fakeGPU {
	return fakeGPU{gpu: &GPU{
		Name:           "NVIDIA GeForce RTX 4070 Ti SUPER",
		Percent:        42,
		VRAMUsedBytes:  4 << 30,
		VRAMTotalBytes: 16 << 30,
		VRAMPercent:    25,
		TemperatureC:   61,
		PowerWatts:     120.5,
	}}
}

func pollWith(t *testing.T, sys systemReader, gpu gpuReader) (Reading, error) {
	t.Helper()
	src := newWithReaders(time.Second, sys, gpu)
	value, err := src.Poll(context.Background())
	reading, ok := value.(Reading)
	if value != nil && !ok {
		t.Fatalf("Poll returned %T, want Reading", value)
	}
	return reading, err
}

func TestNameAndInterval(t *testing.T) {
	src := newWithReaders(2*time.Second, workingSystem(), workingGPU())

	if src.Name() != "telemetry" {
		t.Errorf("Name() = %q, want telemetry", src.Name())
	}
	if got, want := src.Interval(), 2*time.Second; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestPollReportsEverything(t *testing.T) {
	reading, err := pollWith(t, workingSystem(), workingGPU())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if reading.CPU.Percent != 12.5 {
		t.Errorf("CPU.Percent = %v, want 12.5", reading.CPU.Percent)
	}
	if len(reading.CPU.PerCore) != 2 {
		t.Errorf("PerCore = %v, want 2 cores", reading.CPU.PerCore)
	}
	if reading.RAM.TotalBytes != 32<<30 {
		t.Errorf("RAM.TotalBytes = %d", reading.RAM.TotalBytes)
	}
	if len(reading.Disks) != 1 || reading.Disks[0].Mount != "C:" {
		t.Errorf("Disks = %+v, want one entry for C:", reading.Disks)
	}
	if reading.GPU == nil {
		t.Fatal("GPU = nil, want a reading")
	}
	if reading.GPU.TemperatureC != 61 || reading.GPU.PowerWatts != 120.5 {
		t.Errorf("GPU = %+v, want temperature 61 and power 120.5", reading.GPU)
	}
}

func TestPollWithoutNVMLStillReportsTheRest(t *testing.T) {
	reading, err := pollWith(t, workingSystem(), fakeGPU{err: errors.New("could not load nvml.dll")})

	if err == nil {
		t.Fatal("Poll error = nil, want a partial error")
	}
	if reading.GPU != nil {
		t.Errorf("GPU = %+v, want nil", reading.GPU)
	}
	if reading.CPU.Percent != 12.5 {
		t.Error("CPU.Percent lost when the GPU is missing")
	}
	if reading.RAM.TotalBytes == 0 {
		t.Error("RAM lost when the GPU is missing")
	}
	if len(reading.Disks) != 1 {
		t.Error("disks lost when the GPU is missing")
	}
	if !strings.Contains(err.Error(), "nvml.dll") {
		t.Errorf("error = %v, want it to carry the NVML reason", err)
	}
}

func TestMissingNVMLIsPartialNotFatal(t *testing.T) {
	// The runner stores a partial result and drops an ordinary error's value.
	src := newWithReaders(time.Second, workingSystem(), fakeGPU{err: errors.New("could not load nvml.dll")})

	value, err := src.Poll(context.Background())

	if value == nil {
		t.Fatal("degraded poll returned no reading")
	}
	if !isPartial(err) {
		t.Errorf("error = %T %v, want a partial error", err, err)
	}
}

func TestSystemFailureIsAnOrdinaryError(t *testing.T) {
	// No CPU or RAM leaves nothing to publish.
	src := newWithReaders(time.Second, fakeSystem{err: errors.New("cannot read cpu")}, workingGPU())

	value, err := src.Poll(context.Background())

	if err == nil {
		t.Fatal("Poll error = nil, want the system reader failure")
	}
	if isPartial(err) {
		t.Error("system failure marked partial")
	}
	if value != nil {
		t.Errorf("value = %v, want nil", value)
	}
}

func TestGPUIsNullInJSONWhenUnavailable(t *testing.T) {
	// An empty object would render every gauge as a real zero.
	reading, _ := pollWith(t, workingSystem(), fakeGPU{err: errors.New("no nvml")})

	encoded, err := json.Marshal(reading)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"gpu":null`) {
		t.Errorf("encoded = %s, want gpu null", encoded)
	}
}

func TestReadingEncodesTheFieldsThePanelNeeds(t *testing.T) {
	reading, _ := pollWith(t, workingSystem(), workingGPU())

	encoded, err := json.Marshal(reading)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{
		`"cpu"`, `"percent"`, `"per_core"`,
		`"ram"`, `"used_bytes"`, `"total_bytes"`,
		`"disks"`, `"mount"`,
		`"gpu"`, `"vram_used_bytes"`, `"temperature_c"`, `"power_watts"`,
	} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("encoded reading lacks %s:\n%s", key, encoded)
		}
	}
}

func TestNewReadsTheConfiguredInterval(t *testing.T) {
	src, err := New(config.Source{Enabled: true, IntervalMS: 3000})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer src.Close()

	if got, want := src.Interval(), 3*time.Second; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestNewSucceedsEvenWhenNVMLIsUnavailable(t *testing.T) {
	// A machine with no NVIDIA card must still start the agent.
	src, err := New(config.Source{Enabled: true, IntervalMS: 1000})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	src.Close()
}

func TestPercentOfHandlesAZeroTotal(t *testing.T) {
	// NaN would serialise as invalid JSON.
	if got := percentOf(0, 0); got != 0 {
		t.Errorf("percentOf(0, 0) = %v, want 0", got)
	}
	if got := percentOf(50, 200); got != 25 {
		t.Errorf("percentOf(50, 200) = %v, want 25", got)
	}
}

func isPartial(err error) bool { return partial.Is(err) }
