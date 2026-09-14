package calendar

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

func TestParseRRuleReadsFreqIntervalCountUntil(t *testing.T) {
	r, ok := parseRRule("FREQ=DAILY;INTERVAL=2;COUNT=5")
	if !ok {
		t.Fatal("want ok=true")
	}
	if r.Freq != "DAILY" || r.Interval != 2 || r.Count != 5 {
		t.Errorf("got %+v", r)
	}
}

func TestParseRRuleDefaultsIntervalToOne(t *testing.T) {
	r, ok := parseRRule("FREQ=WEEKLY")
	if !ok {
		t.Fatal("want ok=true")
	}
	if r.Interval != 1 {
		t.Errorf("Interval = %d, want 1", r.Interval)
	}
}

func TestParseRRuleRejectsSubDailyFrequencies(t *testing.T) {
	for _, freq := range []string{"SECONDLY", "MINUTELY", "HOURLY", "NONSENSE"} {
		if _, ok := parseRRule("FREQ=" + freq); ok {
			t.Errorf("FREQ=%s should be unsupported", freq)
		}
	}
}

func TestParseRRuleRejectsMissingFreq(t *testing.T) {
	if _, ok := parseRRule("INTERVAL=2"); ok {
		t.Error("a rule with no FREQ should be unsupported")
	}
}

func TestParseRRuleRejectsOrdinalByDay(t *testing.T) {
	// "the third Thursday of the month" - a monthly-position rule this
	// source does not implement.
	if _, ok := parseRRule("FREQ=MONTHLY;BYDAY=3TH"); ok {
		t.Error("an ordinal BYDAY should be unsupported")
	}
}

func TestParseRRuleRejectsNegativeByMonthDay(t *testing.T) {
	if _, ok := parseRRule("FREQ=MONTHLY;BYMONTHDAY=-1"); ok {
		t.Error("a negative BYMONTHDAY should be unsupported")
	}
}

func TestParseRRuleRejectsBySetPosAndByWeekNoAndByYearDay(t *testing.T) {
	for _, rule := range []string{"FREQ=MONTHLY;BYSETPOS=1", "FREQ=WEEKLY;BYWEEKNO=3", "FREQ=YEARLY;BYYEARDAY=100"} {
		if _, ok := parseRRule(rule); ok {
			t.Errorf("%q should be unsupported", rule)
		}
	}
}

func TestNextOccurrenceDaily(t *testing.T) {
	dtstart := date(2026, 1, 1, 9, 0)
	after := date(2026, 1, 5, 8, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=DAILY", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 1, 5, 9, 0)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceDailyWithInterval(t *testing.T) {
	dtstart := date(2026, 1, 1, 9, 0)
	// Every 3 days: Jan 1, 4, 7, 10, 13...
	after := date(2026, 1, 9, 0, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=DAILY;INTERVAL=3", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 1, 10, 9, 0)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceWeeklyNoByDay(t *testing.T) {
	dtstart := date(2026, 1, 6, 9, 0) // a Tuesday
	after := date(2026, 1, 15, 0, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=WEEKLY", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 1, 20, 9, 0) // the next Tuesday on/after the 15th
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceWeeklyWithByDayWeekdays(t *testing.T) {
	// A daily standup on weekdays, dtstart on a Monday.
	dtstart := date(2026, 1, 5, 9, 0) // Monday
	// Friday the 9th at 10:00 - after that day's standup, before the weekend.
	after := date(2026, 1, 9, 10, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 1, 12, 9, 0) // the following Monday, weekend skipped
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceWeeklyWithByDaySameDay(t *testing.T) {
	dtstart := date(2026, 1, 5, 9, 0) // Monday
	// Later the same Monday, before the Wednesday occurrence.
	after := date(2026, 1, 5, 12, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=WEEKLY;BYDAY=MO,WE", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 1, 7, 9, 0) // Wednesday
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceMonthlySameDayOfMonth(t *testing.T) {
	dtstart := date(2026, 1, 15, 9, 0)
	after := date(2026, 2, 1, 0, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=MONTHLY", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 2, 15, 9, 0)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceMonthlySkipsMonthsWithoutThatDay(t *testing.T) {
	// The 31st: no occurrence in April, June, etc. Must not roll into the
	// next month the way time.AddDate would.
	dtstart := date(2026, 1, 31, 9, 0)
	after := date(2026, 3, 1, 0, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=MONTHLY", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 3, 31, 9, 0) // February has no 31st, so skipped
	if !got.Equal(want) {
		t.Errorf("got %v, want %v (February should be skipped, not rolled into March)", got, want)
	}
}

func TestNextOccurrenceMonthlyWithByMonthDay(t *testing.T) {
	dtstart := date(2026, 1, 1, 9, 0)
	after := date(2026, 1, 10, 0, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=MONTHLY;BYMONTHDAY=1,15", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 1, 15, 9, 0)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceYearly(t *testing.T) {
	dtstart := date(2020, 6, 1, 9, 0)
	after := date(2026, 1, 1, 0, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=YEARLY", after)
	if !ok {
		t.Fatal("want an occurrence")
	}
	want := date(2026, 6, 1, 9, 0)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceRespectsCount(t *testing.T) {
	dtstart := date(2026, 1, 1, 9, 0)
	// 3 occurrences total: Jan 1, 2, 3. Asking for anything after Jan 3
	// should find nothing.
	after := date(2026, 1, 3, 10, 0)

	if _, ok := nextOccurrence(dtstart, "FREQ=DAILY;COUNT=3", after); ok {
		t.Error("want ok=false once COUNT is exhausted")
	}
}

func TestNextOccurrenceRespectsUntil(t *testing.T) {
	dtstart := date(2026, 1, 1, 9, 0)
	after := date(2026, 1, 10, 0, 0)

	if _, ok := nextOccurrence(dtstart, "FREQ=DAILY;UNTIL=20260105T090000Z", after); ok {
		t.Error("want ok=false once past UNTIL")
	}
}

func TestNextOccurrenceWithinCountStillReturned(t *testing.T) {
	dtstart := date(2026, 1, 1, 9, 0)
	after := date(2026, 1, 1, 10, 0)

	got, ok := nextOccurrence(dtstart, "FREQ=DAILY;COUNT=3", after)
	if !ok {
		t.Fatal("want an occurrence still within COUNT")
	}
	want := date(2026, 1, 2, 9, 0)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrenceUnsupportedRuleReturnsFalse(t *testing.T) {
	dtstart := date(2026, 1, 1, 9, 0)
	after := date(2026, 1, 5, 0, 0)

	if _, ok := nextOccurrence(dtstart, "FREQ=SECONDLY", after); ok {
		t.Error("want ok=false for an unsupported rule")
	}
}
