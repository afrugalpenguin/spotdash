package state

import (
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan Entry, what string) Entry {
	t.Helper()
	select {
	case entry, open := <-ch:
		if !open {
			t.Fatalf("channel closed while waiting for %s", what)
		}
		return entry
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return Entry{}
	}
}

func TestSubscriberReceivesUpdates(t *testing.T) {
	s := New()
	s.Register("clock", StatusDegraded, "")
	events, cancel := s.Subscribe()
	defer cancel()

	s.Update("clock", "a reading")

	entry := receive(t, events, "the update")
	if entry.Source != "clock" {
		t.Errorf("Source = %q, want clock", entry.Source)
	}
	if entry.Data != "a reading" {
		t.Errorf("Data = %v, want the polled value", entry.Data)
	}
	if entry.UpdatedAt.IsZero() {
		t.Error("the event should carry the update time")
	}
}

func TestEverySubscriberReceivesTheSameUpdate(t *testing.T) {
	s := New()
	s.Register("clock", StatusDegraded, "")
	first, cancelFirst := s.Subscribe()
	defer cancelFirst()
	second, cancelSecond := s.Subscribe()
	defer cancelSecond()

	s.Update("clock", "shared")

	if got := receive(t, first, "the first subscriber"); got.Data != "shared" {
		t.Errorf("first subscriber got %v", got.Data)
	}
	if got := receive(t, second, "the second subscriber"); got.Data != "shared" {
		t.Errorf("second subscriber got %v", got.Data)
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	s := New()
	s.Register("clock", StatusDegraded, "")
	events, cancel := s.Subscribe()

	cancel()
	s.Update("clock", "after cancel")

	select {
	case _, open := <-events:
		if open {
			t.Error("a cancelled subscriber should not receive updates")
		}
	case <-time.After(100 * time.Millisecond):
		// Closed or silent are both acceptable. Still delivering is not.
	}
}

func TestCancelIsSafeToCallTwice(t *testing.T) {
	s := New()
	_, cancel := s.Subscribe()

	cancel()
	cancel()
}

func TestUpdateDoesNotBlockOnASubscriberThatNeverReads(t *testing.T) {
	// This is the property that matters most. A panel that stops reading, or a
	// device that goes to sleep mid-frame, must not be able to stall every
	// source in the agent.
	s := New()
	s.Register("clock", StatusDegraded, "")
	_, cancel := s.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			s.Update("clock", i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Update blocked on a subscriber that never reads")
	}
}

func TestASubscriberThatFallsBehindIsDropped(t *testing.T) {
	// Dropping is better than silently skipping messages: the client
	// reconnects and is handed a fresh snapshot, so it is never quietly stale.
	s := New()
	s.Register("clock", StatusDegraded, "")
	events, cancel := s.Subscribe()
	defer cancel()

	for i := 0; i < 500; i++ {
		s.Update("clock", i)
	}

	// Drain whatever was buffered; the channel must end up closed.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("a subscriber that fell behind was never dropped")
		}
	}
}

func TestFailDoesNotBroadcast(t *testing.T) {
	// The message shape carries readings. A failure changes status, which the
	// client reads from /health, and leaves the last good reading in place.
	s := New()
	s.Register("clock", StatusDegraded, "")
	events, cancel := s.Subscribe()
	defer cancel()

	s.Fail("clock", errTest)

	select {
	case entry := <-events:
		t.Errorf("Fail broadcast an event: %+v", entry)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUpdateToAnUnregisteredSourceDoesNotBroadcast(t *testing.T) {
	s := New()
	events, cancel := s.Subscribe()
	defer cancel()

	s.Update("ghost", "data")

	select {
	case entry := <-events:
		t.Errorf("an unregistered source was broadcast: %+v", entry)
	case <-time.After(100 * time.Millisecond):
	}
}
