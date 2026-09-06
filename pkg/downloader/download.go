package downloader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"adaptive-downloader/pkg/assembler"
	"adaptive-downloader/pkg/fetcher"
	"adaptive-downloader/pkg/policy"
	"adaptive-downloader/pkg/rangetracker"
	"adaptive-downloader/pkg/ui"

	"github.com/vbauerster/mpb/v8"
)

// Config contains the options required to run a download.
type Config struct {
	OutputPath  string
	PolicyType  string
	Concurrency int
	ChunkSize   int64
	SHA256Sum   string
	NoProgress  bool
	JSONLogs    bool
}

// Run executes a download using the supplied configuration.
func Run(
	ctx context.Context,
	targetURL string,
	cfg Config,
) error {
	outputPath := resolveOutputPath(targetURL, cfg.OutputPath)

	f := fetcher.New(cfg.Concurrency)

	probeRes, err := probeTarget(ctx, f, targetURL)
	if err != nil {
		return err
	}

	writer, err := assembler.NewWriter(
		outputPath,
		probeRes.ContentLength,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize storage writer: %w",
			err,
		)
	}

	defer func() {
		if closeErr := writer.Close(); closeErr != nil {
			slog.Error(
				"Failed to close output writer",
				slog.String("path", outputPath),
				slog.Any("error", closeErr),
			)
		}
	}()

	tracker, err := rangetracker.New(probeRes.ContentLength)
	if err != nil {
		return fmt.Errorf(
			"initialize range tracker: %w",
			err,
		)
	}

	chunkPolicy, err := policy.NewFixedChunkPolicy(cfg.ChunkSize)
	if err != nil {
		return fmt.Errorf(
			"initialize fixed chunk policy: %w",
			err,
		)
	}

	slog.Info(
		"Fixed chunk policy initialized",
		slog.Int64("chunk_size", chunkPolicy.ChunkSize()),
	)

	var progressUI *ui.ProgressUI

	if !cfg.NoProgress && !cfg.JSONLogs {
		progressUI = ui.NewProgressUI(probeRes.ContentLength)
	}

	startTime := time.Now()

	runFixedWorkers(
		ctx,
		f,
		writer,
		tracker,
		chunkPolicy,
		cfg.Concurrency,
		targetURL,
		progressUI,
	)

	if progressUI != nil {
		progressUI.Wait()
	}

	if !tracker.IsComplete() {
		completed, total := tracker.Progress()

		return fmt.Errorf(
			"download incomplete: %d of %d bytes completed",
			completed,
			total,
		)
	}

	if err := verifyChecksum(outputPath, cfg.SHA256Sum); err != nil {
		return err
	}

	logDownloadSummary(startTime, tracker, outputPath)

	return nil
}

func probeTarget(
	ctx context.Context,
	f *fetcher.Fetcher,
	targetURL string,
) (fetcher.ProbeResult, error) {
	slog.Info(
		"Probing target URL",
		slog.String("url", targetURL),
	)

	probeRes, err := f.Probe(ctx, targetURL)
	if err != nil {
		if errors.Is(err, fetcher.ErrProbeFailed) {
			return fetcher.ProbeResult{}, fmt.Errorf(
				"server does not support HTTP byte-range requests",
			)
		}

		return fetcher.ProbeResult{}, fmt.Errorf(
			"probe failed: %w",
			err,
		)
	}

	if !probeRes.AcceptsRanges {
		return fetcher.ProbeResult{}, fmt.Errorf(
			"server does not support HTTP byte-range requests",
		)
	}

	if probeRes.ContentLength <= 0 {
		return fetcher.ProbeResult{}, fmt.Errorf(
			"invalid content length from probe: %d",
			probeRes.ContentLength,
		)
	}

	slog.Info(
		"Target server probed successfully",
		slog.Int64(
			"content_length",
			probeRes.ContentLength,
		),
		slog.Bool(
			"accepts_ranges",
			probeRes.AcceptsRanges,
		),
	)

	return probeRes, nil
}

func resolveOutputPath(targetURL, current string) string {
	if current != "" {
		return current
	}

	parsedURL, err := url.Parse(targetURL)
	if err == nil {
		base := path.Base(parsedURL.Path)

		if base != "" && base != "." && base != "/" {
			return base
		}
	}

	return "downloaded_file"
}

func runFixedWorkers(
	ctx context.Context,
	f *fetcher.Fetcher,
	writer fetcher.ChunkWriter,
	tracker *rangetracker.Tracker,
	chunkPolicy *policy.FixedChunkPolicy,
	concurrency int,
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
			workerBar = progressUI.GetOrAddWorkerBar(
				workerID,
				interval.Len(),
			)

			chunkWriter = progressUI.ProxyWriter(
				writer,
				workerBar,
			)
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

func verifyChecksum(outputPath, sha256sum string) error {
	if sha256sum == "" {
		return nil
	}

	slog.Info(
		"Verifying SHA-256 checksum...",
		slog.String("expected", sha256sum),
	)

	match, err := assembler.VerifySHA256Sum(
		outputPath,
		sha256sum,
	)

	if err != nil {
		return fmt.Errorf(
			"checksum verification error: %w",
			err,
		)
	}

	if !match {
		return fmt.Errorf(
			"checksum verification failed: hash mismatch",
		)
	}

	slog.Info("SHA-256 checksum verification passed")
	fmt.Println("SHA-256 verification: PASSED")

	return nil
}

func logDownloadSummary(
	startTime time.Time,
	tracker *rangetracker.Tracker,
	outputPath string,
) {
	elapsed := time.Since(startTime)
	completedBytes, totalBytes := tracker.Progress()

	var throughput float64

	if elapsed.Seconds() > 0 {
		throughput =
			(float64(completedBytes) / (1024 * 1024)) /
				elapsed.Seconds()
	}

	slog.Info(
		"Download finished successfully",
		slog.String("output", outputPath),
		slog.Int64("bytes_downloaded", completedBytes),
		slog.Int64("total_size", totalBytes),
		slog.Duration("elapsed_time", elapsed),
		slog.Float64("avg_throughput_mb_s", throughput),
	)

	fmt.Printf(
		"Download complete: %s (%d bytes in %s, %.2f MB/s)\n",
		outputPath,
		completedBytes,
		elapsed.Round(time.Millisecond),
		throughput,
	)
}
