package sources

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/internal/state"
)

const (
	// maxBackoff caps the delay after repeated failures. A source keeps
	// checking occasionally and never gives up.
	maxBackoff = 30 * time.Second
	// minPollTimeout floors the per-poll deadline for fast intervals.
	minPollTimeout = 2 * time.Second
)

// Runner owns one goroutine per source.
type Runner struct {
	store *state.Store
	log   *slog.Logger
	wg    sync.WaitGroup

	mu     sync.Mutex
	repoll map[string]chan struct{}
}

// NewRunner returns a runner that writes into store.
func NewRunner(store *state.Store, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{store: store, log: log, repoll: make(map[string]chan struct{})}
}

// Start launches one goroutine per source. It returns immediately. Cancelling
// ctx stops every source; Wait blocks until they have all stopped.
func (r *Runner) Start(ctx context.Context, srcs []Source) {
	for _, src := range srcs {
		// Buffered by one: a pending request is enough, extra sends are dropped.
		ch := make(chan struct{}, 1)
		r.mu.Lock()
		r.repoll[src.Name()] = ch
		r.mu.Unlock()

		r.wg.Add(1)
		go func(src Source, repoll <-chan struct{}) {
			defer r.wg.Done()
			r.loop(ctx, src, repoll)
		}(src, ch)
	}
}

// PollNow asks the named source to poll again immediately. It is a no-op if
// the name is not running, and safe to call from any goroutine.
func (r *Runner) PollNow(name string) {
	r.mu.Lock()
	ch, ok := r.repoll[name]
	r.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
		// A request is already pending; this one is redundant.
	}
}

// Wait blocks until every source goroutine has returned.
func (r *Runner) Wait() {
	r.wg.Wait()
}

func (r *Runner) loop(ctx context.Context, src Source, repoll <-chan struct{}) {
	name := src.Name()
	interval := src.Interval()
	failures := 0

	r.log.Debug("source started", "source", name, "interval", interval)

	for {
		// Poll first, so the panel has data without waiting an interval.
		if err := r.pollOnce(ctx, src); err != nil {
			failures++
		} else {
			failures = 0
		}

		delay := backoffDelay(interval, failures)
		if failures > 0 {
			r.log.Debug("source backing off", "source", name, "consecutive_failures", failures, "delay", delay)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			r.log.Debug("source stopped", "source", name)
			return
		case <-repoll:
			// The interval restarts here, so a burst of taps cannot bunch polls
			// closer than the configured interval.
			timer.Stop()
			r.log.Debug("source polling on request", "source", name)
		case <-timer.C:
		}
	}
}

// pollOnce runs one poll under a deadline, converting a panic into an error,
// and records the outcome.
func (r *Runner) pollOnce(ctx context.Context, src Source) (err error) {
	name := src.Name()
	started := time.Now()

	pollCtx, cancel := context.WithTimeout(ctx, pollTimeout(src.Interval()))
	defer cancel()

	var value any
	func() {
		// A panic in one source must not take down the process or the others.
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("panic: %v", recovered)
			}
		}()
		value, err = src.Poll(pollCtx)
	}()

	elapsed := time.Since(started)

	// A partial result is stored and broadcast and does not count toward
	// backoff. See docs/architecture.md, "A third outcome: partial results".
	if err != nil && IsPartial(err) && value != nil {
		r.store.UpdatePartial(name, value, errors.Unwrap(err))
		r.log.Debug("source poll partial", "source", name, "duration", elapsed, "reason", errors.Unwrap(err))
		return nil
	}

	if err != nil {
		r.store.Fail(name, err)
		r.log.Debug("source poll failed", "source", name, "duration", elapsed, "error", err)
		return err
	}

	r.store.Update(name, value)
	r.log.Debug("source poll ok", "source", name, "duration", elapsed)
	return nil
}

// pollTimeout bounds a single poll so a wedged source cannot hang its goroutine.
func pollTimeout(interval time.Duration) time.Duration {
	if interval < minPollTimeout {
		return minPollTimeout
	}
	return interval
}

// backoffDelay returns the wait before the next poll: the interval, doubled per
// consecutive failure up to maxBackoff, and never below the interval.
func backoffDelay(interval time.Duration, consecutiveFailures int) time.Duration {
	if consecutiveFailures <= 0 {
		return interval
	}

	delay := interval
	for i := 0; i < consecutiveFailures; i++ {
		delay *= 2
		if delay >= maxBackoff {
			// Stop early to avoid overflow on a long run of failures.
			delay = maxBackoff
			break
		}
	}
	if delay < interval {
		return interval
	}
	return delay
}
