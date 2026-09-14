// Package calendar reports the next-up event from a calendar.
//
// Only the mock provider exists so far: enough to develop and judge the face
// on the real panel before any feed plumbing is built. The design (ICS feed,
// auto-switch on an imminent event) is not implemented yet; see
// docs/plans/2026-09-14-calendar-face-design.md once it exists.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

// Name is the key this source is configured under.
const Name = "calendar"

// Reading is what the calendar source publishes. An empty Title means no
// upcoming event, a normal state the face shows as idle rather than an
// error.
type Reading struct {
	Title string `json:"title"`
	// StartLabel is preformatted, the same reasoning as clock's Time field:
	// the display device cannot be trusted to have the right locale.
	StartLabel string `json:"start_label"`
	// MinutesUntil lets the face tick the countdown down locally between
	// polls, the same pattern spotify's PositionMS uses for elapsed time.
	MinutesUntil float64 `json:"minutes_until"`
}

type settings struct {
	Mode string `json:"mode"`

	// mode: "mock"
	Title          string `json:"title"`
	StartInMinutes int    `json:"start_in_minutes"`
}

// Source reports the next-up event.
type Source struct {
	interval time.Duration
	settings settings

	// start is computed once, on the first poll, from start_in_minutes and
	// whatever `now` reads at that moment. Fixing it there rather than
	// recomputing start_in_minutes-from-now on every poll means the
	// countdown actually counts down instead of reading the same value
	// forever.
	start time.Time

	// now is overridden in tests.
	now func() time.Time
}

// New builds the calendar source. Only "mock" exists today.
func New(cfg config.Source) (interface {
	Name() string
	Poll(context.Context) (any, error)
	Interval() time.Duration
}, error) {
	var s settings
	if len(cfg.Settings) > 0 {
		if err := json.Unmarshal(cfg.Settings, &s); err != nil {
			return nil, fmt.Errorf("reading settings: %w", err)
		}
	}

	switch s.Mode {
	case "":
		return nil, fmt.Errorf(`"mode" is required: use "mock" to show a configured sample event`)
	case "mock":
		return newMockSource(cfg.Interval(), s)
	default:
		return nil, fmt.Errorf(`"mode" is %q, want "mock"`, s.Mode)
	}
}

func newMockSource(interval time.Duration, s settings) (*Source, error) {
	hasTitle := s.Title != ""
	hasStart := s.StartInMinutes != 0
	if hasTitle != hasStart {
		return nil, fmt.Errorf(`"title" and "start_in_minutes" must be set together, or both left out for no event`)
	}
	return &Source{
		interval: interval,
		settings: s,
		now:      time.Now,
	}, nil
}

// Name identifies the source.
func (s *Source) Name() string { return Name }

// Interval is the configured poll period.
func (s *Source) Interval() time.Duration { return s.interval }

// Poll returns the next-up event, or an empty reading when none is
// configured.
func (s *Source) Poll(_ context.Context) (any, error) {
	if s.settings.Title == "" {
		return Reading{}, nil
	}

	now := s.now()
	if s.start.IsZero() {
		s.start = now.Add(time.Duration(s.settings.StartInMinutes) * time.Minute)
	}
	minutesUntil := s.start.Sub(now).Minutes()

	return Reading{
		Title:        s.settings.Title,
		StartLabel:   s.start.Format("15:04"),
		MinutesUntil: minutesUntil,
	}, nil
}
