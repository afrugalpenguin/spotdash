// Package calendar reports the next-up event and a short agenda after it.
// Mode "mock" plays a configured sample event. Mode "ics" reads a published ICS
// feed. See docs/architecture.md, "Calendar".
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

// Name is the key this source is configured under.
const Name = "calendar"

// DefaultNotifyMinutes is how far out an event counts as urgent when
// notify_minutes is unset. It matches the face's own warn colour.
const DefaultNotifyMinutes = 15

// DefaultShowSeconds is how long an auto-switch holds the calendar face when
// show_seconds is unset.
const DefaultShowSeconds = 45

// AgendaSize is how many events the face knows about in total, the next-up
// event included.
const AgendaSize = 8

// Reading is what the calendar source publishes. An empty Title means no
// upcoming event, which the face shows as idle.
type Reading struct {
	Title string `json:"title"`
	// Location is the feed's own text, such as "Microsoft Teams Meeting".
	Location string `json:"location"`
	// StartLabel is preformatted because the display device may have the wrong
	// locale.
	StartLabel string `json:"start_label"`
	// MinutesUntil lets the face tick the countdown locally between polls.
	MinutesUntil float64 `json:"minutes_until"`
	// Urgent is true within notify_minutes. The client auto-switches on it, so
	// the threshold lives only here.
	Urgent bool `json:"urgent"`
	// ShowSeconds tells the client how long to hold an auto-switch open.
	ShowSeconds int `json:"show_seconds"`
	// Upcoming is up to AgendaSize-1 further events, title and start only.
	Upcoming []AgendaItem `json:"upcoming,omitempty"`
}

// AgendaItem is one entry in the mini agenda below the primary event.
type AgendaItem struct {
	Title      string `json:"title"`
	StartLabel string `json:"start_label"`
}

type settings struct {
	Mode string `json:"mode"`

	// Common to both modes.
	NotifyMinutes int `json:"notify_minutes"`
	ShowSeconds   int `json:"show_seconds"`

	// mode: "mock"
	Title    string `json:"title"`
	Location string `json:"location"`
	// Set at most one of StartInMinutes (relative to agent start) or StartAt
	// (a fixed "HH:MM", for repeatable screenshots).
	StartInMinutes int    `json:"start_in_minutes"`
	StartAt        string `json:"start_at"`
	// Upcoming is canned agenda entries, shown as given.
	Upcoming []AgendaItem `json:"upcoming"`

	// mode: "ics"
	FeedURL string `json:"feed_url"`
}

// Source reports the next-up event from a mock event or an ICS feed.
type Source struct {
	interval      time.Duration
	notifyMinutes int
	showSeconds   int

	// mock mode
	mockTitle    string
	mockLocation string
	mockMinutes  int
	mockStartAt  string
	mockUpcoming []AgendaItem
	start        time.Time // computed once, on the first poll; see Poll

	// ics mode
	feedURL string
	http    interface {
		Do(req *http.Request) (*http.Response, error)
	}

	// now is overridden in tests.
	now func() time.Time
}

// New builds the calendar source from cfg.
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
	if s.NotifyMinutes == 0 {
		s.NotifyMinutes = DefaultNotifyMinutes
	}
	if s.ShowSeconds == 0 {
		s.ShowSeconds = DefaultShowSeconds
	}

	switch s.Mode {
	case "":
		return nil, fmt.Errorf(`"mode" is required: use "mock" to show a configured sample event, or "ics" to connect a real feed`)
	case "mock":
		return newMockSource(cfg.Interval(), s)
	case "ics":
		return newICSSource(cfg.Interval(), s)
	default:
		return nil, fmt.Errorf(`"mode" is %q, want "mock" or "ics"`, s.Mode)
	}
}

func newMockSource(interval time.Duration, s settings) (*Source, error) {
	if s.StartInMinutes != 0 && s.StartAt != "" {
		return nil, fmt.Errorf(`"start_in_minutes" and "start_at" cannot both be set: pick one way to place the sample event`)
	}
	hasTitle := s.Title != ""
	hasStart := s.StartInMinutes != 0 || s.StartAt != ""
	if hasTitle != hasStart {
		return nil, fmt.Errorf(`"title" and one of "start_in_minutes"/"start_at" must be set together, or both left out for no event`)
	}
	if s.StartAt != "" {
		if _, err := time.Parse("15:04", s.StartAt); err != nil {
			return nil, fmt.Errorf(`"start_at" is %q, want "HH:MM"`, s.StartAt)
		}
	}
	return &Source{
		interval:      interval,
		notifyMinutes: s.NotifyMinutes,
		showSeconds:   s.ShowSeconds,
		mockTitle:     s.Title,
		mockLocation:  s.Location,
		mockMinutes:   s.StartInMinutes,
		mockStartAt:   s.StartAt,
		mockUpcoming:  s.Upcoming,
		now:           time.Now,
	}, nil
}

func newICSSource(interval time.Duration, s settings) (*Source, error) {
	if s.FeedURL == "" {
		return nil, fmt.Errorf(`"mode" is "ics" but no "feed_url" is configured`)
	}
	return &Source{
		interval:      interval,
		notifyMinutes: s.NotifyMinutes,
		showSeconds:   s.ShowSeconds,
		feedURL:       s.FeedURL,
		http:          &http.Client{Timeout: 15 * time.Second},
		now:           time.Now,
	}, nil
}

// Name identifies the source.
func (s *Source) Name() string { return Name }

// Interval is the configured poll period.
func (s *Source) Interval() time.Duration { return s.interval }

// Poll returns the next-up event, or an empty reading when none is
// configured or found.
func (s *Source) Poll(ctx context.Context) (any, error) {
	if s.feedURL != "" {
		return s.pollICS(ctx)
	}
	return s.pollMock(), nil
}

// pollMock returns the configured sample event. The start is fixed on the
// first poll and the countdown derives from the clock, so it survives a reload.
func (s *Source) pollMock() Reading {
	if s.mockTitle == "" {
		return Reading{}
	}

	now := s.now()
	if s.start.IsZero() {
		if s.mockStartAt != "" {
			s.start = nextOccurrenceOf(s.mockStartAt, now)
		} else {
			s.start = now.Add(time.Duration(s.mockMinutes) * time.Minute)
		}
	}
	return s.reading(s.mockTitle, s.mockLocation, s.start, now, s.mockUpcoming)
}

// nextOccurrenceOf combines an "HH:MM" time with now's date, rolling to
// tomorrow if it has passed. newMockSource has validated clockTime.
func nextOccurrenceOf(clockTime string, now time.Time) time.Time {
	parsed, err := time.Parse("15:04", clockTime)
	if err != nil {
		return now
	}
	candidate := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	if candidate.Before(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}

// pollICS fetches the feed and picks the next-up event. A feed that cannot be
// fetched or parsed is a failure. A good feed with no qualifying event is an
// empty reading.
func (s *Source) pollICS(ctx context.Context) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("calendar: building request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calendar: fetching feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("calendar: fetching feed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("calendar: reading feed: %w", err)
	}

	events, err := parseICS(data)
	if err != nil {
		return nil, fmt.Errorf("calendar: parsing feed: %w", err)
	}

	now := s.now()
	upcoming := upcomingEvents(events, now, AgendaSize)
	if len(upcoming) == 0 {
		return Reading{}, nil
	}
	primary := upcoming[0]

	var agenda []AgendaItem
	for _, e := range upcoming[1:] {
		agenda = append(agenda, AgendaItem{Title: e.Summary, StartLabel: e.Start.Format("15:04")})
	}

	return s.reading(primary.Summary, primary.Location, primary.Start, now, agenda), nil
}

// reading builds the published Reading for either mode.
func (s *Source) reading(title, location string, start, now time.Time, upcoming []AgendaItem) Reading {
	minutesUntil := start.Sub(now).Minutes()
	return Reading{
		Title:        title,
		Location:     location,
		StartLabel:   start.Format("15:04"),
		MinutesUntil: minutesUntil,
		Urgent:       minutesUntil <= float64(s.notifyMinutes),
		ShowSeconds:  s.showSeconds,
		Upcoming:     upcoming,
	}
}
