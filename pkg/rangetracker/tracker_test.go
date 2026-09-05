package rangetracker_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"adaptive-downloader/pkg/rangetracker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("creates tracker for positive file size", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)

		require.NoError(t, err)
		require.NotNil(t, tracker)

		completed, total := tracker.Progress()

		assert.Equal(t, int64(0), completed)
		assert.Equal(t, int64(1000), total)
		assert.False(t, tracker.IsComplete())
	})

	t.Run("rejects zero file size", func(t *testing.T) {
		tracker, err := rangetracker.New(0)

		require.Error(t, err)
		assert.Nil(t, tracker)
	})

	t.Run("rejects negative file size", func(t *testing.T) {
		tracker, err := rangetracker.New(-1)

		require.Error(t, err)
		assert.Nil(t, tracker)
	})
}

func TestNextRange(t *testing.T) {
	t.Run("allocates requested chunk size", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		interval, ok := tracker.NextRange("req-1", 100)

		require.True(t, ok)
		assert.Equal(t, rangetracker.Interval{
			Start: 0,
			End:   99,
		}, interval)
	})

	t.Run("allocates remaining range when smaller than chunk size", func(t *testing.T) {
		tracker, err := rangetracker.New(100)
		require.NoError(t, err)

		interval, ok := tracker.NextRange("req-1", 200)

		require.True(t, ok)
		assert.Equal(t, rangetracker.Interval{
			Start: 0,
			End:   99,
		}, interval)
	})

	t.Run("allocates multiple chunks sequentially", func(t *testing.T) {
		tracker, err := rangetracker.New(300)
		require.NoError(t, err)

		interval1, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		interval2, ok := tracker.NextRange("req-2", 100)
		require.True(t, ok)

		interval3, ok := tracker.NextRange("req-3", 100)
		require.True(t, ok)

		assert.Equal(t, rangetracker.Interval{Start: 0, End: 99}, interval1)
		assert.Equal(t, rangetracker.Interval{Start: 100, End: 199}, interval2)
		assert.Equal(t, rangetracker.Interval{Start: 200, End: 299}, interval3)
	})

	t.Run("returns false when no pending ranges remain", func(t *testing.T) {
		tracker, err := rangetracker.New(100)
		require.NoError(t, err)

		_, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		interval, ok := tracker.NextRange("req-2", 100)

		assert.False(t, ok)
		assert.Equal(t, rangetracker.Interval{}, interval)
	})

	t.Run("rejects zero chunk size", func(t *testing.T) {
		tracker, err := rangetracker.New(100)
		require.NoError(t, err)

		assert.PanicsWithValue(t, "rangetracker: chunkSize must be positive", func() {
			_, _ = tracker.NextRange("req-1", 0)
		})
	})

	t.Run("rejects negative chunk size", func(t *testing.T) {
		tracker, err := rangetracker.New(100)
		require.NoError(t, err)

		assert.PanicsWithValue(t, "rangetracker: chunkSize must be positive", func() {
			_, _ = tracker.NextRange("req-1", -10)
		})
	})

	t.Run("rejects duplicate request ID", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		first, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		assert.PanicsWithValue(t, "rangetracker: duplicate request ID in-flight: req-1", func() {
			_, _ = tracker.NextRange("req-1", 100)
		})

		// The original request remains valid.
		err = tracker.MarkComplete("req-1")
		require.NoError(t, err)

		completed, _ := tracker.Progress()
		assert.Equal(t, first.Len(), completed)
	})
}

func TestMarkComplete(t *testing.T) {
	t.Run("marks request as complete", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		interval, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.MarkComplete("req-1")

		require.NoError(t, err)

		completed, total := tracker.Progress()

		assert.Equal(t, interval.Len(), completed)
		assert.Equal(t, int64(1000), total)
	})

	t.Run("returns error for unknown request ID", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		err = tracker.MarkComplete("unknown")

		require.Error(t, err)
		assert.ErrorIs(t, err, rangetracker.ErrRequestNotFound)
	})

	t.Run("cannot complete the same request twice", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		_, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.MarkComplete("req-1")
		require.NoError(t, err)

		err = tracker.MarkComplete("req-1")

		require.Error(t, err)
		assert.ErrorIs(t, err, rangetracker.ErrRequestNotFound)
	})
}

func TestRequeue(t *testing.T) {
	t.Run("requeues completely failed request", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		interval, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.Requeue("req-1")
		require.NoError(t, err)

		// The requeued range should be available again.
		requeued, ok := tracker.NextRange("req-2", 100)

		require.True(t, ok)
		assert.Equal(t, interval, requeued)
	})

	t.Run("coalesces adjacent ranges", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		// Allocate two adjacent ranges.
		_, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		_, ok = tracker.NextRange("req-2", 100)
		require.True(t, ok)

		// Requeue both.
		err = tracker.Requeue("req-2")
		require.NoError(t, err)

		err = tracker.Requeue("req-1")
		require.NoError(t, err)

		// They should have been coalesced into one 200-byte range.
		merged, ok := tracker.NextRange("req-3", 200)

		require.True(t, ok)
		assert.Equal(t, rangetracker.Interval{
			Start: 0,
			End:   199,
		}, merged)
	})

	t.Run("returns error for unknown request ID", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		err = tracker.Requeue("unknown")

		require.Error(t, err)
		assert.ErrorIs(t, err, rangetracker.ErrRequestNotFound)
	})
}

func TestRequeuePartial(t *testing.T) {
	t.Run("requeues remaining bytes", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		_, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.RequeuePartial("req-1", 40)

		require.NoError(t, err)

		completed, _ := tracker.Progress()
		assert.Equal(t, int64(40), completed)

		// The remaining 60 bytes should be available.
		remaining, ok := tracker.NextRange("req-2", 100)

		require.True(t, ok)
		assert.Equal(t, rangetracker.Interval{
			Start: 40,
			End:   139,
		}, remaining)
	})

	t.Run("requeues entire interval when zero bytes were downloaded", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		original, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.RequeuePartial("req-1", 0)

		require.NoError(t, err)

		requeued, ok := tracker.NextRange("req-2", 100)

		require.True(t, ok)
		assert.Equal(t, original, requeued)
	})

	t.Run("marks entire interval complete when all bytes were downloaded", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		interval, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.RequeuePartial("req-1", interval.Len())

		require.NoError(t, err)

		completed, _ := tracker.Progress()
		assert.Equal(t, interval.Len(), completed)

		// The completed interval should not be available again.
		next, ok := tracker.NextRange("req-2", 100)

		require.True(t, ok)
		assert.Equal(t, int64(100), next.Start)
	})

	t.Run("handles bytes downloaded greater than interval length", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		interval, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.RequeuePartial("req-1", interval.Len()+50)

		require.NoError(t, err)

		completed, _ := tracker.Progress()
		assert.Equal(t, interval.Len(), completed)
	})

	t.Run("rejects negative bytes downloaded", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		_, ok := tracker.NextRange("req-1", 100)
		require.True(t, ok)

		err = tracker.RequeuePartial("req-1", -1)

		require.Error(t, err)
		assert.ErrorIs(t, err, rangetracker.ErrInvalidBytesDownloaded)

		// The request should remain in-flight after invalid input.
		// This is important because the failed operation did not actually
		// modify the tracker state.
		err = tracker.MarkComplete("req-1")
		require.NoError(t, err)
	})

	t.Run("returns error for unknown request ID", func(t *testing.T) {
		tracker, err := rangetracker.New(1000)
		require.NoError(t, err)

		err = tracker.RequeuePartial("unknown", 10)

		require.Error(t, err)
		assert.ErrorIs(t, err, rangetracker.ErrRequestNotFound)
	})
}

func TestIsComplete(t *testing.T) {
	tracker, err := rangetracker.New(100)
	require.NoError(t, err)

	assert.False(t, tracker.IsComplete())

	_, ok := tracker.NextRange("req-1", 100)
	require.True(t, ok)

	assert.False(t, tracker.IsComplete())

	err = tracker.MarkComplete("req-1")
	require.NoError(t, err)

	assert.True(t, tracker.IsComplete())
}

func TestProgress(t *testing.T) {
	tracker, err := rangetracker.New(1000)
	require.NoError(t, err)

	completed, total := tracker.Progress()

	assert.Equal(t, int64(0), completed)
	assert.Equal(t, int64(1000), total)

	_, ok := tracker.NextRange("req-1", 100)
	require.True(t, ok)

	err = tracker.MarkComplete("req-1")
	require.NoError(t, err)

	completed, total = tracker.Progress()

	assert.Equal(t, int64(100), completed)
	assert.Equal(t, int64(1000), total)
}

func TestRequeueOutOfOrderCoalescing(t *testing.T) {
	t.Run("coalesces non-sequential requeues into single contiguous range", func(t *testing.T) {
		tracker, err := rangetracker.New(300)
		require.NoError(t, err)

		// Allocate 3 contiguous chunks: [0-99], [100-199], [200-299]
		_, ok1 := tracker.NextRange("req-1", 100)
		_, ok2 := tracker.NextRange("req-2", 100)
		_, ok3 := tracker.NextRange("req-3", 100)
		require.True(t, ok1 && ok2 && ok3)

		// Requeue out of order: req-3 [200-299], then req-1 [0-99]
		require.NoError(t, tracker.Requeue("req-3"))
		require.NoError(t, tracker.Requeue("req-1"))

		// Intermediate state check: [0-99] should be allocated first
		firstPending, ok := tracker.NextRange("req-4", 100)
		require.True(t, ok)
		assert.Equal(t, rangetracker.Interval{Start: 0, End: 99}, firstPending)

		// Requeue req-4 [0-99] and req-2 [100-199] (filling the gap between 0-99 and 200-299)
		require.NoError(t, tracker.Requeue("req-4"))
		require.NoError(t, tracker.Requeue("req-2"))

		// All 300 bytes should now be coalesced into one continuous range
		fullRange, ok := tracker.NextRange("req-5", 300)
		require.True(t, ok)
		assert.Equal(t, rangetracker.Interval{Start: 0, End: 299}, fullRange)
	})
}

func TestConcurrentTrackerOperations(t *testing.T) {
	const (
		fileSize   = 100_000
		chunkSize  = 100
		numWorkers = 20
	)

	tracker, err := rangetracker.New(fileSize)
	require.NoError(t, err)

	var (
		wg      sync.WaitGroup
		counter atomic.Int64
	)

	for i := range numWorkers {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()

			for {
				reqNum := counter.Add(1)
				reqID := fmt.Sprintf("worker-%d-req-%d", workerIdx, reqNum)
				interval, ok := tracker.NextRange(reqID, chunkSize)
				if !ok {
					// No more pending intervals
					return
				}

				// Simulate arbitrary completion / failure behavior
				switch reqNum % 3 {
				case 0: // Full success
					_ = tracker.MarkComplete(reqID)
				case 1: // Full failure
					_ = tracker.Requeue(reqID)
				case 2: // Partial failure
					_ = tracker.RequeuePartial(reqID, interval.Len()/2)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify progress state invariants remain consistent under race conditions
	completed, total := tracker.Progress()
	assert.Equal(t, int64(fileSize), total)
	assert.GreaterOrEqual(t, completed, int64(0))
	assert.LessOrEqual(t, completed, int64(fileSize))
}
