package downloader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"time"

	"adaptive-downloader/pkg/assembler"
	"adaptive-downloader/pkg/fetcher"
	"adaptive-downloader/pkg/policy"
	"adaptive-downloader/pkg/rangetracker"
	"adaptive-downloader/pkg/ui"
)

const retryDelay = 100 * time.Millisecond

// Downloader coordinates the download lifecycle.
//
// It owns the orchestration between probing, policy selection, range tracking,
// HTTP fetching, file assembly, verification, and worker execution.
type Downloader struct {
	config Config
}

// New creates a Downloader from the supplied configuration.
func New(config Config) (*Downloader, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Downloader{
		config: config,
	}, nil
}

// Download downloads targetURL to the configured output path.
func (d *Downloader) Download(
	ctx context.Context,
	targetURL string,
) error {
	outputPath := resolveOutputPath(
		targetURL,
		d.config.OutputPath,
	)

	initialContext := policy.Context{
		CurrentConcurrency: d.config.Concurrency,
		CurrentChunkSize:   d.config.ChunkSize,
	}

	d.config.Policy.Observe(initialContext)
	action := d.config.Policy.SelectAction()

	if err := validateAction(action); err != nil {
		return err
	}

	slog.Info(
		"Download policy selected",
		slog.Int("concurrency", action.Concurrency),
		slog.Int64("chunk_size", action.ChunkSize),
	)

	client := fetcher.New(action.Concurrency)

	probeResult, err := probeTarget(
		ctx,
		client,
		targetURL,
	)
	if err != nil {
		return err
	}

	writer, err := assembler.NewWriter(
		outputPath,
		probeResult.ContentLength,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize storage writer: %w",
			err,
		)
	}
	defer closeWriter(writer, outputPath)

	tracker, err := rangetracker.New(
		probeResult.ContentLength,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize range tracker: %w",
			err,
		)
	}

	progressUI := newProgressUI(
		probeResult.ContentLength,
		d.config.NoProgress,
	)

	start := time.Now()

	if err := runWorkers(
		ctx,
		client,
		writer,
		tracker,
		action,
		targetURL,
		progressUI,
	); err != nil {
		if progressUI != nil {
			progressUI.Wait()
		}

		return err
	}

	if progressUI != nil {
		progressUI.Wait()
	}

	if err := verifyDownload(tracker); err != nil {
		return err
	}

	if err := verifyChecksum(
		outputPath,
		d.config.SHA256,
	); err != nil {
		return err
	}

	logDownloadSummary(start, tracker)

	// FixedPolicy ignores observations. Adaptive policies will consume
	// observations as the monitoring/control loop is introduced.

	return nil
}

func probeTarget(
	ctx context.Context,
	client *fetcher.Client,
	targetURL string,
) (fetcher.ProbeResult, error) {
	slog.Info(
		"Probing target URL",
		slog.String("url", targetURL),
	)

	result, err := client.Probe(ctx, targetURL)
	if err != nil {
		return fetcher.ProbeResult{}, fmt.Errorf(
			"probe failed: %w",
			err,
		)
	}

	slog.Info(
		"Target server probed successfully",
		slog.Int64(
			"content_length",
			result.ContentLength,
		),
	)

	return result, nil
}

func validateAction(action policy.Action) error {
	if action.Valid() {
		return nil
	}

	return fmt.Errorf(
		"policy returned invalid action: concurrency=%d chunk_size=%d",
		action.Concurrency,
		action.ChunkSize,
	)
}

func newProgressUI(
	totalSize int64,
	disabled bool,
) *ui.ProgressUI {
	if disabled {
		return nil
	}

	return ui.NewProgressUI(totalSize)
}

func verifyDownload(
	tracker *rangetracker.Tracker,
) error {
	if tracker.IsComplete() {
		return nil
	}

	completed, total := tracker.Progress()

	return fmt.Errorf(
		"download incomplete: %d of %d bytes completed",
		completed,
		total,
	)
}

func verifyChecksum(
	outputPath string,
	expectedHash string,
) error {
	if expectedHash == "" {
		return nil
	}

	slog.Info(
		"Verifying SHA-256 checksum",
		slog.String("expected", expectedHash),
	)

	match, err := assembler.VerifySHA256Sum(
		outputPath,
		expectedHash,
	)
	if err != nil {
		return fmt.Errorf(
			"checksum verification error: %w",
			err,
		)
	}

	if !match {
		return errors.New(
			"checksum verification failed: hash mismatch",
		)
	}

	slog.Info("SHA-256 checksum verification passed")

	return nil
}

func logDownloadSummary(
	start time.Time,
	tracker *rangetracker.Tracker,
) {
	elapsed := time.Since(start)
	completedBytes, totalBytes := tracker.Progress()

	var averageThroughput float64

	if elapsed > 0 {
		averageThroughput =
			float64(completedBytes) /
				(1024 * 1024) /
				elapsed.Seconds()
	}

	slog.Info(
		"Download finished successfully",
		slog.Int64("bytes_downloaded", completedBytes),
		slog.Int64("total_size", totalBytes),
		slog.Duration("elapsed_time", elapsed),
		slog.Float64(
			"avg_throughput_mb_s",
			averageThroughput,
		),
	)

	fmt.Printf(
		"Download complete: %d bytes in %s, %.2f MB/s\n",
		completedBytes,
		elapsed.Round(time.Millisecond),
		averageThroughput,
	)
}

func closeWriter(
	writer *assembler.Writer,
	outputPath string,
) {
	if err := writer.Close(); err != nil {
		slog.Error(
			"Failed to close output writer",
			slog.String("path", outputPath),
			slog.Any("error", err),
		)
	}
}

func resolveOutputPath(
	targetURL string,
	current string,
) string {
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
