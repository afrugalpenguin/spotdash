// Package state holds the latest value and status for every source.
//
// It is the single source of truth behind both /health and the WebSocket, so
// the debug view and the live view can never disagree.
package state

import (
	"sort"
	"sync"
	"time"
)

// Status is the health of a single source.
type Status string

const (
	// StatusOK means the last poll succeeded.
	StatusOK Status = "ok"
	// StatusDegraded means the source is enabled but its last poll failed, or
	// it is running with reduced capability.
	StatusDegraded Status = "degraded"
	// StatusDisabled means the source is turned off in config.
	StatusDisabled Status = "disabled"
)

// Entry is everything known about one source.
type Entry struct {
	Source    string
	Status    Status
	UpdatedAt time.Time
	LastError string
	Data      any
}

// Store is a concurrency-safe map of source name to Entry.
type Store struct {
	mu          sync.RWMutex
	entries     map[string]*Entry
	subscribers map[*subscriber]struct{}
	now         func() time.Time
}

// New returns an empty store.
func New() *Store {
	return &Store{
		entries:     make(map[string]*Entry),
		subscribers: make(map[*subscriber]struct{}),
		now:         time.Now,
	}
}

// Register declares a source and its starting status. Only registered sources
// can be updated, so a stray write cannot invent a source that /health would
// then report on.
func (s *Store) Register(name string, status Status, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[name] = &Entry{
		Source:    name,
		Status:    status,
		LastError: lastError,
	}
}

// Update records a successful poll. The source becomes ok and its last error
// is cleared.
func (s *Store) Update(name string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[name]
	if !ok {
		return
	}
	entry.Status = StatusOK
	entry.LastError = ""
	entry.Data = data
	entry.UpdatedAt = s.now()

	// Broadcast under the same lock that wrote the entry, so a subscriber can
	// never observe an update out of order with the stored value.
	s.broadcastLocked(*entry)
}

// Fail records a failed poll. The source becomes degraded and keeps its last
// good data, because a reading labelled stale beats a blank panel.
func (s *Store) Fail(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[name]
	if !ok {
		return
	}
	entry.Status = StatusDegraded
	if err != nil {
		entry.LastError = err.Error()
	}
}

// Get returns a copy of one entry.
func (s *Store) Get(name string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[name]
	if !ok {
		return Entry{}, false
	}
	return *entry, true
}

// Snapshot returns a copy of every entry, sorted by source name so that output
// order is stable between runs.
func (s *Store) Snapshot() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// UpdatePartial records a poll that produced a usable value while running with
// reduced capability. The value is stored and broadcast as a fresh reading, and
// the source is marked degraded with the reason attached.
//
// Telemetry on a machine where NVML is unavailable is the case this exists for:
// CPU, RAM and disk are genuinely there and worth showing, and the source is
// genuinely degraded. Treating that as a failure would throw away a good
// reading; treating it as a success would hide a real problem.
func (s *Store) UpdatePartial(name string, data any, reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[name]
	if !ok {
		return
	}
	entry.Status = StatusDegraded
	entry.Data = data
	entry.UpdatedAt = s.now()
	if reason != nil {
		entry.LastError = reason.Error()
	}
	s.broadcastLocked(*entry)
}
