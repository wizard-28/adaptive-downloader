package downloader

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"adaptivedl/internal/assembler"
	"adaptivedl/internal/fetcher"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/rangetracker"
)

// workerPool runs a dynamically sized set of workers that pull ranges from a
// RangeTracker. The desired size (target) may change at any time through
// SetTarget; a manager goroutine reconciles the active worker count toward it.
//
// Workers are never killed mid-request. A worker only stops between ranges:
// when no work remains, when the run is aborted, or when the active count
// exceeds the target (draining). That is why a planned concurrency decrease
// costs at most one in-flight request per retiring worker, and never truncates
// a request that is already running.
//
// Control protocol: every state change calls signal(), which pokes a
// capacity-1 channel. run() wakes on that channel, calls reconcile(), and
// sleeps again. Signals coalesce (several changes between two wake-ups cause
// one reconcile), so the manager is edge-triggered but never misses a state
// that is still true when it looks: reconcile always reads the current target,
// active count and pending bytes under mu.
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

	// Opt-in straggler splitting (Config.SplitStragglers). splitMu guards
	// tasks and every task's r and lim; a split shrinks the victim's Limit,
	// splits it in the tracker and updates the victim's boundaries in one
	// critical section.
	splitMu sync.Mutex
	tasks   map[*task]struct{}
	splits  atomic.Int64
}

// task is one range being processed by a worker. Without splitting, r never
// changes. With splitting, another worker may shorten r (never below the
// bytes already received) and take the remainder as a new task.
type task struct {
	r   rangetracker.Range
	lim *fetcher.Limit // current attempt's limit; nil between attempts
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
		tasks:      make(map[*task]struct{}),
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
//
// Two conditions are deliberate:
//   - workers are started only while PendingBytes() > 0, so a target larger
//     than the remaining work does not spawn workers that would exit at once;
//   - "done" requires active == 0 as well as no pending bytes, so a worker that
//     is still finishing a request keeps the pool alive.
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
			if !p.d.cfg.SplitStragglers {
				return
			}
			t, ok := p.splitStraggler()
			if !ok {
				return
			}
			if !p.processTask(ctx, t) {
				return
			}
			continue
		}
		if !p.processRange(ctx, r) {
			return
		}
	}
}

// splitStraggler implements aria2-style dynamic segmentation. When nothing
// is pending, it takes the in-flight range with the most unreceived bytes
// and, if at least 2*SplitMinBytes remain, moves its end to the middle of
// the unreceived part and returns the second half as a new task. Bytes
// already received stay with the original request.
//
// The order of operations matters:
//  1. choose the victim and the split point at := start + received + rem/2;
//  2. shrink the live request to at-1 first, so the reader can never hand bytes
//     at or beyond `at` to the head request (Shrink refuses a point the reader
//     has already passed);
//  3. only then split the tracker entry and update the victim's boundaries.
//
// Example: range [0, 8 MiB) with 3 MiB received leaves rem = 5 MiB, so the
// split point is 3 MiB + 2.5 MiB = 5.5 MiB: the head keeps [0, 5.5 MiB) (its
// 3 MiB are already in hand) and the new task fetches [5.5 MiB, 8 MiB).
func (p *workerPool) splitStraggler() (*task, bool) {
	p.splitMu.Lock()
	defer p.splitMu.Unlock()

	var victim *task
	var victimRem, victimRecv int64
	for t := range p.tasks {
		if t.lim == nil {
			continue
		}
		recv, end := t.lim.Snapshot()
		if rem := end - (t.r.Start + recv) + 1; rem > victimRem {
			victim, victimRem, victimRecv = t, rem, recv
		}
	}
	if victim == nil || victimRem < 2*p.d.splitMinBytes() {
		return nil, false
	}
	at := victim.r.Start + victimRecv + victimRem/2
	if !victim.lim.Shrink(at - 1) {
		return nil, false // the reader passed the split point meanwhile
	}
	head, tail, err := p.tracker.Split(victim.r, at)
	if err != nil {
		p.state.fail(fmt.Errorf("downloader: split [%d,%d) at %d: %w", victim.r.Start, victim.r.End, at, err))
		return nil, false
	}
	victim.r = head
	t := &task{r: tail}
	p.tasks[t] = struct{}{}
	p.splits.Add(1)
	return t, true
}

// processRange is processTask for a freshly allocated range. See processTask
// for the retry and conversion rules.
func (p *workerPool) processRange(ctx context.Context, r rangetracker.Range) bool {
	return p.processTask(ctx, &task{r: r})
}

// processTask fetches a task's range and, on failure, retries the SAME range
// with its current boundaries until it succeeds or the retry limit is reached.
// The chunk-size target may change between retries, but a failed range is never
// resized or replaced by a differently-sized allocation. The one exception is
// opt-in straggler splitting, where another worker may shorten the range's end
// (never below the bytes already received); a retry then uses the shortened
// boundaries.
//
// Per attempt, in this order:
//   - convert the half-open tracker range to inclusive bounds: end = End-1;
//   - count the attempt against the range's start offset (bounded by
//     MaxRetries+1) and build the request id "a<attempt>-r<n>";
//   - fetch (with a fetcher.Limit when splitting is on);
//   - record exactly one metrics.Record, then run one policy decision;
//   - on success: verify the byte count, assemble, mark the tracker range
//     complete; on failure: retry, unless the run is being cancelled.
//
// It returns false when the worker must stop (fatal error or cancellation).
func (p *workerPool) processTask(ctx context.Context, t *task) bool {
	split := p.d.cfg.SplitStragglers
	if split {
		p.splitMu.Lock()
		p.tasks[t] = struct{}{}
		p.splitMu.Unlock()
		defer func() {
			p.splitMu.Lock()
			delete(p.tasks, t)
			p.splitMu.Unlock()
		}()
	}

	for {
		r := t.r
		if split {
			p.splitMu.Lock()
			r = t.r
			p.splitMu.Unlock()
		}
		// Inclusive bounds for fetcher, assembler, and metrics.
		end := r.End - 1

		attempt := p.state.recordAttempt(r.Start)
		if attempt > p.state.maxAttempts {
			p.state.fail(fmt.Errorf("%w: range [%d,%d] after %d attempts",
				ErrRetryLimitExceeded, r.Start, end, p.state.maxAttempts))
			return false
		}

		reqID := fmt.Sprintf("a%d-r%d", attempt, p.reqCounter.Add(1))
		var res fetcher.Result
		if split {
			lim := fetcher.NewLimit(r.Start, end)
			p.splitMu.Lock()
			t.lim = lim
			p.splitMu.Unlock()
			res = p.d.fetcher.FetchLimited(ctx, reqID, p.d.cfg.URL, r.Start, end, lim)
			p.splitMu.Lock()
			t.lim = nil
			r = t.r // may have been shortened during the fetch
			p.splitMu.Unlock()
			end = r.End - 1
		} else {
			res = p.d.fetcher.Fetch(ctx, reqID, p.d.cfg.URL, r.Start, end)
		}

		// Record every attempt (success or failure) for observation only.
		rec := metrics.Record{
			RequestID:  res.RequestID,
			RangeStart: r.Start,
			RangeEnd:   end,
			Bytes:      res.Bytes,
			Duration:   res.Duration,
			Latency:    res.Latency,
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
			if int64(len(res.Data)) != r.Length() {
				p.state.fail(fmt.Errorf("downloader: range [%d,%d] got %d bytes after split", r.Start, end, len(res.Data)))
				return false
			}
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
