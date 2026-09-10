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

	completionCallback func([]Interval) error
}

func New(totalSize int64) (*Tracker, error) {
	return NewWithCompleted(totalSize, nil)
}

func NewWithCompleted(
	totalSize int64,
	completed []Interval,
) (*Tracker, error) {
	if totalSize <= 0 {
		return nil, errors.New("total size must be positive")
	}

	if err := validateCompletedRanges(completed, totalSize); err != nil {
		return nil, err
	}

	pending := complementRanges(completed, totalSize)

	tracker := &Tracker{
		totalSize: totalSize,
		pending:   pending,
		inFlight:  make(map[string]Interval),
	}

	for _, interval := range completed {
		tracker.completedBytes += interval.Len()
	}

	return tracker, nil
}

var (
	ErrRequestNotFound        = errors.New("request ID not found in in-flight registry")
	ErrInvalidBytesDownloaded = errors.New("bytes downloaded cannot be negative")
)

func (t *Tracker) SetCompletionCallback(
	callback func([]Interval) error,
) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.completionCallback = callback
}

func (t *Tracker) NextRange(
	reqID string,
	chunkSize int64,
) (Interval, bool) {
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

func (t *Tracker) MarkComplete(reqID string) error {
	t.mu.Lock()

	interval, exists := t.inFlight[reqID]
	if !exists {
		t.mu.Unlock()

		return fmt.Errorf(
			"%w: %s",
			ErrRequestNotFound,
			reqID,
		)
	}

	delete(t.inFlight, reqID)
	t.completedBytes += interval.Len()

	callback := t.completionCallback

	var completed []Interval

	if callback != nil {
		completed = t.completedRangesLocked()
	}

	t.mu.Unlock()

	if callback != nil {
		if err := callback(completed); err != nil {
			return fmt.Errorf(
				"persist completed ranges: %w",
				err,
			)
		}
	}

	return nil
}

func (t *Tracker) Requeue(reqID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	interval, exists := t.inFlight[reqID]
	if !exists {
		return fmt.Errorf(
			"%w: %s",
			ErrRequestNotFound,
			reqID,
		)
	}

	delete(t.inFlight, reqID)
	t.addPendingAndCoalesce(interval)

	return nil
}

func (t *Tracker) RequeuePartial(
	reqID string,
	bytesDownloaded int64,
) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	interval, exists := t.inFlight[reqID]
	if !exists {
		return fmt.Errorf(
			"%w: %s",
			ErrRequestNotFound,
			reqID,
		)
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

func (t *Tracker) addPendingAndCoalesce(
	interval Interval,
) {
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

	merged := make([]Interval, 0, len(t.pending))

	for _, current := range t.pending {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}

		last := &merged[len(merged)-1]

		if current.Start <= last.End+1 {
			if current.End > last.End {
				last.End = current.End
			}
		} else {
			merged = append(merged, current)
		}
	}

	t.pending = merged
}

func (t *Tracker) IsComplete() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.completedBytes == t.totalSize
}

func (t *Tracker) Progress() (int64, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.completedBytes, t.totalSize
}

func (t *Tracker) CompletedRanges() []Interval {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.completedRangesLocked()
}

func (t *Tracker) completedRangesLocked() []Interval {
	ranges := make([]Interval, 0)

	// Anything not pending or in-flight is considered completed.
	//
	// We reconstruct the completed ranges from the current state rather than
	// maintaining another mutable interval structure.
	covered := make([]Interval, 0, len(t.pending)+len(t.inFlight))

	covered = append(covered, t.pending...)

	for _, interval := range t.inFlight {
		covered = append(covered, interval)
	}

	sortIntervals(covered)

	cursor := int64(0)

	for _, interval := range covered {
		if cursor < interval.Start {
			ranges = append(ranges, Interval{
				Start: cursor,
				End:   interval.Start - 1,
			})
		}

		if interval.End >= cursor {
			cursor = interval.End + 1
		}
	}

	if cursor < t.totalSize {
		ranges = append(ranges, Interval{
			Start: cursor,
			End:   t.totalSize - 1,
		})
	}

	return ranges
}

func validateCompletedRanges(
	completed []Interval,
	totalSize int64,
) error {
	var previousEnd int64 = -1

	for _, interval := range completed {
		if interval.Start < 0 {
			return fmt.Errorf(
				"completed range starts before zero: %s",
				interval,
			)
		}

		if interval.End < interval.Start {
			return fmt.Errorf(
				"invalid completed range: %s",
				interval,
			)
		}

		if interval.End >= totalSize {
			return fmt.Errorf(
				"completed range %s exceeds total size %d",
				interval,
				totalSize,
			)
		}

		if interval.Start <= previousEnd {
			return fmt.Errorf(
				"completed ranges overlap or are not sorted",
			)
		}

		previousEnd = interval.End
	}

	return nil
}

func complementRanges(
	completed []Interval,
	totalSize int64,
) []Interval {
	if len(completed) == 0 {
		return []Interval{
			{
				Start: 0,
				End:   totalSize - 1,
			},
		}
	}

	pending := make([]Interval, 0)
	cursor := int64(0)

	for _, completedRange := range completed {
		if cursor < completedRange.Start {
			pending = append(pending, Interval{
				Start: cursor,
				End:   completedRange.Start - 1,
			})
		}

		cursor = completedRange.End + 1
	}

	if cursor < totalSize {
		pending = append(pending, Interval{
			Start: cursor,
			End:   totalSize - 1,
		})
	}

	return pending
}

func sortIntervals(intervals []Interval) {
	for i := 1; i < len(intervals); i++ {
		current := intervals[i]
		j := i - 1

		for j >= 0 && intervals[j].Start > current.Start {
			intervals[j+1] = intervals[j]
			j--
		}

		intervals[j+1] = current
	}
}
