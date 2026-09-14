package calendar

import (
	"strconv"
	"strings"
	"time"
)

// recurrenceCap bounds how many occurrences this expands looking for one
// after the reference time, so a pathological or very-far-future rule
// cannot spin forever. 500 occurrences covers, at the coarsest supported
// cadence (yearly), 500 years of headroom - far more than "next up" will
// ever need.
const recurrenceCap = 500

// rrule is the subset of RFC 5545's recurrence rule this source understands.
// Deliberately modest: the common shapes an actual person's calendar uses
// (a daily standup, a weekly sync on specific days, a monthly on a given
// date, an annual event), not the full spec. BYSETPOS, BYWEEKNO, BYYEARDAY,
// negative BYMONTHDAY, ordinal BYDAY ("3TH"), WKST, and sub-daily
// frequencies are all unsupported - parseRRule reports that rather than
// guessing, and the caller's fallback is to treat the event as it always
// did before RRULE expansion existed: excluded from next-up.
type rrule struct {
	Freq       string // DAILY, WEEKLY, MONTHLY, YEARLY
	Interval   int    // default 1
	Count      int    // 0 means unbounded
	Until      time.Time
	HasUntil   bool
	ByDay      []time.Weekday // WEEKLY only
	ByMonthDay []int          // MONTHLY only, positive days
}

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// parseRRule reads an RRULE property value. ok is false for anything this
// source does not support, which the caller treats as "cannot expand this
// one", not a parse failure for the rest of the feed.
func parseRRule(value string) (r rrule, ok bool) {
	r.Interval = 1

	for _, part := range strings.Split(value, ";") {
		key, val, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		key = strings.ToUpper(key)

		switch key {
		case "FREQ":
			switch strings.ToUpper(val) {
			case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
				r.Freq = strings.ToUpper(val)
			default:
				return rrule{}, false // SECONDLY/MINUTELY/HOURLY, or unrecognised
			}
		case "INTERVAL":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return rrule{}, false
			}
			r.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return rrule{}, false
			}
			r.Count = n
		case "UNTIL":
			until, _, err := parseICSTime(nil, val)
			if err != nil {
				return rrule{}, false
			}
			r.Until = until
			r.HasUntil = true
		case "BYDAY":
			for _, code := range strings.Split(val, ",") {
				// An ordinal prefix ("1MO", "-1FR", for "the first Monday"
				// or "the last Friday") is a monthly-position rule this
				// source does not implement.
				if len(code) != 2 {
					return rrule{}, false
				}
				wd, known := weekdayCodes[strings.ToUpper(code)]
				if !known {
					return rrule{}, false
				}
				r.ByDay = append(r.ByDay, wd)
			}
		case "BYMONTHDAY":
			for _, s := range strings.Split(val, ",") {
				n, err := strconv.Atoi(s)
				if err != nil || n < 1 {
					// A negative BYMONTHDAY ("-1" for "the last day of the
					// month") is a shape this source does not implement.
					return rrule{}, false
				}
				r.ByMonthDay = append(r.ByMonthDay, n)
			}
		case "BYSETPOS", "BYWEEKNO", "BYYEARDAY", "WKST":
			return rrule{}, false
		}
	}

	if r.Freq == "" {
		return rrule{}, false
	}
	if len(r.ByDay) > 0 && r.Freq != "WEEKLY" {
		return rrule{}, false // BYDAY without ordinals is only implemented for WEEKLY
	}
	if len(r.ByMonthDay) > 0 && r.Freq != "MONTHLY" {
		return rrule{}, false
	}
	return r, true
}

// nextOccurrence returns the earliest occurrence of dtstart recurring under
// rruleValue that falls strictly after `after`, honouring COUNT and UNTIL.
// found is false when rruleValue is not a shape this source supports, or
// when every occurrence within recurrenceCap is not after `after` (the rule
// has already ended, or is coarser than the cap can reach that far out).
func nextOccurrence(dtstart time.Time, rruleValue string, after time.Time) (t time.Time, found bool) {
	r, ok := parseRRule(rruleValue)
	if !ok {
		return time.Time{}, false
	}

	for i, occ := range candidateOccurrences(dtstart, r) {
		if r.Count > 0 && i >= r.Count {
			return time.Time{}, false
		}
		if r.HasUntil && occ.After(r.Until) {
			return time.Time{}, false
		}
		if occ.After(after) {
			return occ, true
		}
	}
	return time.Time{}, false
}

// candidateOccurrences generates up to recurrenceCap occurrence times in
// chronological order, starting from dtstart. It does not itself apply
// COUNT or UNTIL - nextOccurrence does, so this stays a plain generator.
func candidateOccurrences(dtstart time.Time, r rrule) []time.Time {
	switch r.Freq {
	case "DAILY":
		return dailyOccurrences(dtstart, r.Interval)
	case "WEEKLY":
		if len(r.ByDay) > 0 {
			return weeklyByDayOccurrences(dtstart, r.Interval, r.ByDay)
		}
		return dailyOccurrences(dtstart, r.Interval*7)
	case "MONTHLY":
		if len(r.ByMonthDay) > 0 {
			return monthlyByMonthDayOccurrences(dtstart, r.Interval, r.ByMonthDay)
		}
		return monthlyOccurrences(dtstart, r.Interval)
	case "YEARLY":
		return yearlyOccurrences(dtstart, r.Interval)
	default:
		return nil
	}
}

func dailyOccurrences(dtstart time.Time, intervalDays int) []time.Time {
	out := make([]time.Time, 0, recurrenceCap)
	for i := 0; i < recurrenceCap; i++ {
		out = append(out, dtstart.AddDate(0, 0, i*intervalDays))
	}
	return out
}

func monthlyOccurrences(dtstart time.Time, intervalMonths int) []time.Time {
	out := make([]time.Time, 0, recurrenceCap)
	day := dtstart.Day()
	for i := 0; len(out) < recurrenceCap; i++ {
		candidate := addMonthsOnDay(dtstart, i*intervalMonths, day)
		if !candidate.IsZero() {
			out = append(out, candidate)
		}
		if i > recurrenceCap*2 {
			break // every candidate month overflowed (e.g. day 31 mostly); stop rather than loop far past the cap
		}
	}
	return out
}

func monthlyByMonthDayOccurrences(dtstart time.Time, intervalMonths int, days []int) []time.Time {
	sorted := append([]int(nil), days...)
	sortInts(sorted)

	out := make([]time.Time, 0, recurrenceCap)
	for i := 0; len(out) < recurrenceCap; i++ {
		for _, day := range sorted {
			candidate := addMonthsOnDay(dtstart, i*intervalMonths, day)
			if candidate.IsZero() || candidate.Before(dtstart) {
				continue // this period's day does not exist, or (period 0) falls before dtstart itself
			}
			out = append(out, candidate)
		}
		if i > recurrenceCap*2 {
			break
		}
	}
	sortTimes(out)
	return out
}

// addMonthsOnDay returns dtstart's month plus monthOffset months, on the
// given day-of-month, at dtstart's time-of-day. Returns the zero Time when
// that day does not exist in the target month (e.g. day 31 in April),
// rather than let time.AddDate silently roll into the following month.
func addMonthsOnDay(dtstart time.Time, monthOffset, day int) time.Time {
	firstOfMonth := time.Date(dtstart.Year(), dtstart.Month(), 1, dtstart.Hour(), dtstart.Minute(), dtstart.Second(), 0, dtstart.Location())
	firstOfMonth = firstOfMonth.AddDate(0, monthOffset, 0)
	candidate := firstOfMonth.AddDate(0, 0, day-1)
	if candidate.Month() != firstOfMonth.Month() {
		return time.Time{}
	}
	return candidate
}

func yearlyOccurrences(dtstart time.Time, intervalYears int) []time.Time {
	out := make([]time.Time, 0, recurrenceCap)
	for i := 0; i < recurrenceCap; i++ {
		out = append(out, dtstart.AddDate(i*intervalYears, 0, 0))
	}
	return out
}

// weeklyByDayOccurrences generates occurrences on each of days within every
// qualifying week, interval weeks apart, starting from the calendar week
// containing dtstart. Only occurrences on or after dtstart itself count,
// which can trim the first week to fewer than len(days) occurrences.
func weeklyByDayOccurrences(dtstart time.Time, intervalWeeks int, days []time.Weekday) []time.Time {
	sorted := append([]time.Weekday(nil), days...)
	sortWeekdays(sorted)

	// Monday of dtstart's week, purely as a stable anchor; which day a week
	// is considered to "start" on does not change which dates get produced.
	daysSinceMonday := (int(dtstart.Weekday()) + 6) % 7
	weekStart := dtstart.AddDate(0, 0, -daysSinceMonday)

	out := make([]time.Time, 0, recurrenceCap)
	for week := 0; len(out) < recurrenceCap; week += intervalWeeks {
		periodStart := weekStart.AddDate(0, 0, 7*week)
		for _, wd := range sorted {
			offset := (int(wd) - int(periodStart.Weekday()) + 7) % 7
			candidate := periodStart.AddDate(0, 0, offset)
			if candidate.Before(dtstart) {
				continue
			}
			out = append(out, candidate)
		}
	}
	sortTimes(out)
	return out
}

func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func sortWeekdays(s []time.Weekday) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func sortTimes(s []time.Time) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].After(s[j]); j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
