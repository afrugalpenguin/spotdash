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
	// maxBackoff caps the delay after repeated failures. A source whose
	// dependency has gone away should keep checking occasionally, not spin and
	// not give up.
	maxBackoff = 30 * time.Second
	// minPollTimeout floors the per-poll deadline so that a fast interval does
	// not cancel a poll that was always going to take a moment.
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
		// Buffered by one: a pending request is enough. Several rapid calls
		// while one is already waiting to be picked up are redundant, not
		// queued work, so the extra sends are dropped rather than piling up.
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

// PollNow asks the named source to poll again immediately, without waiting
// for its next scheduled tick.
//
// This is what a control action needs: a track skipped or paused from the
// panel has to show up right away, not after however long is left on the
// source's normal interval. A no-op if the name is not running, and safe to
// call from any goroutine.
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
		// Poll immediately on start so the panel has data without waiting a
		// whole interval for it.
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
			// The interval restarts from here rather than the request merely
			// skipping the rest of the current wait, so a burst of taps cannot
			// bunch polls closer together than the source is configured for.
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
		// A source is third party code as far as the agent is concerned. A
		// panic in one must not take the process, or any other source, down.
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("panic: %v", recovered)
			}
		}()
		value, err = src.Poll(pollCtx)
	}()

	elapsed := time.Since(started)

	// A partial result is a working source with something worth publishing, so
	// it is stored and broadcast, and it is not a failure for backoff: nothing
	// is failing, and polling less often would not bring the missing part back.
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

// pollTimeout bounds a single poll so a wedged source cannot hang its goroutine
// forever.
func pollTimeout(interval time.Duration) time.Duration {
	if interval < minPollTimeout {
		return minPollTimeout
	}
	return interval
}

// backoffDelay returns how long to wait before the next poll. With no failures
// that is the configured interval. Consecutive failures double it up to
// maxBackoff, and it never drops below the configured interval.
func backoffDelay(interval time.Duration, consecutiveFailures int) time.Duration {
	if consecutiveFailures <= 0 {
		return interval
	}

	delay := interval
	for i := 0; i < consecutiveFailures; i++ {
		delay *= 2
		if delay >= maxBackoff {
			// Stop early so a long run of failures cannot overflow.
			delay = maxBackoff
			break
		}
	}
	if delay < interval {
		return interval
	}
	return delay
}
