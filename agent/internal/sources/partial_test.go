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
		t.Error("the reason for degrading should be recorded")
	}
}

func TestPartialIsNotAFailureForBackoff(t *testing.T) {
	// A source reporting partial results is working. Backing off would make a
	// machine with no GPU update its CPU gauge once every thirty seconds, and
	// retrying sooner would not bring the GPU back anyway.
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

	// At a 5ms interval with no backoff this is roughly 20 more polls. With
	// backoff wrongly applied it would be a handful.
	if extra := src.callCount() - after; extra < 8 {
		t.Errorf("only %d polls in 100ms, backoff is being applied to a partial result", extra)
	}
}

func TestPartialWithNoValueIsTreatedAsAFailure(t *testing.T) {
	// Nothing to publish means nothing to publish, whatever the error says.
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
		t.Errorf("Data = %v, want nothing stored when there is no value", entry.Data)
	}
}

func TestPartialErrorCarriesItsCause(t *testing.T) {
	cause := errors.New("nvml: could not load nvml.dll")

	err := Partial(cause)

	if !errors.Is(err, cause) {
		t.Error("a partial error should wrap its cause so callers can inspect it")
	}
	if !IsPartial(err) {
		t.Error("IsPartial should recognise a partial error")
	}
	if IsPartial(cause) {
		t.Error("IsPartial should not report an ordinary error as partial")
	}
	if IsPartial(nil) {
		t.Error("IsPartial(nil) should be false")
	}
}
