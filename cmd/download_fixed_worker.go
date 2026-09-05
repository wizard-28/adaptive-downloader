package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"adaptive-downloader/pkg/fetcher"
	"adaptive-downloader/pkg/policy"
	"adaptive-downloader/pkg/rangetracker"
	"adaptive-downloader/pkg/ui"

	"github.com/vbauerster/mpb/v8"
)

func runFixedWorkers(
	ctx context.Context,
	f *fetcher.Fetcher,
	writer fetcher.ChunkWriter,
	tracker *rangetracker.Tracker,
	chunkPolicy *policy.FixedChunkPolicy,
	targetURL string,
	progressUI *ui.ProgressUI,
) {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg           sync.WaitGroup
		workerCount  atomic.Int64
		totalWritten atomic.Int64
	)

	for workerID := 0; workerID < concurrency; workerID++ {
		wg.Add(1)

		go func(wID int) {
			defer wg.Done()

			runFixedWorker(
				workerCtx,
				wID,
				f,
				writer,
				tracker,
				chunkPolicy,
				targetURL,
				progressUI,
				&workerCount,
				&totalWritten,
			)
		}(workerID)
	}

	wg.Wait()
}

func runFixedWorker(
	ctx context.Context,
	workerID int,
	f *fetcher.Fetcher,
	writer fetcher.ChunkWriter,
	tracker *rangetracker.Tracker,
	chunkPolicy *policy.FixedChunkPolicy,
	targetURL string,
	progressUI *ui.ProgressUI,
	workerCounter *atomic.Int64,
	totalWritten *atomic.Int64,
) {
	if progressUI != nil {
		defer progressUI.RemoveWorkerBar(workerID)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if tracker.IsComplete() {
			return
		}

		reqNum := workerCounter.Add(1)
		reqID := fmt.Sprintf(
			"worker-%d-req-%d",
			workerID,
			reqNum,
		)

		interval, ok := tracker.NextRange(
			reqID,
			chunkPolicy.ChunkSize(),
		)

		if !ok || interval.Len() == 0 {
			return
		}

		var (
			chunkWriter fetcher.ChunkWriter = writer
			workerBar   *mpb.Bar
		)
		if progressUI != nil {
			workerBar = progressUI.GetOrAddWorkerBar(workerID, interval.Len())
			chunkWriter = progressUI.ProxyWriter(writer, workerBar)
		} else {
			fmt.Printf(
				"Worker %d: range %d-%d (%d bytes)\n",
				workerID,
				interval.Start,
				interval.End,
				interval.Len(),
			)
		}

		metrics, err := f.Fetch(
			ctx,
			fetcher.FetchRequest{
				ReqID:    reqID,
				URL:      targetURL,
				Interval: interval,
			},
			chunkWriter,
		)

		if err != nil {
			if workerBar != nil {
				workerBar.SetCurrent(0)
			}

			handleFixedFetchFailure(
				ctx,
				tracker,
				reqID,
				interval,
				metrics,
			)

			continue
		}

		if markErr := tracker.MarkComplete(reqID); markErr != nil {
			slog.Error(
				"Failed to mark chunk complete",
				slog.String("req_id", reqID),
				slog.Any("error", markErr),
			)
			continue
		}

		totalWritten.Add(metrics.BytesWritten)
	}
}

func handleFixedFetchFailure(
	ctx context.Context,
	tracker *rangetracker.Tracker,
	reqID string,
	interval rangetracker.Interval,
	metrics fetcher.Metrics,
) {
	slog.Warn(
		"Chunk fetch failed, requeuing partial range",
		slog.String("req_id", reqID),
		slog.Any("interval", interval),
		slog.Int64("bytes_written", metrics.BytesWritten),
	)

	_ = tracker.RequeuePartial(
		reqID,
		metrics.BytesWritten,
	)

	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
	}
}
