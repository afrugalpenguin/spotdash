package state

import (
	"errors"
	"testing"
	"time"
)

func TestRegisteredSourceAppearsInSnapshot(t *testing.T) {
	s := New()
	s.Register("clock", StatusDegraded, "awaiting first poll")

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d entries, want 1", len(snap))
	}
	if snap[0].Source != "clock" {
		t.Errorf("Source = %q, want %q", snap[0].Source, "clock")
	}
	if snap[0].Status != StatusDegraded {
		t.Errorf("Status = %q, want %q", snap[0].Status, StatusDegraded)
	}
	if snap[0].LastError != "awaiting first poll" {
		t.Errorf("LastError = %q, want %q", snap[0].LastError, "awaiting first poll")
	}
	if !snap[0].UpdatedAt.IsZero() {
		t.Error("UpdatedAt set before any poll, want zero")
	}
}

func TestUpdateMarksSourceOKAndClearsError(t *testing.T) {
	s := New()
	s.Register("clock", StatusDegraded, "awaiting first poll")

	before := time.Now()
	s.Update("clock", map[string]any{"time": "10:00"})

	entry, ok := s.Get("clock")
	if !ok {
		t.Fatal("clock missing after Update")
	}
	if entry.Status != StatusOK {
		t.Errorf("Status = %q, want %q", entry.Status, StatusOK)
	}
	if entry.LastError != "" {
		t.Errorf("LastError = %q, want it cleared", entry.LastError)
	}
	if entry.UpdatedAt.Before(before) {
		t.Error("UpdatedAt is zero after Update, want a time")
	}
	if entry.Data == nil {
		t.Error("Data is nil, want the polled value")
	}
}

func TestFailDegradesButKeepsLastGoodData(t *testing.T) {
	s := New()
	s.Register("clock", StatusDegraded, "awaiting first poll")
	s.Update("clock", "good value")

	s.Fail("clock", errors.New("clock exploded"))

	entry, _ := s.Get("clock")
	if entry.Status != StatusDegraded {
		t.Errorf("Status = %q, want %q", entry.Status, StatusDegraded)
	}
	if entry.LastError != "clock exploded" {
		t.Errorf("LastError = %q, want %q", entry.LastError, "clock exploded")
	}
	// A stale reading beats a blank panel while the status says it is stale.
	if entry.Data != "good value" {
		t.Errorf("Data = %v, want the last good value retained", entry.Data)
	}
}

func TestSnapshotIsSortedByName(t *testing.T) {
	s := New()
	s.Register("telemetry", StatusDisabled, "")
	s.Register("clock", StatusOK, "")
	s.Register("alarm", StatusOK, "")

	snap := s.Snapshot()
	want := []string{"alarm", "clock", "telemetry"}
	for i, name := range want {
		if snap[i].Source != name {
			t.Fatalf("Snapshot order = %v, want %v", names(snap), want)
		}
	}
}

func TestGetReportsMissingSource(t *testing.T) {
	s := New()

	if _, ok := s.Get("nope"); ok {
		t.Error("Get of an unregistered source = found, want missing")
	}
}

func TestUpdateIgnoresUnregisteredSource(t *testing.T) {
	s := New()

	// An unregistered write is a bug and must not invent a source.
	s.Update("ghost", "data")

	if len(s.Snapshot()) != 0 {
		t.Error("Update created an unregistered source")
	}
}

func names(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Source
	}
	return out
}

var errTest = errors.New("test failure")
