// Package calendar reports the next-up event from a calendar.
//
// "mock" plays a configured sample event, for developing and judging the
// face without a real feed. "ics" fetches and parses a real ICS feed URL,
// which is what Outlook (and anything else that publishes one) exposes
// without needing OAuth. Recurring and all-day events are a known v1
// limitation: see the comment on nextUpEvent in ics.go.
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

// DefaultNotifyMinutes is how far out an event counts as "imminent" when
// notify_minutes is not set: the same threshold as the face's own warn
// colour, so the panel switching to show you the event lines up with the
// point where the face itself starts looking urgent.
const DefaultNotifyMinutes = 15

// DefaultShowSeconds is how long the panel holds the calendar face after an
// auto-switch before returning to whatever it was showing, when
// show_seconds is not set.
const DefaultShowSeconds = 45

// Reading is what the calendar source publishes. An empty Title means no
// upcoming event, a normal state the face shows as idle rather than an
// error.
type Reading struct {
	Title string `json:"title"`
	// Location is whatever the calendar puts in the event's location field.
	// For an online meeting that is typically the conferencing platform's own
	// label, such as "Microsoft Teams Meeting", straight from the source
	// rather than anything this agent constructs.
	Location string `json:"location"`
	// StartLabel is preformatted, the same reasoning as clock's Time field:
	// the display device cannot be trusted to have the right locale.
	StartLabel string `json:"start_label"`
	// MinutesUntil lets the face tick the countdown down locally between
	// polls, the same pattern spotify's PositionMS uses for elapsed time.
	MinutesUntil float64 `json:"minutes_until"`
	// Urgent is true once the event is within notify_minutes. The panel uses
	// this to decide whether to auto-switch to the calendar face; the
	// threshold is decided here, server-side, rather than duplicated in the
	// client.
	Urgent bool `json:"urgent"`
	// ShowSeconds travels with the reading so the client knows how long to
	// hold the face open on an auto-switch without needing its own config.
	ShowSeconds int `json:"show_seconds"`
}

type settings struct {
	Mode string `json:"mode"`

	// Common to both modes.
	NotifyMinutes int `json:"notify_minutes"`
	ShowSeconds   int `json:"show_seconds"`

	// mode: "mock"
	Title          string `json:"title"`
	Location       string `json:"location"`
	StartInMinutes int    `json:"start_in_minutes"`

	// mode: "ics"
	FeedURL string `json:"feed_url"`
}

// Source reports the next-up event, either from a configured mock event or
// a real ICS feed.
type Source struct {
	interval      time.Duration
	notifyMinutes int
	showSeconds   int

	// mock mode
	mockTitle    string
	mockLocation string
	mockMinutes  int
	start        time.Time // computed once, on the first poll; see Poll

	// ics mode
	feedURL string
	http    interface {
		Do(req *http.Request) (*http.Response, error)
	}

	// now is overridden in tests.
	now func() time.Time
}

// New builds the calendar source: a mock that shows a configured sample
// event, or the real ICS feed provider.
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
	hasTitle := s.Title != ""
	hasStart := s.StartInMinutes != 0
	if hasTitle != hasStart {
		return nil, fmt.Errorf(`"title" and "start_in_minutes" must be set together, or both left out for no event`)
	}
	return &Source{
		interval:      interval,
		notifyMinutes: s.NotifyMinutes,
		showSeconds:   s.ShowSeconds,
		mockTitle:     s.Title,
		mockLocation:  s.Location,
		mockMinutes:   s.StartInMinutes,
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

// pollMock returns the configured sample event, its countdown derived from
// the clock rather than a stored start time, the same reasoning spotify's
// mock uses for track position: it advances and survives a reload without
// jumping back to the configured value.
func (s *Source) pollMock() Reading {
	if s.mockTitle == "" {
		return Reading{}
	}

	now := s.now()
	if s.start.IsZero() {
		s.start = now.Add(time.Duration(s.mockMinutes) * time.Minute)
	}
	return s.reading(s.mockTitle, s.mockLocation, s.start, now)
}

// pollICS fetches and parses the configured feed and picks the next-up
// event. Not connected, or a feed that cannot be fetched or parsed, is a
// real failure: there is nothing to publish. No qualifying event in an
// otherwise-good feed is success with an empty reading, the same as spotify
// treats "nothing currently playing".
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
	next, found := nextUpEvent(events, now)
	if !found {
		return Reading{}, nil
	}

	return s.reading(next.Summary, next.Location, next.Start, now), nil
}

// reading builds the published Reading from a title, location and start
// time, applying the notify threshold and hold duration common to both
// modes.
func (s *Source) reading(title, location string, start, now time.Time) Reading {
	minutesUntil := start.Sub(now).Minutes()
	return Reading{
		Title:        title,
		Location:     location,
		StartLabel:   start.Format("15:04"),
		MinutesUntil: minutesUntil,
		Urgent:       minutesUntil <= float64(s.notifyMinutes),
		ShowSeconds:  s.showSeconds,
	}
}
