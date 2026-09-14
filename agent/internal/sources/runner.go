package sources

import (
	"context"
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
}

// NewRunner returns a runner that writes into store.
func NewRunner(store *state.Store, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{store: store, log: log}
}

// Start launches one goroutine per source. It returns immediately. Cancelling
// ctx stops every source; Wait blocks until they have all stopped.
func (r *Runner) Start(ctx context.Context, srcs []Source) {
	for _, src := range srcs {
		r.wg.Add(1)
		go func(src Source) {
			defer r.wg.Done()
			r.loop(ctx, src)
		}(src)
	}
}

// Wait blocks until every source goroutine has returned.
func (r *Runner) Wait() {
	r.wg.Wait()
}

func (r *Runner) loop(ctx context.Context, src Source) {
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
