package cmd

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

func runDownload(ctx context.Context, targetURL string) error {
	outputPath = resolveOutputPath(targetURL, outputPath)

	f := fetcher.New(concurrency)

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

	chunkPolicy, err := policy.NewFixedChunkPolicy(chunkSize)
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
	if !noProgress && logFormat != "json" {
		progressUI = ui.NewProgressUI(probeRes.ContentLength)
	}

	startTime := time.Now()

	runFixedWorkers(
		ctx,
		f,
		writer,
		tracker,
		chunkPolicy,
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

	if err := verifyChecksum(); err != nil {
		return err
	}

	logDownloadSummary(startTime, tracker)

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
