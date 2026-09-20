package calendar

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"time"

	// Embeds a timezone database so TZID values resolve on any host.
	_ "time/tzdata"
)

// icsEvent holds the few VEVENT fields this source reads.
type icsEvent struct {
	Summary  string
	Location string
	Start    time.Time
	// RRule is the raw RRULE value, empty when not recurring. Expanding it is
	// nextOccurrence's job.
	RRule  string
	AllDay bool // a DATE value with no time part
}

// parseICS extracts VEVENT blocks, reading only SUMMARY, LOCATION, DTSTART and
// RRULE. Recurring and all-day events are returned for the caller to filter.
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
			continue // VCALENDAR or VTIMEZONE properties
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
			current.RRule = value
		case "DTSTART":
			start, allDay, err := parseICSTime(params, value)
			if err != nil {
				// Skip this event. One odd DTSTART must not fail the whole feed.
				continue
			}
			current.Start = start
			current.AllDay = allDay
		}
	}

	return events, nil
}

// unfoldLines joins RFC 5545 continuation lines and normalises CRLF/LF.
func unfoldLines(data []byte) ([]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Widen past bufio's 64KB token limit for long description lines.
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

// splitProperty splits an unfolded line such as
// "DTSTART;TZID=Europe/London:20260101T090000" into name, parameters and value.
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

// parseICSTime parses a DTSTART value: UTC ("Z" suffix), a TZID zone, floating
// local time, or an all-day DATE.
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
		// An unknown TZID falls back to local time. An hour out beats no event.
	}
	parsed, err := time.ParseInLocation("20060102T150405", value, loc)
	return parsed, false, err
}

// nextUpEvent returns the first entry upcomingEvents would.
func nextUpEvent(events []icsEvent, now time.Time) (icsEvent, bool) {
	up := upcomingEvents(events, now, 1)
	if len(up) == 0 {
		return icsEvent{}, false
	}
	return up[0], true
}

// upcomingEvents returns up to limit future events in order of next occurrence,
// one entry per event. A recurring event's DTSTART is usually past, so its start
// becomes the next occurrence. Unsupported RRULEs and all-day events are dropped.
func upcomingEvents(events []icsEvent, now time.Time, limit int) []icsEvent {
	var resolved []icsEvent
	for _, e := range events {
		if e.AllDay || e.Summary == "" {
			continue
		}

		start := e.Start
		if e.RRule != "" {
			occ, ok := nextOccurrence(e.Start, e.RRule, now)
			if !ok {
				continue
			}
			start = occ
		} else if !e.Start.After(now) {
			continue
		}

		resolved = append(resolved, e)
		resolved[len(resolved)-1].Start = start
	}

	sortEventsByStart(resolved)
	if len(resolved) > limit {
		resolved = resolved[:limit]
	}
	return resolved
}

func sortEventsByStart(events []icsEvent) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j-1].Start.After(events[j].Start); j-- {
			events[j-1], events[j] = events[j], events[j-1]
		}
	}
}
