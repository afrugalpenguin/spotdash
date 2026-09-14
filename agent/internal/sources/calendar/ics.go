package calendar

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"time"

	// The agent ships as a single static binary, so a TZID like
	// "Europe/London" in a feed must resolve without relying on the target
	// machine having its own IANA timezone database. This embeds one.
	_ "time/tzdata"
)

// icsEvent is the handful of VEVENT fields this source cares about. Nothing
// about attendees, alarms, categories, or anything else an ICS feed can carry.
type icsEvent struct {
	Summary   string
	Location  string
	Start     time.Time
	Recurring bool // has an RRULE
	AllDay    bool // DATE rather than DATE-TIME
}

// parseICS extracts VEVENT blocks from raw ICS data.
//
// Deliberately narrow: only SUMMARY, LOCATION, DTSTART and the presence of
// RRULE are read. Recurring events and all-day events are still returned
// (recognisable as flagged, not skipped here) so the caller decides what to
// do with them; parsing is not where policy belongs.
func parseICS(data []byte) ([]icsEvent, error) {
	lines, err := unfoldLines(data)
	if err != nil {
		return nil, err
	}

	var events []icsEvent
	var current *icsEvent

	for _, line := range lines {
		switch {
		case line == "BEGIN:VEVENT":
			current = &icsEvent{}
			continue
		case line == "END:VEVENT":
			if current != nil {
				events = append(events, *current)
				current = nil
			}
			continue
		}
		if current == nil {
			continue // outside any VEVENT, e.g. VCALENDAR or VTIMEZONE properties
		}

		name, params, value, ok := splitProperty(line)
		if !ok {
			continue
		}

		switch name {
		case "SUMMARY":
			current.Summary = unescapeText(value)
		case "LOCATION":
			current.Location = unescapeText(value)
		case "RRULE":
			current.Recurring = true
		case "DTSTART":
			start, allDay, err := parseICSTime(params, value)
			if err != nil {
				// A DTSTART this source cannot parse is not fatal to the whole
				// feed: skip this one event rather than fail every reading
				// because one entry uses a form not handled yet.
				continue
			}
			current.Start = start
			current.AllDay = allDay
		}
	}

	return events, nil
}

// unfoldLines joins RFC 5545 continuation lines (a line starting with a
// single space or tab continues the previous one) and normalises CRLF/LF.
func unfoldLines(data []byte) ([]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// A calendar with a very long description line should not fail to parse;
	// widen past bufio's default 64KB token limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lines []string
	for scanner.Scan() {
		raw := strings.TrimRight(scanner.Text(), "\r")
		if (strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += raw[1:]
			continue
		}
		lines = append(lines, raw)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading ics data: %w", err)
	}
	return lines, nil
}

// splitProperty splits one unfolded ICS line into its name, parameters and
// value, e.g. "DTSTART;TZID=Europe/London:20260101T090000" splits into
// "DTSTART", {"TZID": "Europe/London"}, "20260101T090000".
func splitProperty(line string) (name string, params map[string]string, value string, ok bool) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return "", nil, "", false
	}
	head, value := line[:colon], line[colon+1:]

	parts := strings.Split(head, ";")
	name = strings.ToUpper(parts[0])
	if name == "" {
		return "", nil, "", false
	}

	params = make(map[string]string, len(parts)-1)
	for _, p := range parts[1:] {
		if eq := strings.IndexByte(p, '='); eq >= 0 {
			params[strings.ToUpper(p[:eq])] = p[eq+1:]
		}
	}
	return name, params, value, true
}

// unescapeText undoes RFC 5545 TEXT escaping: \\, \;, \,, and \n/\N.
func unescapeText(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			switch value[i+1] {
			case 'n', 'N':
				b.WriteByte('\n')
			case '\\', ';', ',':
				b.WriteByte(value[i+1])
			default:
				b.WriteByte(value[i])
				continue
			}
			i++
			continue
		}
		b.WriteByte(value[i])
	}
	return b.String()
}

// parseICSTime parses a DTSTART value in any of the three forms the format
// allows: UTC ("Z" suffix), a named zone via the TZID parameter, floating
// local time, or an all-day DATE with no time component at all.
func parseICSTime(params map[string]string, value string) (t time.Time, allDay bool, err error) {
	if params["VALUE"] == "DATE" || (len(value) == 8 && !strings.Contains(value, "T")) {
		parsed, err := time.ParseInLocation("20060102", value, time.Local)
		if err != nil {
			return time.Time{}, false, err
		}
		return parsed, true, nil
	}

	if strings.HasSuffix(value, "Z") {
		parsed, err := time.Parse("20060102T150405Z", value)
		return parsed, false, err
	}

	loc := time.Local
	if tzid := params["TZID"]; tzid != "" {
		if named, err := time.LoadLocation(tzid); err == nil {
			loc = named
		}
		// An unrecognised TZID falls back to local time rather than failing
		// the whole event: a wrong-by-an-hour reading is still more useful
		// than none.
	}
	parsed, err := time.ParseInLocation("20060102T150405", value, loc)
	return parsed, false, err
}

// nextUpEvent picks the soonest event that is still in the future, excluding
// recurring and all-day events.
//
// Recurring events are excluded rather than guessed at: this source does not
// expand RRULEs, so a recurring event's own DTSTART is just its first ever
// occurrence, almost always in the past, and showing that as "next up" would
// be actively wrong rather than merely incomplete. All-day events are
// excluded because "next up in N minutes" does not mean anything for one.
// Both are a known v1 limitation, not a silent gap: a daily standup will not
// appear here until RRULE expansion is built.
func nextUpEvent(events []icsEvent, now time.Time) (icsEvent, bool) {
	var best icsEvent
	found := false
	for _, e := range events {
		if e.Recurring || e.AllDay || e.Summary == "" {
			continue
		}
		if !e.Start.After(now) {
			continue
		}
		if !found || e.Start.Before(best.Start) {
			best = e
			found = true
		}
	}
	return best, found
}
