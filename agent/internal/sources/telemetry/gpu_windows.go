//go:build windows

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NVML is bound directly rather than through a library.
//
// The obvious choice, NVIDIA's own go-nvml, cannot build on Windows at all: it
// loads the library through dlfcn.h, which is POSIX. Binding nvml.dll here
// instead costs about a hundred lines and removes cgo from the project
// entirely, which means the agent stays a single static binary with no runtime
// dependency on a compiler's DLLs.
//
// NewLazySystemDLL resolves only from the system directory, so a stray nvml.dll
// next to the binary or in the working directory cannot be loaded instead.
var (
	nvmlDLL = windows.NewLazySystemDLL("nvml.dll")

	procInit         = nvmlDLL.NewProc("nvmlInit_v2")
	procShutdown     = nvmlDLL.NewProc("nvmlShutdown")
	procHandleByIdx  = nvmlDLL.NewProc("nvmlDeviceGetHandleByIndex_v2")
	procGetName      = nvmlDLL.NewProc("nvmlDeviceGetName")
	procGetUtil      = nvmlDLL.NewProc("nvmlDeviceGetUtilizationRates")
	procGetMemory    = nvmlDLL.NewProc("nvmlDeviceGetMemoryInfo")
	procGetTemp      = nvmlDLL.NewProc("nvmlDeviceGetTemperature")
	procGetPower     = nvmlDLL.NewProc("nvmlDeviceGetPowerUsage")
	temperatureGPU   = uintptr(0) // NVML_TEMPERATURE_GPU
	nameBufferLength = uintptr(96)
)

const nvmlSuccess = 0

// utilization mirrors nvmlUtilization_t.
type utilization struct {
	GPU    uint32
	Memory uint32
}

// memoryInfo mirrors nvmlMemory_t.
type memoryInfo struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// nvmlReader reads the first NVIDIA GPU.
//
// Initialisation is deferred to the first read and retried whenever it is not
// yet established. That covers the case that actually happens on a desktop: the
// agent starts while the driver is updating or the card is not ready, and it
// comes back a minute later. Initialising once at construction would leave the
// GPU missing until the agent was restarted.
type nvmlReader struct {
	mu      sync.Mutex
	started bool
}

func newPlatformGPUReader() gpuReader { return &nvmlReader{} }

func (r *nvmlReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return nil
	}
	r.started = false
	if ret, _, _ := procShutdown.Call(); ret != nvmlSuccess {
		return fmt.Errorf("nvml shutdown: %s", nvmlError(ret))
	}
	return nil
}

func (r *nvmlReader) Read(_ context.Context) (*GPU, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := nvmlDLL.Load(); err != nil {
		// No driver, or no NVIDIA card at all. This is the expected degraded
		// path, not an exceptional one.
		return nil, fmt.Errorf("nvml unavailable: %w", err)
	}

	if !r.started {
		if err := procInit.Find(); err != nil {
			return nil, fmt.Errorf("nvml unavailable: %w", err)
		}
		if ret, _, _ := procInit.Call(); ret != nvmlSuccess {
			return nil, fmt.Errorf("nvml init: %s", nvmlError(ret))
		}
		r.started = true
	}

	var device uintptr
	ret, _, _ := procHandleByIdx.Call(0, uintptr(unsafe.Pointer(&device)))
	if ret != nvmlSuccess {
		// Give up the handle state so the next poll initialises again. A driver
		// restart invalidates everything obtained before it.
		r.started = false
		_, _, _ = procShutdown.Call()
		return nil, fmt.Errorf("nvml device 0: %s", nvmlError(ret))
	}

	gpu := &GPU{}

	// Each field is read independently and one failure is not fatal. Not every
	// card reports power draw, and a driver can refuse a single metric while
	// serving the rest. Losing the whole GPU over a missing wattage would be
	// the wrong trade.
	name := make([]byte, nameBufferLength)
	if ret, _, _ := procGetName.Call(device, uintptr(unsafe.Pointer(&name[0])), nameBufferLength); ret == nvmlSuccess {
		gpu.Name = windows.ByteSliceToString(name)
	}

	var util utilization
	if ret, _, _ := procGetUtil.Call(device, uintptr(unsafe.Pointer(&util))); ret == nvmlSuccess {
		gpu.Percent = float64(util.GPU)
	}

	var vram memoryInfo
	if ret, _, _ := procGetMemory.Call(device, uintptr(unsafe.Pointer(&vram))); ret == nvmlSuccess {
		gpu.VRAMUsedBytes = vram.Used
		gpu.VRAMTotalBytes = vram.Total
		gpu.VRAMPercent = percentOf(vram.Used, vram.Total)
	}

	var celsius uint32
	if ret, _, _ := procGetTemp.Call(device, temperatureGPU, uintptr(unsafe.Pointer(&celsius))); ret == nvmlSuccess {
		gpu.TemperatureC = float64(celsius)
	}

	var milliwatts uint32
	if ret, _, _ := procGetPower.Call(device, uintptr(unsafe.Pointer(&milliwatts))); ret == nvmlSuccess {
		gpu.PowerWatts = float64(milliwatts) / 1000
	}

	return gpu, nil
}

// nvmlReturns maps the return codes worth naming.
//
// Reading nvmlErrorString would mean converting a pointer into memory the Go
// runtime does not own, which go vet rightly objects to. The codes are stable
// API, so naming them here is both safer and more predictable. The two that
// matter most for this source are a missing library and an unloaded driver:
// those are the expected degraded path on a machine with no NVIDIA card.
var nvmlReturns = map[uintptr]string{
	1:   "not initialised",
	2:   "invalid argument",
	3:   "not supported",
	4:   "no permission",
	6:   "not found",
	7:   "insufficient buffer size",
	9:   "driver not loaded",
	10:  "timeout",
	12:  "library not found",
	13:  "function not found",
	15:  "gpu is lost",
	16:  "gpu reset required",
	17:  "operating system rejected the call",
	18:  "driver and library version mismatch",
	20:  "memory error",
	21:  "no data",
	999: "unknown error",
}

func nvmlError(code uintptr) string {
	if message, known := nvmlReturns[code]; known {
		return message
	}
	return fmt.Sprintf("code %d", code)
}
