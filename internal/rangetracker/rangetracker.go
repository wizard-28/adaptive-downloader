// Package rangetracker tracks how a single file's byte space is partitioned
// into contiguous chunks and the lifecycle of each chunk as it moves through
// the pending, in-flight, and completed states.
//
// A Tracker is safe for concurrent use by multiple goroutines.
package rangetracker

import (
	"errors"
	"sort"
	"sync"
)

// ErrRangeNotInFlight is returned when a range that is not currently in
// flight is completed or failed.
var ErrRangeNotInFlight = errors.New("rangetracker: range is not in flight")

// Range is a half-open byte interval [Start, End). End is exclusive, so
// Length is End-Start.
type Range struct {
	Start int64
	End   int64
}

// Length returns the number of bytes in the range.
func (r Range) Length() int64 {
	if r.End <= r.Start {
		return 0
	}
	return r.End - r.Start
}

// Empty reports whether the range covers no bytes.
func (r Range) Empty() bool { return r.End <= r.Start }

// Tracker tracks the allocation state of one file.
//
// Invariants, maintained under mu:
//   - pending is sorted by Start, and its ranges are disjoint and non-adjacent
//     (adjacent ranges are merged).
//   - inFlight holds ranges that were allocated but not yet completed or
//     failed; they are disjoint from pending and from each other.
//   - completed is sorted by Start with adjacent/overlapping ranges merged.
//   - completedBytes equals the sum of the lengths in completed.
type Tracker struct {
	mu             sync.Mutex
	totalSize      int64
	pending        []Range
	inFlight       map[Range]struct{}
	completed      []Range
	completedBytes int64
}

// New returns a Tracker for a file of totalSize bytes. A negative totalSize
// panics, since it indicates a programming error.
func New(totalSize int64) *Tracker {
	if totalSize < 0 {
		panic("rangetracker: negative total size")
	}
	t := &Tracker{
		totalSize: totalSize,
		inFlight:  make(map[Range]struct{}),
	}
	if totalSize > 0 {
		t.pending = []Range{{Start: 0, End: totalSize}}
	}
	return t
}

// TotalSize returns the total number of bytes in the tracked file. It is
// immutable and therefore safe to read without synchronization.
func (t *Tracker) TotalSize() int64 { return t.totalSize }

// Allocate carves up to chunkSize contiguous bytes from the front of the
// pending set and marks them in flight. Different calls may use different
// chunk sizes.
//
// It returns the allocated range and true on success. It returns a zero Range
// and false when nothing is pending or chunkSize is not positive. Allocated
// ranges never overlap and never extend beyond TotalSize.
func (t *Tracker) Allocate(chunkSize int64) (Range, bool) {
	if chunkSize <= 0 {
		return Range{}, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.pending) == 0 {
		return Range{}, false
	}

	first := t.pending[0]
	n := chunkSize
	if n > first.Length() {
		n = first.Length()
	}
	alloc := Range{Start: first.Start, End: first.Start + n}

	if n == first.Length() {
		t.pending = t.pending[1:]
	} else {
		t.pending[0].Start = first.Start + n
	}

	t.inFlight[alloc] = struct{}{}
	return alloc, true
}

// Complete marks an in-flight range as completed and records its bytes.
// It returns ErrRangeNotInFlight if r is not currently in flight.
func (t *Tracker) Complete(r Range) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.inFlight[r]; !ok {
		return ErrRangeNotInFlight
	}
	delete(t.inFlight, r)
	t.completed = mergeRange(t.completed, r)
	t.completedBytes += r.Length()
	return nil
}

// Fail returns an in-flight range to the pending set so it can be retried.
// The range is merged with any adjacent pending ranges. It returns
// ErrRangeNotInFlight if r is not currently in flight.
func (t *Tracker) Fail(r Range) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.inFlight[r]; !ok {
		return ErrRangeNotInFlight
	}
	delete(t.inFlight, r)
	t.pending = mergeRange(t.pending, r)
	return nil
}

// Done reports whether there is no pending or in-flight work.
func (t *Tracker) Done() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending) == 0 && len(t.inFlight) == 0
}

// Pending returns a copy of the pending ranges, sorted by Start.
func (t *Tracker) Pending() []Range {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Range(nil), t.pending...)
}

// InFlight returns a copy of the in-flight ranges, sorted by Start.
func (t *Tracker) InFlight() []Range {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Range, 0, len(t.inFlight))
	for r := range t.inFlight {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// Completed returns a copy of the completed ranges, sorted by Start with
// adjacent and overlapping ranges merged.
func (t *Tracker) Completed() []Range {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Range(nil), t.completed...)
}

// CompletedBytes returns the total number of completed bytes.
func (t *Tracker) CompletedBytes() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.completedBytes
}

// PendingBytes returns the total number of pending bytes.
func (t *Tracker) PendingBytes() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return sumLengths(t.pending)
}

// InFlightBytes returns the total number of in-flight bytes.
func (t *Tracker) InFlightBytes() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var n int64
	for r := range t.inFlight {
		n += r.Length()
	}
	return n
}

// mergeRange inserts r into ranges, keeping the result sorted by Start and
// merging overlapping or adjacent ranges. ranges must already be sorted,
// disjoint, and non-adjacent.
func mergeRange(ranges []Range, r Range) []Range {
	if r.Empty() {
		return ranges
	}

	out := make([]Range, 0, len(ranges)+1)
	inserted := false
	for _, cur := range ranges {
		switch {
		case cur.End < r.Start:
			out = append(out, cur)
		case cur.Start > r.End:
			if !inserted {
				out = append(out, r)
				inserted = true
			}
			out = append(out, cur)
		default:
			if cur.Start < r.Start {
				r.Start = cur.Start
			}
			if cur.End > r.End {
				r.End = cur.End
			}
		}
	}
	if !inserted {
		out = append(out, r)
	}
	return out
}

func sumLengths(ranges []Range) int64 {
	var n int64
	for _, r := range ranges {
		n += r.Length()
	}
	return n
}
