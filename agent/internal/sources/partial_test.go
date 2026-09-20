package sources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

func TestPartialResultIsStoredAndMarkedDegraded(t *testing.T) {
	store := state.New()
	store.Register("telemetry", state.StatusDegraded, "awaiting first poll")
	src := &fakeSource{
		name:     "telemetry",
		interval: 10 * time.Millisecond,
		poll: func(context.Context, int) (any, error) {
			return "cpu and ram, no gpu", Partial(errors.New("nvml: could not load nvml.dll"))
		},
	}

	startRunner(t, store, src)

	eventually(t, "the partial reading to be stored", func() bool {
		entry, ok := store.Get("telemetry")
		return ok && entry.Data == "cpu and ram, no gpu"
	})

	entry, _ := store.Get("telemetry")
	if entry.Status != state.StatusDegraded {
		t.Errorf("Status = %q, want degraded", entry.Status)
	}
	if entry.LastError == "" {
		t.Error("LastError is empty")
	}
}

func TestPartialIsNotAFailureForBackoff(t *testing.T) {
	// Backing off would slow the CPU gauge on a machine with no GPU.
	store := state.New()
	store.Register("telemetry", state.StatusDegraded, "")
	src := &fakeSource{
		name:     "telemetry",
		interval: 5 * time.Millisecond,
		poll: func(context.Context, int) (any, error) {
			return "a reading", Partial(errors.New("nvml unavailable"))
		},
	}

	startRunner(t, store, src)

	eventually(t, "the first few polls", func() bool { return src.callCount() >= 3 })
	after := src.callCount()
	time.Sleep(100 * time.Millisecond)

	// A 5ms interval without backoff gives about 20 polls, with backoff a handful.
	if extra := src.callCount() - after; extra < 8 {
		t.Errorf("%d polls in 100ms, want at least 8 (backoff applied)", extra)
	}
}

func TestPartialWithNoValueIsTreatedAsAFailure(t *testing.T) {
	// With no value there is nothing to publish.
	store := state.New()
	store.Register("telemetry", state.StatusDegraded, "")
	src := &fakeSource{
		name:     "telemetry",
		interval: 10 * time.Millisecond,
		poll: func(context.Context, int) (any, error) {
			return nil, Partial(errors.New("nvml unavailable"))
		},
	}

	startRunner(t, store, src)

	eventually(t, "the source to be marked degraded", func() bool {
		entry, ok := store.Get("telemetry")
		return ok && entry.Status == state.StatusDegraded && entry.LastError != ""
	})

	entry, _ := store.Get("telemetry")
	if entry.Data != nil {
		t.Errorf("Data = %v, want nil", entry.Data)
	}
}

func TestPartialErrorCarriesItsCause(t *testing.T) {
	cause := errors.New("nvml: could not load nvml.dll")

	err := Partial(cause)

	if !errors.Is(err, cause) {
		t.Error("Partial does not wrap its cause")
	}
	if !IsPartial(err) {
		t.Error("IsPartial(partial) = false")
	}
	if IsPartial(cause) {
		t.Error("IsPartial(ordinary error) = true")
	}
	if IsPartial(nil) {
		t.Error("IsPartial(nil) = true")
	}
}
