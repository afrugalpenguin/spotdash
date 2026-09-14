package state

import (
	"errors"
	"testing"
	"time"
)

func TestUpdatePartialStoresTheValueAndDegrades(t *testing.T) {
	// A source can produce a complete, useful value and still be running with
	// reduced capability. Telemetry without a GPU is the case that forced this:
	// CPU, RAM and disk are all genuinely there and worth publishing, and the
	// source is genuinely degraded.
	s := New()
	s.Register("telemetry", StatusDegraded, "awaiting first poll")

	before := time.Now()
	s.UpdatePartial("telemetry", "cpu and ram only", errors.New("nvml unavailable"))

	entry, ok := s.Get("telemetry")
	if !ok {
		t.Fatal("telemetry should be present")
	}
	if entry.Data != "cpu and ram only" {
		t.Errorf("Data = %v, want the partial value stored", entry.Data)
	}
	if entry.Status != StatusDegraded {
		t.Errorf("Status = %q, want degraded", entry.Status)
	}
	if entry.LastError != "nvml unavailable" {
		t.Errorf("LastError = %q, want the reason recorded", entry.LastError)
	}
	if entry.UpdatedAt.Before(before) {
		t.Error("UpdatedAt should advance: a partial poll is still a fresh reading")
	}
}

func TestUpdatePartialBroadcasts(t *testing.T) {
	// The value changed, so a connected panel has to hear about it. A degraded
	// telemetry source still updates every poll.
	s := New()
	s.Register("telemetry", StatusDegraded, "")
	events, cancel := s.Subscribe()
	defer cancel()

	s.UpdatePartial("telemetry", "a reading", errors.New("nvml unavailable"))

	entry := receive(t, events, "the partial update")
	if entry.Data != "a reading" {
		t.Errorf("broadcast Data = %v, want the partial value", entry.Data)
	}
	if entry.Status != StatusDegraded {
		t.Errorf("broadcast Status = %q, want degraded", entry.Status)
	}
}

func TestUpdatePartialThenFullRecovers(t *testing.T) {
	s := New()
	s.Register("telemetry", StatusDegraded, "")
	s.UpdatePartial("telemetry", "partial", errors.New("nvml unavailable"))

	s.Update("telemetry", "complete")

	entry, _ := s.Get("telemetry")
	if entry.Status != StatusOK {
		t.Errorf("Status = %q, want ok once the source recovers", entry.Status)
	}
	if entry.LastError != "" {
		t.Errorf("LastError = %q, want it cleared on recovery", entry.LastError)
	}
}

func TestUpdatePartialIgnoresUnregisteredSource(t *testing.T) {
	s := New()

	s.UpdatePartial("ghost", "data", errors.New("some reason"))

	if len(s.Snapshot()) != 0 {
		t.Error("UpdatePartial should not create a source that was never registered")
	}
}
