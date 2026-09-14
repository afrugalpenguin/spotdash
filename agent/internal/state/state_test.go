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
		t.Error("a source that has never polled should have a zero UpdatedAt")
	}
}

func TestUpdateMarksSourceOKAndClearsError(t *testing.T) {
	s := New()
	s.Register("clock", StatusDegraded, "awaiting first poll")

	before := time.Now()
	s.Update("clock", map[string]any{"time": "10:00"})

	entry, ok := s.Get("clock")
	if !ok {
		t.Fatal("clock should be present after Update")
	}
	if entry.Status != StatusOK {
		t.Errorf("Status = %q, want %q", entry.Status, StatusOK)
	}
	if entry.LastError != "" {
		t.Errorf("LastError = %q, want it cleared", entry.LastError)
	}
	if entry.UpdatedAt.Before(before) {
		t.Error("UpdatedAt should advance on a successful update")
	}
	if entry.Data == nil {
		t.Error("Data should hold the polled value")
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
	// A stale reading is more useful than a blank panel, so long as the status
	// says it is stale.
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
		t.Error("Get should report a source that was never registered as missing")
	}
}

func TestUpdateIgnoresUnregisteredSource(t *testing.T) {
	s := New()

	// Only the registry writes here, and it only writes sources it started.
	// An unregistered write means a bug, and must not invent a source that
	// /health would then report as healthy.
	s.Update("ghost", "data")

	if len(s.Snapshot()) != 0 {
		t.Error("Update should not create a source that was never registered")
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
