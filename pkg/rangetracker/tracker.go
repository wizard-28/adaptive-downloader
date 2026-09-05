package rangetracker

import (
	"errors"
	"fmt"
	"sync"
)

type Tracker struct {
	mu             sync.Mutex
	totalSize      int64
	pending        []Interval
	inFlight       map[string]Interval
	completedBytes int64
}

// New creates a Tracker for a file containing totalSize bytes.
// totalSize must be positive.
func New(totalSize int64) (*Tracker, error) {
	if totalSize <= 0 {
		return nil, errors.New("total size must be positive")
	}

	return &Tracker{
		totalSize: totalSize,
		pending:   []Interval{{Start: 0, End: totalSize - 1}},
		inFlight:  make(map[string]Interval),
	}, nil
}

var (
	ErrRequestNotFound        = errors.New("request ID not found in in-flight registry")
	ErrInvalidBytesDownloaded = errors.New("bytes downloaded cannot be negative")
)

// NextRange allocates up to chunkSize bytes from the front of the pending queue,
// assigns the interval to reqID in inFlight, and updates the pending set.
//
// If no pending intervals remain, it returns an empty Interval and false. If the
// available contiguous range is smaller than chunkSize, it allocates the full
// available range rather than bridging across non-contiguous pending intervals.
// Each request ID is associated with exactly one contiguous interval.
func (t *Tracker) NextRange(reqID string, chunkSize int64) (Interval, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if chunkSize <= 0 {
		panic("rangetracker: chunkSize must be positive")
	}

	if _, exists := t.inFlight[reqID]; exists {
		panic("rangetracker: duplicate request ID in-flight: " + reqID)
	}

	if len(t.pending) == 0 {
		return Interval{}, false
	}
	first := &t.pending[0]

	if first.Len() <= chunkSize {
		t.pending = t.pending[1:]
		t.inFlight[reqID] = *first
		return *first, true
	}

	allocated := Interval{
		Start: first.Start,
		End:   first.Start + chunkSize - 1,
	}

	first.Start += chunkSize
	t.inFlight[reqID] = allocated

	return allocated, true
}

// MarkComplete marks a reqID as completed.
func (t *Tracker) MarkComplete(reqID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	interval, exists := t.inFlight[reqID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrRequestNotFound, reqID)
	}

	delete(t.inFlight, reqID)
	t.completedBytes += interval.Len()

	return nil
}

// Requeue inserts a completely failed request back into the pending queue.
func (t *Tracker) Requeue(reqID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	interval, exists := t.inFlight[reqID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrRequestNotFound, reqID)
	}

	delete(t.inFlight, reqID)
	t.addPendingAndCoalesce(interval)

	return nil
}

// RequeuePartial records bytesDownloaded from the beginning of the
// in-flight interval as completed and requeues the remaining suffix.
func (t *Tracker) RequeuePartial(reqID string, bytesDownloaded int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	interval, exists := t.inFlight[reqID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrRequestNotFound, reqID)
	}

	if bytesDownloaded < 0 {
		return ErrInvalidBytesDownloaded
	}

	delete(t.inFlight, reqID)

	if bytesDownloaded == 0 {
		t.addPendingAndCoalesce(interval)
		return nil
	}

	if bytesDownloaded >= interval.Len() {
		t.completedBytes += interval.Len()
		return nil
	}

	t.completedBytes += bytesDownloaded

	remaining := Interval{
		Start: interval.Start + bytesDownloaded,
		End:   interval.End,
	}

	t.addPendingAndCoalesce(remaining)

	return nil
}

// addPendingAndCoalesce inserts an interval into pending and merges adjacent ranges.
// MUST be called with t.mu held by the caller.
func (t *Tracker) addPendingAndCoalesce(interval Interval) {
	// Insert into t.pending sorted by Start byte.
	inserted := false

	for i, p := range t.pending {
		if interval.Start < p.Start {
			t.pending = append(t.pending, Interval{})
			copy(t.pending[i+1:], t.pending[i:])
			t.pending[i] = interval
			inserted = true
			break
		}
	}

	if !inserted {
		t.pending = append(t.pending, interval)
	}

	// Single-pass merge.
	merged := make([]Interval, 0, len(t.pending))

	for _, current := range t.pending {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}

		last := &merged[len(merged)-1]

		// Merge overlapping or directly adjacent intervals.
		if current.Start <= last.End+1 {
			if current.End > last.End {
				last.End = current.End
			}
		} else {
			// Keep disjoint intervals separate.
			merged = append(merged, current)
		}
	}

	t.pending = merged
}

// IsComplete returns true if all file bytes have been marked complete.
func (t *Tracker) IsComplete() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.completedBytes == t.totalSize
}

// Progress returns completed bytes and total file size for progress reporting.
func (t *Tracker) Progress() (int64, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.completedBytes, t.totalSize
}
