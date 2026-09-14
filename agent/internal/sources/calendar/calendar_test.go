package calendar

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

func mustSettings(t *testing.T, s settings) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshalling settings: %v", err)
	}
	return raw
}

func TestNewRejectsAMissingMode(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{})}
	if _, err := New(cfg); err == nil {
		t.Fatal("want an error when mode is missing, got none")
	}
}

func TestNewRejectsAnUnknownMode(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "outlook"})}
	if _, err := New(cfg); err == nil {
		t.Fatal("want an error for an unknown mode, got none")
	}
}

func TestMockModeRequiresATitle(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "mock", StartInMinutes: 12})}
	if _, err := New(cfg); err == nil {
		t.Fatal("want an error when title is missing, got none")
	}
}

func TestMockSourcePublishesTheConfiguredEvent(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:           "mock",
		Title:          "Standup",
		StartInMinutes: 12,
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading, ok := value.(Reading)
	if !ok {
		t.Fatalf("Poll returned %T, want Reading", value)
	}
	if reading.Title != "Standup" {
		t.Errorf("Title = %q, want %q", reading.Title, "Standup")
	}
	if reading.MinutesUntil != 12 {
		t.Errorf("MinutesUntil = %v, want 12", reading.MinutesUntil)
	}
	if reading.StartLabel == "" {
		t.Error("StartLabel is empty, want a formatted time")
	}
}

func TestMockSourceWithNoEventIsIdle(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "mock"})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading := value.(Reading)
	if reading.Title != "" {
		t.Errorf("Title = %q, want empty for an idle mock", reading.Title)
	}
}

// TestMockSourceCountsDown checks that minutes_until decreases as the clock
// advances, since the mock exists to let the countdown behaviour be judged on
// the real panel, not just a frozen number.
func TestMockSourceCountsDown(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:           "mock",
		Title:          "Standup",
		StartInMinutes: 10,
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mock := src.(*Source)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	mock.now = func() time.Time { return start }

	first, _ := mock.Poll(context.Background())
	mock.now = func() time.Time { return start.Add(4 * time.Minute) }
	second, _ := mock.Poll(context.Background())

	got := first.(Reading).MinutesUntil - second.(Reading).MinutesUntil
	if got != 4 {
		t.Errorf("MinutesUntil dropped by %v over 4 minutes, want 4", got)
	}
}
