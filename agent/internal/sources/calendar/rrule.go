package calendar

import (
	"strconv"
	"strings"
	"time"
)

// recurrenceCap bounds how many occurrences are expanded, so a pathological
// rule cannot spin forever. Yearly rules get 500 years of headroom.
const recurrenceCap = 500

// rrule is the subset of RFC 5545 recurrence this source understands. The
// unsupported parts are listed in docs/architecture.md, "Calendar". parseRRule
// reports them and the event is excluded from next-up.
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

// parseRRule reads an RRULE value. ok is false for anything unsupported, which
// only excludes that one event.
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
				// An ordinal prefix such as "1MO" or "-1FR" is unsupported.
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
					// Negative values such as "-1" (last day) are unsupported.
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

// nextOccurrence returns the earliest occurrence of dtstart under rruleValue
// strictly after `after`, honouring COUNT and UNTIL. found is false for an
// unsupported rule, one that has ended, or none within recurrenceCap.
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

// candidateOccurrences generates up to recurrenceCap occurrences in order from
// dtstart. nextOccurrence applies COUNT and UNTIL.
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
			break // nearly every month overflowed (day 31)
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
				continue // no such day this month, or before dtstart
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

// addMonthsOnDay returns dtstart's month plus monthOffset months, on the given
// day, at dtstart's time of day. It returns the zero Time when the day does not
// exist (day 31 in April), where AddDate would roll into the next month.
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

// weeklyByDayOccurrences generates occurrences on each of days in every
// intervalWeeks-th week, from the week containing dtstart. Occurrences before
// dtstart are dropped, which can trim the first week.
func weeklyByDayOccurrences(dtstart time.Time, intervalWeeks int, days []time.Weekday) []time.Time {
	sorted := append([]time.Weekday(nil), days...)
	sortWeekdays(sorted)

	// Monday of dtstart's week. Any fixed anchor gives the same dates.
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
