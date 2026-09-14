package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		Location:       "Microsoft Teams Meeting",
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
	if reading.Location != "Microsoft Teams Meeting" {
		t.Errorf("Location = %q, want %q", reading.Location, "Microsoft Teams Meeting")
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

// TestMockSourceWithStartAtUsesTheGivenClockTime checks that "start_at" pins
// the mock event to a specific time of day, e.g. for a screenshot or a demo,
// rather than the countdown drifting with whenever the agent happens to
// start.
func TestMockSourceWithStartAtUsesTheGivenClockTime(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:    "mock",
		Title:   "Standup with team 2",
		StartAt: "16:30",
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mock := src.(*Source)
	mock.now = func() time.Time {
		return time.Date(2026, 1, 1, 16, 12, 0, 0, time.Local)
	}

	value, err := mock.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading := value.(Reading)
	if reading.StartLabel != "16:30" {
		t.Errorf("StartLabel = %q, want %q", reading.StartLabel, "16:30")
	}
	if reading.MinutesUntil != 18 {
		t.Errorf("MinutesUntil = %v, want 18", reading.MinutesUntil)
	}
}

// TestMockSourceWithStartAtInThePastRollsToTomorrow checks that a start_at
// earlier than the current time is still a sensible future demo event rather
// than an already-passed, negative countdown.
func TestMockSourceWithStartAtInThePastRollsToTomorrow(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:    "mock",
		Title:   "Standup with team 2",
		StartAt: "09:00",
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mock := src.(*Source)
	mock.now = func() time.Time {
		return time.Date(2026, 1, 1, 16, 12, 0, 0, time.Local)
	}

	value, err := mock.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading := value.(Reading)
	if reading.MinutesUntil <= 0 {
		t.Errorf("MinutesUntil = %v, want positive (rolled to tomorrow)", reading.MinutesUntil)
	}
}

func TestMockModeRejectsAnUnparseableStartAt(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:    "mock",
		Title:   "Standup",
		StartAt: "not a time",
	})}
	if _, err := New(cfg); err == nil {
		t.Fatal("want an error for an unparseable start_at, got none")
	}
}

func TestMockModeRejectsBothStartInMinutesAndStartAt(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:           "mock",
		Title:          "Standup",
		StartInMinutes: 12,
		StartAt:        "16:30",
	})}
	if _, err := New(cfg); err == nil {
		t.Fatal("want an error when both start_in_minutes and start_at are set, got none")
	}
}

func TestReadingIsUrgentWithinNotifyMinutes(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:           "mock",
		Title:          "Standup",
		StartInMinutes: 10,
		NotifyMinutes:  15,
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, _ := src.Poll(context.Background())
	if !value.(Reading).Urgent {
		t.Error("want Urgent=true for an event 10 minutes out with a 15 minute threshold")
	}
}

func TestReadingIsNotUrgentBeyondNotifyMinutes(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:           "mock",
		Title:          "Standup",
		StartInMinutes: 30,
		NotifyMinutes:  15,
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, _ := src.Poll(context.Background())
	if value.(Reading).Urgent {
		t.Error("want Urgent=false for an event 30 minutes out with a 15 minute threshold")
	}
}

func TestDefaultsApplyWhenNotifyMinutesAndShowSecondsAreUnset(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:           "mock",
		Title:          "Standup",
		StartInMinutes: DefaultNotifyMinutes, // exactly at the default threshold
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, _ := src.Poll(context.Background())
	reading := value.(Reading)
	if !reading.Urgent {
		t.Error("want Urgent=true when start_in_minutes equals the default notify threshold")
	}
	if reading.ShowSeconds != DefaultShowSeconds {
		t.Errorf("ShowSeconds = %d, want the default %d", reading.ShowSeconds, DefaultShowSeconds)
	}
}

func TestICSModeRequiresAFeedURL(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "ics"})}
	if _, err := New(cfg); err == nil {
		t.Fatal("want an error when feed_url is missing, got none")
	}
}

func TestICSSourceFetchesAndPublishesTheNextEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BEGIN:VCALENDAR\r\n" +
			"BEGIN:VEVENT\r\n" +
			"SUMMARY:Standup with team 2\r\n" +
			"LOCATION:Microsoft Teams Meeting\r\n" +
			"DTSTART:20990101T090000Z\r\n" +
			"END:VEVENT\r\n" +
			"END:VCALENDAR\r\n"))
	}))
	defer server.Close()

	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "ics", FeedURL: server.URL})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading := value.(Reading)
	if reading.Title != "Standup with team 2" {
		t.Errorf("Title = %q, want %q", reading.Title, "Standup with team 2")
	}
	if reading.Location != "Microsoft Teams Meeting" {
		t.Errorf("Location = %q, want %q", reading.Location, "Microsoft Teams Meeting")
	}
}

func TestICSSourcePopulatesTheAgendaAfterThePrimaryEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BEGIN:VCALENDAR\r\n" +
			"BEGIN:VEVENT\r\n" +
			"SUMMARY:First\r\n" +
			"DTSTART:20990101T090000Z\r\n" +
			"END:VEVENT\r\n" +
			"BEGIN:VEVENT\r\n" +
			"SUMMARY:Second\r\n" +
			"DTSTART:20990101T110000Z\r\n" +
			"END:VEVENT\r\n" +
			"BEGIN:VEVENT\r\n" +
			"SUMMARY:Third\r\n" +
			"DTSTART:20990101T130000Z\r\n" +
			"END:VEVENT\r\n" +
			"END:VCALENDAR\r\n"))
	}))
	defer server.Close()

	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "ics", FeedURL: server.URL})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading := value.(Reading)

	if reading.Title != "First" {
		t.Fatalf("Title = %q, want %q", reading.Title, "First")
	}
	if len(reading.Upcoming) != 2 {
		t.Fatalf("Upcoming has %d entries, want 2", len(reading.Upcoming))
	}
	if reading.Upcoming[0].Title != "Second" || reading.Upcoming[1].Title != "Third" {
		t.Errorf("Upcoming = %+v, want Second then Third", reading.Upcoming)
	}
	if reading.Upcoming[0].StartLabel != "11:00" {
		t.Errorf("Upcoming[0].StartLabel = %q, want %q", reading.Upcoming[0].StartLabel, "11:00")
	}
}

func TestICSSourceOmitsUpcomingWhenNoFurtherEventsExist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BEGIN:VCALENDAR\r\n" +
			"BEGIN:VEVENT\r\n" +
			"SUMMARY:Only one\r\n" +
			"DTSTART:20990101T090000Z\r\n" +
			"END:VEVENT\r\n" +
			"END:VCALENDAR\r\n"))
	}))
	defer server.Close()

	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "ics", FeedURL: server.URL})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading := value.(Reading)
	if len(reading.Upcoming) != 0 {
		t.Errorf("Upcoming = %+v, want empty with only one qualifying event", reading.Upcoming)
	}
}

func TestMockSourcePassesThroughConfiguredUpcoming(t *testing.T) {
	cfg := config.Source{Settings: mustSettings(t, settings{
		Mode:           "mock",
		Title:          "Standup",
		StartInMinutes: 10,
		Upcoming: []AgendaItem{
			{Title: "Design review", StartLabel: "11:00"},
			{Title: "1:1", StartLabel: "14:30"},
		},
	})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	reading := value.(Reading)
	if len(reading.Upcoming) != 2 {
		t.Fatalf("Upcoming has %d entries, want 2", len(reading.Upcoming))
	}
	if reading.Upcoming[0].Title != "Design review" || reading.Upcoming[1].Title != "1:1" {
		t.Errorf("Upcoming = %+v", reading.Upcoming)
	}
}

func TestICSSourceWithNoQualifyingEventIsIdle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"))
	}))
	defer server.Close()

	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "ics", FeedURL: server.URL})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := src.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if value.(Reading).Title != "" {
		t.Errorf("Title = %q, want empty for a feed with no qualifying event", value.(Reading).Title)
	}
}

func TestICSSourceFailsOnAnUnreachableFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.Source{Settings: mustSettings(t, settings{Mode: "ics", FeedURL: server.URL})}
	src, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := src.Poll(context.Background()); err == nil {
		t.Fatal("want an error when the feed returns 404, got none")
	}
}
