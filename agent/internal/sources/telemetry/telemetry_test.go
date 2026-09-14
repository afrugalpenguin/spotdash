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
		t.Fatalf("Poll returned an error: %v", err)
	}

	if reading.CPU.Percent != 12.5 {
		t.Errorf("CPU.Percent = %v, want 12.5", reading.CPU.Percent)
	}
	if len(reading.CPU.PerCore) != 2 {
		t.Errorf("PerCore = %v, want two cores", reading.CPU.PerCore)
	}
	if reading.RAM.TotalBytes != 32<<30 {
		t.Errorf("RAM.TotalBytes = %d", reading.RAM.TotalBytes)
	}
	if len(reading.Disks) != 1 || reading.Disks[0].Mount != "C:" {
		t.Errorf("Disks = %+v, want one entry for C:", reading.Disks)
	}
	if reading.GPU == nil {
		t.Fatal("GPU should be present when NVML works")
	}
	if reading.GPU.TemperatureC != 61 || reading.GPU.PowerWatts != 120.5 {
		t.Errorf("GPU = %+v, want the temperature and power carried through", reading.GPU)
	}
}

func TestPollWithoutNVMLStillReportsTheRest(t *testing.T) {
	// The whole point of the degraded path. A machine with no usable NVML is
	// still worth a CPU, RAM and disk readout.
	reading, err := pollWith(t, workingSystem(), fakeGPU{err: errors.New("could not load nvml.dll")})

	if err == nil {
		t.Fatal("Poll should report that it is degraded")
	}
	if reading.GPU != nil {
		t.Errorf("GPU = %+v, want nil when NVML is unavailable", reading.GPU)
	}
	if reading.CPU.Percent != 12.5 {
		t.Error("CPU should still be reported when the GPU is missing")
	}
	if reading.RAM.TotalBytes == 0 {
		t.Error("RAM should still be reported when the GPU is missing")
	}
	if len(reading.Disks) != 1 {
		t.Error("disks should still be reported when the GPU is missing")
	}
	if !strings.Contains(err.Error(), "nvml.dll") {
		t.Errorf("the error should carry the NVML reason, got: %v", err)
	}
}

func TestMissingNVMLIsPartialNotFatal(t *testing.T) {
	// The distinction the runner acts on: a partial result is stored and
	// broadcast, an ordinary error is not.
	src := newWithReaders(time.Second, workingSystem(), fakeGPU{err: errors.New("could not load nvml.dll")})

	value, err := src.Poll(context.Background())

	if value == nil {
		t.Fatal("a degraded poll must still return its reading")
	}
	if !isPartial(err) {
		t.Errorf("error should be marked partial so the reading is kept, got: %T %v", err, err)
	}
}

func TestSystemFailureIsAnOrdinaryError(t *testing.T) {
	// No CPU or RAM means there is nothing worth publishing, so this is a
	// plain failure rather than a partial result.
	src := newWithReaders(time.Second, fakeSystem{err: errors.New("cannot read cpu")}, workingGPU())

	value, err := src.Poll(context.Background())

	if err == nil {
		t.Fatal("Poll should fail when the system reader fails")
	}
	if isPartial(err) {
		t.Error("a system read failure has nothing to publish, so it is not a partial result")
	}
	if value != nil {
		t.Errorf("value = %v, want nothing when there is no reading", value)
	}
}

func TestGPUIsNullInJSONWhenUnavailable(t *testing.T) {
	// The panel reads this. A missing GPU has to be null rather than an empty
	// object, or every gauge renders as a real zero.
	reading, _ := pollWith(t, workingSystem(), fakeGPU{err: errors.New("no nvml")})

	encoded, err := json.Marshal(reading)
	if err != nil {
		t.Fatalf("encoding the reading: %v", err)
	}
	if !strings.Contains(string(encoded), `"gpu":null`) {
		t.Errorf("encoded reading should carry a null gpu, got: %s", encoded)
	}
}

func TestReadingEncodesTheFieldsThePanelNeeds(t *testing.T) {
	reading, _ := pollWith(t, workingSystem(), workingGPU())

	encoded, err := json.Marshal(reading)
	if err != nil {
		t.Fatalf("encoding the reading: %v", err)
	}
	for _, key := range []string{
		`"cpu"`, `"percent"`, `"per_core"`,
		`"ram"`, `"used_bytes"`, `"total_bytes"`,
		`"disks"`, `"mount"`,
		`"gpu"`, `"vram_used_bytes"`, `"temperature_c"`, `"power_watts"`,
	} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("encoded reading is missing %s\n%s", key, encoded)
		}
	}
}

func TestNewReadsTheConfiguredInterval(t *testing.T) {
	src, err := New(config.Source{Enabled: true, IntervalMS: 3000})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	defer src.Close()

	if got, want := src.Interval(), 3*time.Second; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestNewSucceedsEvenWhenNVMLIsUnavailable(t *testing.T) {
	// Construction must not fail on a machine with no NVIDIA card, or the
	// agent refuses to start there instead of degrading.
	src, err := New(config.Source{Enabled: true, IntervalMS: 1000})
	if err != nil {
		t.Fatalf("New should never fail on account of the GPU, got: %v", err)
	}
	src.Close()
}

func TestPercentOfHandlesAZeroTotal(t *testing.T) {
	// A disk or a GPU reporting a zero total should read as zero, not NaN,
	// which would serialise as invalid JSON and break the whole message.
	if got := percentOf(0, 0); got != 0 {
		t.Errorf("percentOf(0, 0) = %v, want 0", got)
	}
	if got := percentOf(50, 200); got != 25 {
		t.Errorf("percentOf(50, 200) = %v, want 25", got)
	}
}

func isPartial(err error) bool { return partial.Is(err) }
