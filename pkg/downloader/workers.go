package downloader

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"adaptive-downloader/pkg/fetcher"
	"adaptive-downloader/pkg/policy"
	"adaptive-downloader/pkg/rangetracker"
	"adaptive-downloader/pkg/ui"

	"golang.org/x/sync/errgroup"
)

// runWorkers executes a fixed number of workers using the supplied action.
//
// This is the temporary fixed-policy execution path. Dynamic concurrency and
// scheduling will be introduced when the Concurrency Manager and Scheduler
// are implemented.
func runWorkers(
	ctx context.Context,
	client *fetcher.Client,
	writer fetcher.ChunkWriter,
	tracker *rangetracker.Tracker,
	action policy.Action,
	targetURL string,
	progressUI *ui.ProgressUI,
) error {
	// If a worker returns a fatal error, errgroup cancels workerCtx so the
	// remaining workers stop.
	g, workerCtx := errgroup.WithContext(ctx)

	var requestID atomic.Int64

	for workerID := 0; workerID < action.Concurrency; workerID++ {
		workerID := workerID

		g.Go(func() error {
			return runWorker(
				workerCtx,
				workerID,
				client,
				writer,
				tracker,
				action,
				targetURL,
				progressUI,
				&requestID,
			)
		})
	}

	return g.Wait()
}

func runWorker(
	ctx context.Context,
	workerID int,
	client *fetcher.Client,
	writer fetcher.ChunkWriter,
	tracker *rangetracker.Tracker,
	action policy.Action,
	targetURL string,
	progressUI *ui.ProgressUI,
	requestID *atomic.Int64,
) error {
	if progressUI != nil {
		defer progressUI.RemoveWorkerBar(workerID)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		reqID := nextRequestID(workerID, requestID)

		interval, workLeft := tracker.NextRange(
			reqID,
			action.ChunkSize,
		)

		if !workLeft {
			return nil
		}

		chunkWriter := writer

		if progressUI != nil {
			chunkWriter = progressUI.Writer(
				writer,
				workerID,
				interval.Len(),
			)
		}

		metrics, err := client.Fetch(
			ctx,
			fetcher.FetchRequest{
				ReqID:    reqID,
				URL:      targetURL,
				Interval: interval,
			},
			chunkWriter,
		)
		if err != nil {
			// Cancellation is not a failed request. Do not requeue and retry
			// when the worker is being intentionally stopped.
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if progressUI != nil {
				progressUI.ResetWorker(workerID)
			}

			if err := requeueFailedRequest(
				ctx,
				tracker,
				reqID,
				interval,
				metrics,
			); err != nil {
				return err
			}

			continue
		}

		if err := tracker.MarkComplete(reqID); err != nil {
			return fmt.Errorf(
				"mark request %q complete: %w",
				reqID,
				err,
			)
		}
	}
}

func nextRequestID(
	workerID int,
	counter *atomic.Int64,
) string {
	return fmt.Sprintf(
		"worker-%d-req-%d",
		workerID,
		counter.Add(1),
	)
}

// requeueFailedRequest requeues the failed request's remaining bytes and waits
// for retryDelay before allowing the caller to retry the request.
func requeueFailedRequest(
	ctx context.Context,
	tracker *rangetracker.Tracker,
	reqID string,
	interval rangetracker.Interval,
	metrics fetcher.Metrics,
) error {
	slog.Warn(
		"chunk fetch failed, requeueing partial range",
		slog.String("req_id", reqID),
		slog.Any("interval", interval),
		slog.Int64("bytes_written", metrics.BytesWritten),
	)

	if err := tracker.RequeuePartial(
		reqID,
		metrics.BytesWritten,
	); err != nil {
		return fmt.Errorf(
			"requeue request %q: %w",
			reqID,
			err,
		)
	}

	// Wait before allowing the caller to retry the request.
	timer := time.NewTimer(retryDelay)
	defer timer.Stop()

	// Context cancells or timer expires, whichever happens first.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
