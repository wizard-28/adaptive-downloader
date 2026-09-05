package cmd

import (
	"fmt"
	"log/slog"
	"time"

	"adaptive-downloader/pkg/assembler"
	"adaptive-downloader/pkg/rangetracker"
)

func verifyChecksum() error {
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
