// Package clock is the time and date source. It also computes whether the
// display should be asleep, from a configurable window on a 24 hour clock.
package clock

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

// Name is the key this source is configured under.
const Name = "clock"

// Reading is what the clock source publishes.
type Reading struct {
	// ISO is the full timestamp, for any client that wants to format it itself.
	ISO string `json:"iso"`
	// Time is preformatted as HH:MM, because the display device cannot be
	// trusted to have the right locale or timezone.
	Time    string `json:"time"`
	Seconds int    `json:"seconds"`
	Date    string `json:"date"`
	Sleep   bool   `json:"sleep"`
}

type settings struct {
	SleepStart string `json:"sleep_start"`
	SleepEnd   string `json:"sleep_end"`
}

// Source reports the current time.
type Source struct {
	interval time.Duration

	// hasWindow is false when no sleep window is configured, in which case the
	// panel never blanks itself.
	hasWindow  bool
	startMins  int
	endMins    int
	dateLayout string

	// now is overridden in tests.
	now func() time.Time
}

// New builds the clock source from its config block. An unparseable sleep
// window is an error here rather than on every poll, so a typo stops the agent
// instead of quietly disabling the feature.
func New(cfg config.Source) (*Source, error) {
	var s settings
	if len(cfg.Settings) > 0 {
		if err := json.Unmarshal(cfg.Settings, &s); err != nil {
			return nil, fmt.Errorf("reading settings: %w", err)
		}
	}

	src := &Source{
		interval:   cfg.Interval(),
		dateLayout: "Mon 2 Jan",
		now:        time.Now,
	}

	switch {
	case s.SleepStart == "" && s.SleepEnd == "":
		// No window. Never sleeps.
	case s.SleepStart == "":
		return nil, fmt.Errorf(`"sleep_end" is set but "sleep_start" is missing: a sleep window needs both ends`)
	case s.SleepEnd == "":
		return nil, fmt.Errorf(`"sleep_start" is set but "sleep_end" is missing: a sleep window needs both ends`)
	default:
		start, err := parseClockTime(s.SleepStart)
		if err != nil {
			return nil, fmt.Errorf(`"sleep_start" %q: %w`, s.SleepStart, err)
		}
		end, err := parseClockTime(s.SleepEnd)
		if err != nil {
			return nil, fmt.Errorf(`"sleep_end" %q: %w`, s.SleepEnd, err)
		}
		src.hasWindow = true
		src.startMins = start
		src.endMins = end
	}

	return src, nil
}

// Name identifies the source.
func (s *Source) Name() string { return Name }

// Interval is the configured poll period.
func (s *Source) Interval() time.Duration { return s.interval }

// Poll returns the current time. It cannot fail, which makes it the useful
// source to check the rest of the pipeline with.
func (s *Source) Poll(_ context.Context) (any, error) {
	now := s.now()
	return Reading{
		ISO:     now.Format(time.RFC3339),
		Time:    now.Format("15:04"),
		Seconds: now.Second(),
		Date:    now.Format(s.dateLayout),
		Sleep:   s.sleeping(now),
	}, nil
}

func (s *Source) sleeping(now time.Time) bool {
	if !s.hasWindow || s.startMins == s.endMins {
		// An empty window is treated as never sleeping. Reading it the other
		// way would blank the panel permanently, which is the worse failure.
		return false
	}

	mins := now.Hour()*60 + now.Minute()
	if s.startMins < s.endMins {
		return mins >= s.startMins && mins < s.endMins
	}
	// The window crosses midnight, which is the normal case for a sleep window.
	return mins >= s.startMins || mins < s.endMins
}

// parseClockTime reads "HH:MM" or "H:MM" on a 24 hour clock and returns minutes
// since midnight.
func parseClockTime(value string) (int, error) {
	var hour, minute int
	n, err := fmt.Sscanf(value, "%d:%d", &hour, &minute)
	if err != nil || n != 2 {
		return 0, fmt.Errorf("want a 24 hour time such as 23:30")
	}
	// Sscanf accepts trailing junk, so rebuild and compare.
	if fmt.Sprintf("%d:%02d", hour, minute) != normalizeHour(value) {
		return 0, fmt.Errorf("want a 24 hour time such as 23:30")
	}
	if hour < 0 || hour > 23 {
		return 0, fmt.Errorf("hour %d is out of range, want 0 to 23", hour)
	}
	if minute < 0 || minute > 59 {
		return 0, fmt.Errorf("minute %d is out of range, want 0 to 59", minute)
	}
	return hour*60 + minute, nil
}

// normalizeHour strips a leading zero from the hour so that "07:00" and "7:00"
// compare equal against the reconstructed form.
func normalizeHour(value string) string {
	if len(value) > 1 && value[0] == '0' {
		return value[1:]
	}
	return value
}
