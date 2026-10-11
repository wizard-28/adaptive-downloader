package downloader

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"adaptivedl/internal/assembler"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/rangetracker"
)

// workerPool runs a dynamically sized set of workers that pull ranges from a
// RangeTracker. The desired size (target) may change at any time through
// SetTarget; a manager goroutine reconciles the active worker count toward it.
//
// Workers are never killed mid-request. A worker only stops between ranges:
// when no work remains, when the run is aborted, or when the active count
// exceeds the target (draining).
type workerPool struct {
	d          *Downloader
	tracker    *rangetracker.Tracker
	asm        *assembler.Assembler
	state      *runState
	collector  *metrics.Collector
	reqCounter *atomic.Int64
	runner     *policyRunner

	mu     sync.Mutex
	target int
	active int
	wake   chan struct{}
	wg     sync.WaitGroup
}

func newWorkerPool(
	d *Downloader,
	tracker *rangetracker.Tracker,
	asm *assembler.Assembler,
	state *runState,
	collector *metrics.Collector,
	reqCounter *atomic.Int64,
	target int,
) *workerPool {
	return &workerPool{
		d:          d,
		tracker:    tracker,
		asm:        asm,
		state:      state,
		collector:  collector,
		reqCounter: reqCounter,
		target:     target,
		wake:       make(chan struct{}, 1),
	}
}

// signal wakes the manager without blocking. Redundant signals coalesce.
func (p *workerPool) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// SetTarget changes the desired worker count and wakes the manager.
func (p *workerPool) SetTarget(target int) {
	p.mu.Lock()
	p.target = target
	p.mu.Unlock()
	p.signal()
}

// run drives the pool until all work is complete or ctx is cancelled.
func (p *workerPool) run(ctx context.Context) {
	p.signal()
	for {
		select {
		case <-ctx.Done():
			p.wg.Wait()
			return
		case <-p.wake:
		}
		if p.reconcile(ctx) {
			return
		}
	}
}

// reconcile starts workers until the target is met or no pending work remains.
// It reports true when there is no work left and no active worker.
func (p *workerPool) reconcile(ctx context.Context) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.active == 0 && p.tracker.PendingBytes() == 0 {
		return true
	}
	// Only start workers when there is pending work, so an idle target never
	// causes a spawn/exit spin.
	for p.active < p.target && p.tracker.PendingBytes() > 0 {
		p.active++
		p.wg.Add(1)
		go p.worker(ctx)
	}
	return false
}

// overTarget reports whether this worker should retire to drain toward target.
func (p *workerPool) overTarget() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active > p.target
}

// worker claims and processes ranges until it should stop.
func (p *workerPool) worker(ctx context.Context) {
	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
		p.wg.Done()
		p.signal()
	}()

	for {
		if ctx.Err() != nil {
			return
		}
		if p.overTarget() {
			return
		}

		// Read the target chunk size at allocation time, so a change applies to
		// future ranges only. RangeTracker ranges are half-open [Start, End).
		r, ok := p.tracker.Allocate(p.d.currentChunkSize())
		if !ok {
			return
		}
		if !p.processRange(ctx, r) {
			return
		}
	}
}

// processRange fetches r and, on failure, retries the SAME range with its
// original boundaries until it succeeds or the retry limit is reached. The
// chunk-size target may change between retries, but a failed range is never
// resized or replaced by a differently-sized allocation.
//
// It returns false when the worker must stop (fatal error or cancellation).
func (p *workerPool) processRange(ctx context.Context, r rangetracker.Range) bool {
	// Inclusive bounds for fetcher, assembler, and metrics.
	end := r.End - 1

	for {
		attempt := p.state.recordAttempt(r.Start)
		if attempt > p.state.maxAttempts {
			p.state.fail(fmt.Errorf("%w: range [%d,%d] after %d attempts",
				ErrRetryLimitExceeded, r.Start, end, p.state.maxAttempts))
			return false
		}

		reqID := fmt.Sprintf("a%d-r%d", attempt, p.reqCounter.Add(1))
		res := p.d.fetcher.Fetch(ctx, reqID, p.d.cfg.URL, r.Start, end)

		// Record every attempt (success or failure) for observation only.
		rec := metrics.Record{
			RequestID:  res.RequestID,
			RangeStart: r.Start,
			RangeEnd:   end,
			Bytes:      res.Bytes,
			Duration:   res.Duration,
			Throughput: res.Throughput,
			Success:    res.Success,
			StatusCode: res.StatusCode,
			Attempt:    attempt,
		}
		p.collector.Record(rec)

		// A policy decision (if configured) runs after the attempt produced
		// metrics. Decisions are serialized by the runner.
		if p.runner != nil {
			p.runner.observe(rec)
		}

		if res.Success {
			if err := p.asm.WriteRange(r.Start, end, res.Data); err != nil {
				p.state.fail(fmt.Errorf("downloader: assemble [%d,%d]: %w", r.Start, end, err))
				return false
			}
			if err := p.tracker.Complete(r); err != nil {
				p.state.fail(fmt.Errorf("downloader: complete [%d,%d]: %w", r.Start, end, err))
				return false
			}
			return true
		}

		if ctx.Err() != nil {
			return false
		}
		// Retry the same range; boundaries are preserved even if the chunk
		// size target changed.
	}
}
