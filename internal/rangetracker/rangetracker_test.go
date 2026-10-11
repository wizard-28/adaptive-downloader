package rangetracker

import (
	"errors"
	"sort"
	"sync"
	"testing"
)

func TestSequentialAllocation(t *testing.T) {
	tr := New(100)

	for i := int64(0); i < 10; i++ {
		want := Range{Start: i * 10, End: i*10 + 10}
		got, ok := tr.Allocate(10)
		if !ok {
			t.Fatalf("allocation %d: got ok=false, want true", i)
		}
		if got != want {
			t.Fatalf("allocation %d: got %v, want %v", i, got, want)
		}
	}

	if got, ok := tr.Allocate(10); ok {
		t.Fatalf("expected exhaustion, got %v", got)
	}
	if got := tr.PendingBytes(); got != 0 {
		t.Fatalf("PendingBytes() = %d, want 0", got)
	}
	if got := tr.InFlightBytes(); got != 100 {
		t.Fatalf("InFlightBytes() = %d, want 100", got)
	}
}

func TestVariableChunkSizes(t *testing.T) {
	tr := New(100)
	sizes := []int64{30, 10, 25, 5, 30}

	start := int64(0)
	for i, size := range sizes {
		got, ok := tr.Allocate(size)
		if !ok {
			t.Fatalf("allocation %d: got ok=false, want true", i)
		}
		want := Range{Start: start, End: start + size}
		if got != want {
			t.Fatalf("allocation %d: got %v, want %v", i, got, want)
		}
		start += size
	}

	if got, ok := tr.Allocate(1); ok {
		t.Fatalf("expected exhaustion, got %v", got)
	}
	if got := tr.InFlightBytes(); got != 100 {
		t.Fatalf("InFlightBytes() = %d, want 100", got)
	}
}

func TestFinalPartialChunk(t *testing.T) {
	tr := New(25)

	first, ok := tr.Allocate(10)
	if !ok || first != (Range{Start: 0, End: 10}) {
		t.Fatalf("first allocation = %v, %v; want {0 10}, true", first, ok)
	}
	second, ok := tr.Allocate(10)
	if !ok || second != (Range{Start: 10, End: 20}) {
		t.Fatalf("second allocation = %v, %v; want {10 20}, true", second, ok)
	}

	last, ok := tr.Allocate(10)
	if !ok {
		t.Fatal("final allocation: got ok=false, want true")
	}
	if want := (Range{Start: 20, End: 25}); last != want {
		t.Fatalf("final allocation = %v, want %v", last, want)
	}
	if got := last.Length(); got != 5 {
		t.Fatalf("final allocation length = %d, want 5", got)
	}

	if got, ok := tr.Allocate(1); ok {
		t.Fatalf("expected exhaustion, got %v", got)
	}
}

func TestSingleAllocationLargerThanFile(t *testing.T) {
	tr := New(25)
	got, ok := tr.Allocate(100)
	if !ok {
		t.Fatal("got ok=false, want true")
	}
	if want := (Range{Start: 0, End: 25}); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, ok := tr.Allocate(1); ok {
		t.Fatalf("expected exhaustion, got %v", got)
	}
}

func TestEmptyFile(t *testing.T) {
	tr := New(0)
	if !tr.Done() {
		t.Fatal("Done() = false, want true for empty file")
	}
	if got, ok := tr.Allocate(10); ok {
		t.Fatalf("expected no allocation, got %v", got)
	}
	if got := tr.TotalSize(); got != 0 {
		t.Fatalf("TotalSize() = %d, want 0", got)
	}
}

func TestInvalidChunkSize(t *testing.T) {
	tr := New(100)
	for _, size := range []int64{0, -1, -100} {
		if got, ok := tr.Allocate(size); ok {
			t.Fatalf("Allocate(%d) = %v, true; want false", size, got)
		}
	}
	if got := tr.PendingBytes(); got != 100 {
		t.Fatalf("PendingBytes() = %d, want 100", got)
	}
}

func TestCompletion(t *testing.T) {
	tr := New(100)

	first, _ := tr.Allocate(40)
	if err := tr.Complete(first); err != nil {
		t.Fatalf("Complete(%v) = %v, want nil", first, err)
	}
	if got := tr.CompletedBytes(); got != 40 {
		t.Fatalf("CompletedBytes() = %d, want 40", got)
	}
	completed := tr.Completed()
	if len(completed) != 1 || completed[0] != first {
		t.Fatalf("Completed() = %v, want [%v]", completed, first)
	}
	if got := tr.InFlightBytes(); got != 0 {
		t.Fatalf("InFlightBytes() = %d, want 0", got)
	}

	second, _ := tr.Allocate(60)
	if err := tr.Complete(second); err != nil {
		t.Fatalf("Complete(%v) = %v, want nil", second, err)
	}

	// Adjacent completed ranges are merged into one.
	completed = tr.Completed()
	if len(completed) != 1 || completed[0] != (Range{Start: 0, End: 100}) {
		t.Fatalf("Completed() = %v, want [{0 100}]", completed)
	}
	if got := tr.CompletedBytes(); got != 100 {
		t.Fatalf("CompletedBytes() = %d, want 100", got)
	}
	if !tr.Done() {
		t.Fatal("Done() = false, want true")
	}
}

func TestCompleteUnknownRange(t *testing.T) {
	tr := New(100)
	alloc, _ := tr.Allocate(10)

	if err := tr.Complete(Range{Start: 10, End: 20}); !errors.Is(err, ErrRangeNotInFlight) {
		t.Fatalf("Complete(unknown) = %v, want ErrRangeNotInFlight", err)
	}
	if err := tr.Complete(alloc); err != nil {
		t.Fatalf("Complete(in-flight) = %v, want nil", err)
	}
	if err := tr.Complete(alloc); !errors.Is(err, ErrRangeNotInFlight) {
		t.Fatalf("double Complete = %v, want ErrRangeNotInFlight", err)
	}
}

func TestFailedRangeRequeue(t *testing.T) {
	tr := New(100)

	a, _ := tr.Allocate(30) // [0,30)
	b, _ := tr.Allocate(30) // [30,60)

	if err := tr.Fail(b); err != nil {
		t.Fatalf("Fail(%v) = %v, want nil", b, err)
	}
	if got := len(tr.InFlight()); got != 1 {
		t.Fatalf("len(InFlight()) = %d, want 1", got)
	}
	// b merged with the remaining tail into a single pending range.
	if pending := tr.Pending(); len(pending) != 1 || pending[0] != (Range{Start: 30, End: 100}) {
		t.Fatalf("Pending() = %v, want [{30 100}]", pending)
	}

	retried, ok := tr.Allocate(30)
	if !ok {
		t.Fatal("retry allocation: got ok=false, want true")
	}
	if retried != b {
		t.Fatalf("retry allocation = %v, want %v", retried, b)
	}

	// Failing a range that is not in flight is rejected.
	if err := tr.Fail(Range{Start: 0, End: 5}); !errors.Is(err, ErrRangeNotInFlight) {
		t.Fatalf("Fail(unknown) = %v, want ErrRangeNotInFlight", err)
	}

	// A completed range is no longer in flight and cannot be failed.
	if err := tr.Complete(a); err != nil {
		t.Fatalf("Complete(%v) = %v, want nil", a, err)
	}
	if err := tr.Fail(a); !errors.Is(err, ErrRangeNotInFlight) {
		t.Fatalf("Fail(completed) = %v, want ErrRangeNotInFlight", err)
	}
}

func TestRequeueMergesNeighbors(t *testing.T) {
	tr := New(30)

	a, _ := tr.Allocate(10) // [0,10), pending [10,30)
	b, _ := tr.Allocate(10) // [10,20), pending [20,30)

	// Failing b merges it with the pending successor [20,30).
	if err := tr.Fail(b); err != nil {
		t.Fatalf("Fail(b) = %v, want nil", err)
	}
	if pending := tr.Pending(); len(pending) != 1 || pending[0] != (Range{Start: 10, End: 30}) {
		t.Fatalf("after Fail(b) Pending() = %v, want [{10 30}]", pending)
	}

	if err := tr.Fail(a); err != nil {
		t.Fatalf("Fail(a) = %v, want nil", err)
	}
	if pending := tr.Pending(); len(pending) != 1 || pending[0] != (Range{Start: 0, End: 30}) {
		t.Fatalf("after Fail(a) Pending() = %v, want [{0 30}]", pending)
	}

	if got, ok := tr.Allocate(30); !ok || got != (Range{Start: 0, End: 30}) {
		t.Fatalf("allocation after full requeue = %v, %v; want {0 30}, true", got, ok)
	}
}

func TestPreventDuplicateAllocation(t *testing.T) {
	tr := New(10)

	got, ok := tr.Allocate(10)
	if !ok || got != (Range{Start: 0, End: 10}) {
		t.Fatalf("Allocate(10) = %v, %v; want {0 10}, true", got, ok)
	}
	// The whole file is in flight, so it cannot be handed out again.
	if dup, ok := tr.Allocate(10); ok {
		t.Fatalf("duplicate Allocate returned %v, want ok=false", dup)
	}

	// No overlap and full coverage across varied chunk sizes.
	tr2 := New(1000)
	var allocated []Range
	for i := 0; ; i++ {
		size := int64(7 + i%13)
		r, ok := tr2.Allocate(size)
		if !ok {
			break
		}
		allocated = append(allocated, r)
	}
	assertNoOverlap(t, allocated)
	assertCoverage(t, allocated, 1000)
}

func TestConcurrentAccess(t *testing.T) {
	const (
		total   = 100_000
		workers = 8
	)
	tr := New(total)

	var (
		mu         sync.Mutex
		failedOnce = make(map[int64]bool)
		completed  []Range
		wg         sync.WaitGroup
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			chunk := int64(100 + id)
			for {
				r, ok := tr.Allocate(chunk)
				if !ok {
					return
				}

				// Fail each distinct starting offset at most once to
				// exercise requeueing without looping forever.
				if r.Start%2 == 0 {
					mu.Lock()
					already := failedOnce[r.Start]
					failedOnce[r.Start] = true
					mu.Unlock()
					if !already {
						if err := tr.Fail(r); err != nil {
							t.Errorf("Fail(%v) = %v, want nil", r, err)
						}
						continue
					}
				}

				if err := tr.Complete(r); err != nil {
					t.Errorf("Complete(%v) = %v, want nil", r, err)
				} else {
					mu.Lock()
					completed = append(completed, r)
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if !tr.Done() {
		t.Fatalf("Done() = false; pending=%d inFlight=%d", len(tr.Pending()), len(tr.InFlight()))
	}
	if got := tr.CompletedBytes(); got != total {
		t.Fatalf("CompletedBytes() = %d, want %d", got, total)
	}
	if got := tr.Completed(); len(got) != 1 || got[0] != (Range{Start: 0, End: total}) {
		t.Fatalf("Completed() = %v, want [{0 %d}]", got, total)
	}

	assertNoOverlap(t, completed)
	assertCoverage(t, completed, total)
}

// assertNoOverlap fails if any two ranges share bytes.
func assertNoOverlap(t *testing.T, ranges []Range) {
	t.Helper()
	sorted := append([]Range(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Start < sorted[i-1].End {
			t.Fatalf("overlapping ranges: %v and %v", sorted[i-1], sorted[i])
		}
	}
}

// assertCoverage fails unless the ranges exactly cover [0, total).
func assertCoverage(t *testing.T, ranges []Range, total int64) {
	t.Helper()
	sorted := append([]Range(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	var covered int64
	next := int64(0)
	for _, r := range sorted {
		if r.Start != next {
			t.Fatalf("coverage gap at %d: next range %v", next, r)
		}
		covered += r.Length()
		next = r.End
	}
	if covered != total {
		t.Fatalf("covered %d bytes, want %d", covered, total)
	}
}
