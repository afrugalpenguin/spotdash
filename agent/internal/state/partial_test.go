package state

import (
	"errors"
	"testing"
	"time"
)

func TestUpdatePartialStoresTheValueAndDegrades(t *testing.T) {
	// Telemetry without a GPU has a useful value and is still degraded.
	s := New()
	s.Register("telemetry", StatusDegraded, "awaiting first poll")

	before := time.Now()
	s.UpdatePartial("telemetry", "cpu and ram only", errors.New("nvml unavailable"))

	entry, ok := s.Get("telemetry")
	if !ok {
		t.Fatal("telemetry missing")
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
		t.Error("UpdatedAt is zero, want a time")
	}
}

func TestUpdatePartialBroadcasts(t *testing.T) {
	// A connected panel has to hear about a degraded source's new value.
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
		t.Errorf("Status = %q, want ok after recovery", entry.Status)
	}
	if entry.LastError != "" {
		t.Errorf("LastError = %q, want empty after recovery", entry.LastError)
	}
}

func TestUpdatePartialIgnoresUnregisteredSource(t *testing.T) {
	s := New()

	s.UpdatePartial("ghost", "data", errors.New("some reason"))

	if len(s.Snapshot()) != 0 {
		t.Error("UpdatePartial created an unregistered source")
	}
}
