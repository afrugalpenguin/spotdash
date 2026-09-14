package sources

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

// fakeSource is a Source whose behaviour each test dictates.
type fakeSource struct {
	name     string
	interval time.Duration

	mu    sync.Mutex
	calls int
	// poll is called with the number of times Poll has been entered, starting
	// at 1.
	poll func(ctx context.Context, call int) (any, error)
}

func (f *fakeSource) Name() string            { return f.name }
func (f *fakeSource) Interval() time.Duration { return f.interval }

func (f *fakeSource) Poll(ctx context.Context) (any, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	fn := f.poll
	f.mu.Unlock()
	return fn(ctx, call)
}

func (f *fakeSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func alwaysReturns(value any) func(context.Context, int) (any, error) {
	return func(context.Context, int) (any, error) { return value, nil }
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// eventually polls cond until it holds or the deadline passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// startRunner starts a runner and guarantees it is stopped when the test ends.
// Order matters: the context has to be cancelled before Wait is called, or Wait
// blocks forever.
func startRunner(t *testing.T, store *state.Store, srcs ...Source) *Runner {
	t.Helper()
	runner := NewRunner(store, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	runner.Start(ctx, srcs)
	t.Cleanup(func() {
		cancel()
		runner.Wait()
	})
	return runner
}

func TestRunnerWritesSuccessfulPollsToTheStore(t *testing.T) {
	store := state.New()
	store.Register("fake", state.StatusDegraded, "awaiting first poll")
	src := &fakeSource{name: "fake", interval: 10 * time.Millisecond, poll: alwaysReturns("a value")}

	startRunner(t, store, src)

	eventually(t, "the store to hold the polled value", func() bool {
		entry, ok := store.Get("fake")
		return ok && entry.Status == state.StatusOK && entry.Data == "a value"
	})

	entry, _ := store.Get("fake")
	if entry.LastError != "" {
		t.Errorf("LastError = %q, want it cleared after a successful poll", entry.LastError)
	}
}

func TestRunnerPollsRepeatedly(t *testing.T) {
	store := state.New()
	store.Register("fake", state.StatusDegraded, "")
	src := &fakeSource{name: "fake", interval: 5 * time.Millisecond, poll: alwaysReturns(1)}

	startRunner(t, store, src)

	eventually(t, "at least three polls", func() bool { return src.callCount() >= 3 })
}

func TestRunnerMarksFailingSourceDegraded(t *testing.T) {
	store := state.New()
	store.Register("fake", state.StatusDegraded, "")
	src := &fakeSource{
		name:     "fake",
		interval: 10 * time.Millisecond,
		poll: func(context.Context, int) (any, error) {
			return nil, errors.New("the dependency is gone")
		},
	}

	startRunner(t, store, src)

	eventually(t, "the source to be marked degraded", func() bool {
		entry, ok := store.Get("fake")
		return ok && entry.Status == state.StatusDegraded && strings.Contains(entry.LastError, "the dependency is gone")
	})
}

func TestRunnerRecoversFromPanic(t *testing.T) {
	store := state.New()
	store.Register("fake", state.StatusDegraded, "")
	src := &fakeSource{
		name:     "fake",
		interval: 10 * time.Millisecond,
		poll: func(_ context.Context, call int) (any, error) {
			if call == 1 {
				panic("source exploded")
			}
			return "recovered value", nil
		},
	}

	startRunner(t, store, src)

	// The panic is recorded as an error.
	eventually(t, "the panic to be recorded", func() bool {
		entry, ok := store.Get("fake")
		return ok && strings.Contains(entry.LastError, "source exploded")
	})
	// And the source keeps running afterwards.
	eventually(t, "the source to recover on a later poll", func() bool {
		entry, ok := store.Get("fake")
		return ok && entry.Status == state.StatusOK && entry.Data == "recovered value"
	})
}

func TestOneFailingSourceDoesNotAffectAnother(t *testing.T) {
	store := state.New()
	store.Register("broken", state.StatusDegraded, "")
	store.Register("healthy", state.StatusDegraded, "")

	broken := &fakeSource{
		name:     "broken",
		interval: 10 * time.Millisecond,
		poll:     func(context.Context, int) (any, error) { panic("still broken") },
	}
	healthy := &fakeSource{name: "healthy", interval: 10 * time.Millisecond, poll: alwaysReturns("fine")}

	startRunner(t, store, broken, healthy)

	eventually(t, "the healthy source to report ok", func() bool {
		entry, ok := store.Get("healthy")
		return ok && entry.Status == state.StatusOK
	})

	entry, _ := store.Get("broken")
	if entry.Status != state.StatusDegraded {
		t.Errorf("broken source status = %q, want degraded", entry.Status)
	}
}

func TestRunnerStopsOnContextCancel(t *testing.T) {
	store := state.New()
	store.Register("fake", state.StatusDegraded, "")
	src := &fakeSource{name: "fake", interval: 5 * time.Millisecond, poll: alwaysReturns(1)}

	runner := NewRunner(store, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	runner.Start(ctx, []Source{src})

	eventually(t, "the first poll", func() bool { return src.callCount() >= 1 })
	cancel()

	done := make(chan struct{})
	go func() {
		runner.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after the context was cancelled")
	}

	// No further polls once Wait has returned.
	settled := src.callCount()
	time.Sleep(30 * time.Millisecond)
	if got := src.callCount(); got != settled {
		t.Errorf("source polled %d more times after shutdown", got-settled)
	}
}

func TestPollRunsUnderADeadline(t *testing.T) {
	store := state.New()
	store.Register("slow", state.StatusDegraded, "")

	deadlines := make(chan bool, 1)
	src := &fakeSource{
		name:     "slow",
		interval: 10 * time.Millisecond,
		poll: func(ctx context.Context, call int) (any, error) {
			if call == 1 {
				_, ok := ctx.Deadline()
				deadlines <- ok
			}
			return "value", nil
		},
	}

	startRunner(t, store, src)

	select {
	case hasDeadline := <-deadlines:
		if !hasDeadline {
			t.Error("Poll should receive a context with a deadline so a wedged source cannot hang forever")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the source was never polled")
	}
}

func TestBackoffGrowsWithConsecutiveFailuresAndIsCapped(t *testing.T) {
	interval := 100 * time.Millisecond

	if got := backoffDelay(interval, 0); got != interval {
		t.Errorf("with no failures the delay should be the plain interval, got %v", got)
	}

	first := backoffDelay(interval, 1)
	second := backoffDelay(interval, 2)
	third := backoffDelay(interval, 3)
	if !(first < second && second < third) {
		t.Errorf("delay should grow with consecutive failures, got %v, %v, %v", first, second, third)
	}

	// A source whose dependency is gone for good must settle at the ceiling
	// rather than growing without bound.
	if got := backoffDelay(interval, 100); got != maxBackoff {
		t.Errorf("backoffDelay with many failures = %v, want the ceiling %v", got, maxBackoff)
	}
	if got := backoffDelay(interval, 1000); got != maxBackoff {
		t.Errorf("backoff should stay capped and must not overflow, got %v", got)
	}
}

func TestBackoffNeverShortensTheInterval(t *testing.T) {
	// A source configured to poll slowly must not be polled faster just because
	// it is failing.
	interval := 2 * maxBackoff

	if got := backoffDelay(interval, 5); got < interval {
		t.Errorf("backoffDelay = %v, want at least the configured interval %v", got, interval)
	}
}

func TestFailingSourceDoesNotSpin(t *testing.T) {
	store := state.New()
	store.Register("fake", state.StatusDegraded, "")
	src := &fakeSource{
		name:     "fake",
		interval: 5 * time.Millisecond,
		poll: func(context.Context, int) (any, error) {
			return nil, errors.New("still broken")
		},
	}

	startRunner(t, store, src)

	eventually(t, "the first few failures", func() bool { return src.callCount() >= 3 })
	after := src.callCount()
	time.Sleep(100 * time.Millisecond)

	// Without backoff a 5ms interval would produce roughly 20 more polls in
	// 100ms. With backoff it should be a small handful.
	if extra := src.callCount() - after; extra > 5 {
		t.Errorf("a failing source polled %d more times in 100ms, backoff is not being applied", extra)
	}
}

func TestPollNowSkipsTheWaitForTheNextTick(t *testing.T) {
	// This is what a control action needs: the reading a user just changed
	// (skip, pause) has to reach the panel immediately, not after however long
	// is left on a slow interval. Waiting up to the interval after a tap reads
	// as the tap not having worked.
	store := state.New()
	store.Register("fake", state.StatusDegraded, "")
	calls := make(chan struct{}, 10)
	src := &fakeSource{
		name:     "fake",
		interval: time.Hour, // long enough that only PollNow could trigger a second poll in this test
		poll: func(context.Context, int) (any, error) {
			calls <- struct{}{}
			return "reading", nil
		},
	}

	runner := startRunner(t, store, src)

	<-calls // the immediate poll on start
	runner.PollNow("fake")

	select {
	case <-calls:
		// A second poll arrived without waiting anywhere near the hour interval.
	case <-time.After(2 * time.Second):
		t.Fatal("PollNow did not trigger an immediate second poll")
	}
}

func TestPollNowOnAnUnknownSourceIsANoOp(t *testing.T) {
	store := state.New()
	runner := NewRunner(store, discardLogger())

	// Must not panic or block when nothing by that name is running.
	runner.PollNow("does-not-exist")
}

func TestPollNowCoalescesRapidCalls(t *testing.T) {
	// Several taps in quick succession should not queue up a poll per tap; one
	// pending immediate poll is enough; the rest are redundant.
	store := state.New()
	store.Register("fake", state.StatusDegraded, "")
	calls := make(chan struct{}, 20)
	src := &fakeSource{
		name:     "fake",
		interval: time.Hour,
		poll: func(context.Context, int) (any, error) {
			select {
			case calls <- struct{}{}:
			default:
			}
			return "reading", nil
		},
	}

	runner := startRunner(t, store, src)
	<-calls // drain the immediate poll on start

	for i := 0; i < 5; i++ {
		runner.PollNow("fake")
	}

	// Give it a moment to process, then confirm it did not spin.
	time.Sleep(200 * time.Millisecond)
	if got := src.callCount(); got > 4 {
		t.Errorf("callCount = %d after 5 rapid PollNow calls, want a small, bounded number", got)
	}
}
