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
			t.Fatalf("channel closed waiting for %s", what)
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
		t.Error("event UpdatedAt is zero, want a time")
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
		t.Errorf("first subscriber Data = %v", got.Data)
	}
	if got := receive(t, second, "the second subscriber"); got.Data != "shared" {
		t.Errorf("second subscriber Data = %v", got.Data)
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
			t.Error("cancelled subscriber received an update")
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
	// A panel that stops reading must not stall every source.
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
	// A dropped client reconnects for a fresh snapshot.
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
			t.Fatal("slow subscriber not dropped")
		}
	}
}

func TestFailDoesNotBroadcast(t *testing.T) {
	// A failure changes status, which the client reads from /health.
	s := New()
	s.Register("clock", StatusDegraded, "")
	events, cancel := s.Subscribe()
	defer cancel()

	s.Fail("clock", errTest)

	select {
	case entry := <-events:
		t.Errorf("Fail broadcast %+v, want nothing", entry)
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
		t.Errorf("unregistered source broadcast %+v, want nothing", entry)
	case <-time.After(100 * time.Millisecond):
	}
}
