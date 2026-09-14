package calendar

import (
	"testing"
	"time"
)

func TestUnfoldLinesJoinsContinuations(t *testing.T) {
	// The fold point inserts a mandatory space on the continuation line,
	// which unfolding must strip; the real space between "meeting" and
	// "title" is the trailing space already on the first line.
	data := []byte("SUMMARY:Long meeting \r\n title that wraps\r\nLOCATION:Room 1\r\n")
	lines, err := unfoldLines(data)
	if err != nil {
		t.Fatalf("unfoldLines: %v", err)
	}
	want := []string{"SUMMARY:Long meeting title that wraps", "LOCATION:Room 1"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestUnescapeText(t *testing.T) {
	cases := map[string]string{
		`Standup\, daily`:    "Standup, daily",
		`Line one\nLine two`: "Line one\nLine two",
		`A\;B`:               "A;B",
		`Back\\slash`:        `Back\slash`,
		"no escaping needed": "no escaping needed",
	}
	for in, want := range cases {
		if got := unescapeText(in); got != want {
			t.Errorf("unescapeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitProperty(t *testing.T) {
	name, params, value, ok := splitProperty("DTSTART;TZID=Europe/London:20260101T090000")
	if !ok {
		t.Fatal("splitProperty returned ok=false")
	}
	if name != "DTSTART" {
		t.Errorf("name = %q, want DTSTART", name)
	}
	if params["TZID"] != "Europe/London" {
		t.Errorf("TZID = %q, want Europe/London", params["TZID"])
	}
	if value != "20260101T090000" {
		t.Errorf("value = %q, want 20260101T090000", value)
	}
}

func TestSplitPropertyWithNoColonIsNotOK(t *testing.T) {
	_, _, _, ok := splitProperty("not a property line")
	if ok {
		t.Fatal("want ok=false for a line with no colon")
	}
}

func TestParseICSTimeUTC(t *testing.T) {
	got, allDay, err := parseICSTime(nil, "20260315T140000Z")
	if err != nil {
		t.Fatalf("parseICSTime: %v", err)
	}
	if allDay {
		t.Error("want allDay=false for a UTC DATE-TIME")
	}
	want := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseICSTimeNamedZone(t *testing.T) {
	got, allDay, err := parseICSTime(map[string]string{"TZID": "Europe/London"}, "20260615T090000")
	if err != nil {
		t.Fatalf("parseICSTime: %v", err)
	}
	if allDay {
		t.Error("want allDay=false")
	}
	// London is on BST (UTC+1) in June.
	if got.UTC().Hour() != 8 {
		t.Errorf("got %v in UTC, want 08:00 (09:00 BST)", got.UTC())
	}
}

func TestParseICSTimeAllDay(t *testing.T) {
	got, allDay, err := parseICSTime(map[string]string{"VALUE": "DATE"}, "20260301")
	if err != nil {
		t.Fatalf("parseICSTime: %v", err)
	}
	if !allDay {
		t.Error("want allDay=true")
	}
	if got.Year() != 2026 || got.Month() != 3 || got.Day() != 1 {
		t.Errorf("got %v, want 2026-03-01", got)
	}
}

func TestParseICSExtractsEvents(t *testing.T) {
	data := []byte("BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\n" +
		"SUMMARY:Standup\r\n" +
		"LOCATION:Microsoft Teams Meeting\r\n" +
		"DTSTART:20260101T090000Z\r\n" +
		"END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\n" +
		"SUMMARY:Weekly sync\r\n" +
		"DTSTART:20260102T100000Z\r\n" +
		"RRULE:FREQ=WEEKLY\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	events, err := parseICS(data)
	if err != nil {
		t.Fatalf("parseICS: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Summary != "Standup" || events[0].Location != "Microsoft Teams Meeting" {
		t.Errorf("event 0 = %+v", events[0])
	}
	if events[1].RRule != "FREQ=WEEKLY" {
		t.Errorf("event 1 RRule = %q, want %q", events[1].RRule, "FREQ=WEEKLY")
	}
}

func TestNextUpEventPicksTheSoonestFutureTimedEvent(t *testing.T) {
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	events := []icsEvent{
		{Summary: "past", Start: now.Add(-time.Hour)},
		// An RRULE shape this source does not support: excluded, same as a
		// plain past event, rather than guessed at.
		{Summary: "unsupported recurrence", Start: now.Add(time.Hour), RRule: "FREQ=SECONDLY"},
		{Summary: "all day", Start: now.Add(30 * time.Minute), AllDay: true},
		{Summary: "later", Start: now.Add(2 * time.Hour)},
		{Summary: "soonest", Start: now.Add(15 * time.Minute)},
	}

	got, ok := nextUpEvent(events, now)
	if !ok {
		t.Fatal("nextUpEvent found nothing, want \"soonest\"")
	}
	if got.Summary != "soonest" {
		t.Errorf("got %q, want %q", got.Summary, "soonest")
	}
}

func TestNextUpEventExpandsASupportedRecurringEvent(t *testing.T) {
	now := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC) // after today's 9am occurrence
	events := []icsEvent{
		// First-ever occurrence is long in the past; the daily standup is
		// still today's occurrence, which is what should come back as
		// next-up ahead of a one-off meeting later in the week.
		{Summary: "daily standup", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), RRule: "FREQ=DAILY"},
		{Summary: "later one-off", Start: now.Add(72 * time.Hour)},
	}

	got, ok := nextUpEvent(events, now)
	if !ok {
		t.Fatal("nextUpEvent found nothing, want the standup's next occurrence")
	}
	if got.Summary != "daily standup" {
		t.Errorf("got %q, want %q", got.Summary, "daily standup")
	}
	want := time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)
	if !got.Start.Equal(want) {
		t.Errorf("Start = %v, want %v (tomorrow's occurrence, today's already passed)", got.Start, want)
	}
}

func TestUpcomingEventsReturnsChronologicalOrder(t *testing.T) {
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	events := []icsEvent{
		{Summary: "third", Start: now.Add(3 * time.Hour)},
		{Summary: "past", Start: now.Add(-time.Hour)},
		{Summary: "first", Start: now.Add(15 * time.Minute)},
		{Summary: "second", Start: now.Add(2 * time.Hour)},
	}

	got := upcomingEvents(events, now, 3)

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	wantOrder := []string{"first", "second", "third"}
	for i, want := range wantOrder {
		if got[i].Summary != want {
			t.Errorf("position %d = %q, want %q", i, got[i].Summary, want)
		}
	}
}

func TestUpcomingEventsRespectsTheLimit(t *testing.T) {
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	events := []icsEvent{
		{Summary: "a", Start: now.Add(time.Hour)},
		{Summary: "b", Start: now.Add(2 * time.Hour)},
		{Summary: "c", Start: now.Add(3 * time.Hour)},
	}

	got := upcomingEvents(events, now, 2)

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
}

func TestUpcomingEventsGivesEachRecurringSeriesOnlyOneEntry(t *testing.T) {
	now := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)
	events := []icsEvent{
		{Summary: "daily standup", Start: date(2026, 1, 1, 9, 0), RRule: "FREQ=DAILY"},
		{Summary: "one-off", Start: now.Add(48 * time.Hour)},
	}

	got := upcomingEvents(events, now, 5)

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (one occurrence per series, not several)", len(got))
	}
	if got[0].Summary != "daily standup" || got[1].Summary != "one-off" {
		t.Errorf("got %q then %q", got[0].Summary, got[1].Summary)
	}
}

func TestUpcomingEventsWithFewerThanLimitReturnsWhatItHas(t *testing.T) {
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	events := []icsEvent{{Summary: "only one", Start: now.Add(time.Hour)}}

	got := upcomingEvents(events, now, 3)

	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
}

func TestNextUpEventWithNoCandidatesReturnsFalse(t *testing.T) {
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	events := []icsEvent{
		{Summary: "past", Start: now.Add(-time.Hour)},
		{Summary: "unsupported recurrence", Start: now.Add(time.Hour), RRule: "FREQ=SECONDLY"},
	}
	if _, ok := nextUpEvent(events, now); ok {
		t.Fatal("want ok=false when nothing qualifies")
	}
}
