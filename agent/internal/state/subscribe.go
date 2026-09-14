package state

// subscriberBuffer is how far behind a client may fall before it is dropped.
//
// Sized for a brief stall, a garbage collection pause or a frame the device
// spent elsewhere, not for a client that has stopped reading. Once it is full
// the subscriber is dropped so it reconnects and receives a fresh snapshot,
// which is better than quietly skipping messages and leaving it stale with no
// way to know.
const subscriberBuffer = 32

type subscriber struct {
	ch     chan Entry
	closed bool
}

// Subscribe returns a channel of source updates and a function that stops the
// subscription. The channel is closed when the subscription ends, whether that
// is because cancel was called or because the subscriber fell too far behind.
func (s *Store) Subscribe() (<-chan Entry, func()) {
	sub := &subscriber{ch: make(chan Entry, subscriberBuffer)}

	s.mu.Lock()
	if s.subscribers == nil {
		s.subscribers = make(map[*subscriber]struct{})
	}
	s.subscribers[sub] = struct{}{}
	s.mu.Unlock()

	var once bool
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if once {
			return
		}
		once = true
		s.removeLocked(sub)
	}
	return sub.ch, cancel
}

// removeLocked drops a subscriber and closes its channel. The caller holds the
// lock.
func (s *Store) removeLocked(sub *subscriber) {
	if _, present := s.subscribers[sub]; !present {
		return
	}
	delete(s.subscribers, sub)
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
}

// broadcastLocked delivers an entry to every subscriber without blocking. The
// caller holds the lock.
//
// A source goroutine calls this, so it must never wait on a client. A
// subscriber whose buffer is full is dropped instead.
func (s *Store) broadcastLocked(entry Entry) {
	for sub := range s.subscribers {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- entry:
		default:
			s.removeLocked(sub)
		}
	}
}
