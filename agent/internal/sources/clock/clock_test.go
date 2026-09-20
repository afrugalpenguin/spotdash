package clock

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

func sourceFor(t *testing.T, settings string) *Source {
	t.Helper()
	cfg := config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(settings)}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return src
}

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		t.Fatalf("Parse(%q): %v", value, err)
	}
	return parsed
}

func pollAt(t *testing.T, src *Source, when string) Reading {
	t.Helper()
	src.now = func() time.Time { return at(t, when) }
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading, ok := value.(Reading)
	if !ok {
		t.Fatalf("Poll returned %T, want Reading", value)
	}
	return reading
}

func TestNameIsClock(t *testing.T) {
	src := sourceFor(t, `{"enabled":true,"interval_ms":1000}`)

	if src.Name() != "clock" {
		t.Errorf("Name() = %q, want clock", src.Name())
	}
}

func TestIntervalComesFromConfig(t *testing.T) {
	src := sourceFor(t, `{"enabled":true,"interval_ms":1000}`)

	if got, want := src.Interval(), time.Second; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestPollReportsTimeAndDate(t *testing.T) {
	src := sourceFor(t, `{"enabled":true,"interval_ms":1000}`)

	reading := pollAt(t, src, "2026-09-14 14:05:09")

	if reading.Time != "14:05" {
		t.Errorf("Time = %q, want 14:05", reading.Time)
	}
	if reading.Seconds != 9 {
		t.Errorf("Seconds = %d, want 9", reading.Seconds)
	}
	if !strings.Contains(reading.Date, "14") || !strings.Contains(reading.Date, "Sep") {
		t.Errorf("Date = %q, want day 14 and month Sep", reading.Date)
	}
	if reading.ISO == "" {
		t.Error("ISO is empty")
	}
}

func TestSleepIsFalseWhenNoWindowConfigured(t *testing.T) {
	src := sourceFor(t, `{"enabled":true,"interval_ms":1000}`)

	for _, when := range []string{"2026-09-14 03:00:00", "2026-09-14 14:00:00", "2026-09-14 23:59:00"} {
		if pollAt(t, src, when).Sleep {
			t.Errorf("Sleep at %s = true, want false", when)
		}
	}
}

func TestSleepWindowCrossingMidnight(t *testing.T) {
	src := sourceFor(t, `{"enabled":true,"interval_ms":1000,"sleep_start":"23:30","sleep_end":"07:00"}`)

	tests := []struct {
		when string
		want bool
	}{
		{"2026-09-14 23:29:59", false},
		{"2026-09-14 23:30:00", true},
		{"2026-09-14 23:45:00", true},
		{"2026-09-15 00:00:00", true},
		{"2026-09-15 03:00:00", true},
		{"2026-09-15 06:59:59", true},
		{"2026-09-15 07:00:00", false},
		{"2026-09-15 12:00:00", false},
	}

	for _, tt := range tests {
		if got := pollAt(t, src, tt.when).Sleep; got != tt.want {
			t.Errorf("at %s Sleep = %v, want %v", tt.when, got, tt.want)
		}
	}
}

func TestSleepWindowWithinOneDay(t *testing.T) {
	src := sourceFor(t, `{"enabled":true,"interval_ms":1000,"sleep_start":"01:00","sleep_end":"05:00"}`)

	tests := []struct {
		when string
		want bool
	}{
		{"2026-09-14 00:59:59", false},
		{"2026-09-14 01:00:00", true},
		{"2026-09-14 04:59:59", true},
		{"2026-09-14 05:00:00", false},
		{"2026-09-14 22:00:00", false},
	}

	for _, tt := range tests {
		if got := pollAt(t, src, tt.when).Sleep; got != tt.want {
			t.Errorf("at %s Sleep = %v, want %v", tt.when, got, tt.want)
		}
	}
}

func TestSleepWindowWithEqualStartAndEndNeverSleeps(t *testing.T) {
	// An empty window must not blank the panel permanently.
	src := sourceFor(t, `{"enabled":true,"interval_ms":1000,"sleep_start":"07:00","sleep_end":"07:00"}`)

	for _, when := range []string{"2026-09-14 07:00:00", "2026-09-14 12:00:00", "2026-09-14 23:00:00"} {
		if pollAt(t, src, when).Sleep {
			t.Errorf("Sleep at %s = true, want false", when)
		}
	}
}

func TestNewRejectsBadWindows(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		wantIn   string
	}{
		{"hour out of range", `{"sleep_start":"25:00","sleep_end":"07:00"}`, "sleep_start"},
		{"minute out of range", `{"sleep_start":"23:70","sleep_end":"07:00"}`, "sleep_start"},
		{"not a time", `{"sleep_start":"bedtime","sleep_end":"07:00"}`, "sleep_start"},
		{"missing colon", `{"sleep_start":"2330","sleep_end":"07:00"}`, "sleep_start"},
		{"bad end", `{"sleep_start":"23:30","sleep_end":"7"}`, "sleep_end"},
		{"start without end", `{"sleep_start":"23:30"}`, "sleep_end"},
		{"end without start", `{"sleep_end":"07:00"}`, "sleep_start"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(tt.settings)})
			if err == nil {
				t.Fatal("New accepted the window")
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("error lacks %q: %v", tt.wantIn, err)
			}
		})
	}
}

func TestNewAcceptsSingleDigitHours(t *testing.T) {
	// 7:00 is what a person writes.
	if _, err := New(config.Source{Enabled: true, IntervalMS: 1000, Settings: []byte(`{"sleep_start":"23:30","sleep_end":"7:00"}`)}); err != nil {
		t.Errorf("New: %v", err)
	}
}
