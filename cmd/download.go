package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"adaptive-downloader/pkg/assembler"
	"adaptive-downloader/pkg/fetcher"
	"adaptive-downloader/pkg/rangetracker"

	"github.com/spf13/cobra"
)

var (
	outputPath  string
	policyType  string
	concurrency int
	chunkSize   int64
	sha256sum   string
)

// downloadCmd represents the download command
var downloadCmd = &cobra.Command{
	Use:          "download <url>",
	Short:        "Start a download",
	Args:         cobra.ExactArgs(1),
	Long:         `Download a file using multi-threaded HTTP byte-range requests. For example: download https://example.com/file.iso -o ./file.iso`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetURL := args[0]
		ctx := cmd.Context()

		if concurrency <= 0 {
			return fmt.Errorf("concurrency must be greater than 0, got %d", concurrency)
		}
		if chunkSize <= 0 {
			return fmt.Errorf("chunk size must be greater than 0, got %d", chunkSize)
		}

		if outputPath == "" {
			parsedURL, err := url.Parse(targetURL)
			if err == nil {
				base := path.Base(parsedURL.Path)
				if base != "" && base != "." && base != "/" {
					outputPath = base
				}
			}
			if outputPath == "" {
				outputPath = "downloaded_file"
			}
		}

		// 1. Probe Target URL
		f := fetcher.New(concurrency)
		slog.Info("Probing target URL", slog.String("url", targetURL))

		probeRes, err := f.Probe(ctx, targetURL)
		if err != nil {
			return fmt.Errorf("probe failed: %w", err)
		}
		if !probeRes.AcceptsRanges {
			return fmt.Errorf("server does not support HTTP byte-range requests (Accept-Ranges: bytes missing or rejected)")
		}
		if probeRes.ContentLength <= 0 {
			return fmt.Errorf("invalid content length from probe: %d", probeRes.ContentLength)
		}

		slog.Info("Target server probed successfully",
			slog.Int64("content_length", probeRes.ContentLength),
			slog.Bool("accepts_ranges", probeRes.AcceptsRanges),
		)

		// 2. Initialize Storage & Tracker
		writer, err := assembler.NewWriter(outputPath, probeRes.ContentLength)
		if err != nil {
			return fmt.Errorf("initialize storage writer: %w", err)
		}
		defer func() {
			if closeErr := writer.Close(); closeErr != nil {
				slog.Error("Failed to close output writer", slog.String("path", outputPath), slog.Any("error", closeErr))
			}
		}()

		tracker, err := rangetracker.New(probeRes.ContentLength)
		if err != nil {
			return fmt.Errorf("initialize range tracker: %w", err)
		}

		// 3. Fixed Worker Loop
		startTime := time.Now()
		var (
			wg            sync.WaitGroup
			workerCounter atomic.Int64
			totalWritten  atomic.Int64
		)
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		for workerID := 0; workerID < concurrency; workerID++ {
			wg.Add(1)
			go func(wID int) {
				defer wg.Done()
				for {
					select {
					case <-workerCtx.Done():
						return
					default:
					}

					if tracker.IsComplete() {
						return
					}

					reqNum := workerCounter.Add(1)
					reqID := fmt.Sprintf("worker-%d-req-%d", wID, reqNum)

					interval, ok := tracker.NextRange(reqID, chunkSize)
					if !ok || interval.Len() == 0 {
						return
					}

					fetchReq := fetcher.FetchRequest{
						ReqID:    reqID,
						URL:      targetURL,
						Interval: interval,
					}

					metrics, fetchErr := f.Fetch(workerCtx, fetchReq, writer)
					if fetchErr != nil {
						slog.Warn("Chunk fetch failed, requeuing partial range",
							slog.String("req_id", reqID),
							slog.Any("interval", interval),
							slog.Int64("bytes_written", metrics.BytesWritten),
							slog.Any("error", fetchErr),
						)
						_ = tracker.RequeuePartial(reqID, metrics.BytesWritten)
						totalWritten.Add(metrics.BytesWritten)

						// Back off briefly before retrying
						select {
						case <-workerCtx.Done():
							return
						case <-time.After(100 * time.Millisecond):
							continue
						}
					}

					if markErr := tracker.MarkComplete(reqID); markErr != nil {
						slog.Error("Failed to mark chunk complete", slog.String("req_id", reqID), slog.Any("error", markErr))
					}
					totalWritten.Add(metrics.BytesWritten)
				}
			}(workerID)
		}

		wg.Wait()

		if !tracker.IsComplete() {
			completed, total := tracker.Progress()
			return fmt.Errorf("download incomplete: %d of %d bytes completed", completed, total)
		}

		// 4. Checksum Verification
		if sha256sum != "" {
			slog.Info("Verifying SHA-256 checksum...", slog.String("expected", sha256sum))
			match, verifyErr := assembler.VerifySHA256Sum(outputPath, sha256sum)
			if verifyErr != nil {
				return fmt.Errorf("checksum verification error: %w", verifyErr)
			}
			if !match {
				return fmt.Errorf("checksum verification failed: hash mismatch")
			}
			slog.Info("SHA-256 checksum verification passed")
			fmt.Println("SHA-256 verification: PASSED")
		}

		// 5. Log Summary
		elapsed := time.Since(startTime)
		completedBytes, totalBytes := tracker.Progress()
		var avgThroughputMBs float64
		if elapsed.Seconds() > 0 {
			avgThroughputMBs = (float64(completedBytes) / (1024 * 1024)) / elapsed.Seconds()
		}

		slog.Info("Download finished successfully",
			slog.String("output", outputPath),
			slog.Int64("bytes_downloaded", completedBytes),
			slog.Int64("total_size", totalBytes),
			slog.Duration("elapsed_time", elapsed),
			slog.Float64("avg_throughput_mb_s", avgThroughputMBs),
		)
		fmt.Printf("Download complete: %s (%d bytes in %s, %.2f MB/s)\n", outputPath, completedBytes, elapsed.Round(time.Millisecond), avgThroughputMBs)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	const defaultN = 2
	const defaultS = 5 * 1024 * 1024

	// Define help without shorthand 'h' to allow -h for sha256
	downloadCmd.Flags().Bool("help", false, "help for download")

	downloadCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Path to downloaded file")
	downloadCmd.Flags().StringVarP(&policyType, "policy", "p", "fixed", "Policy: fixed | linucb")
	downloadCmd.Flags().IntVarP(&concurrency, "concurrency", "n", defaultN, "Concurrency limit N")
	downloadCmd.Flags().Int64VarP(&chunkSize, "chunk-size", "s", defaultS, "Chunk size S in bytes")
	downloadCmd.Flags().StringVarP(&sha256sum, "sha256", "h", "", "Sha256 checksum to verify against")

	// Backward-compatibility aliases for flag names
	downloadCmd.Flags().IntVar(&concurrency, "initial-concurrency", defaultN, "Initial concurrency limit N")
	downloadCmd.Flags().Int64Var(&chunkSize, "initial-chunk-size", defaultS, "Initial chunk size in bytes")
	downloadCmd.Flags().StringVar(&sha256sum, "sha256sum", "", "Sha256 checksum to verify against")
	_ = downloadCmd.Flags().MarkHidden("initial-concurrency")
	_ = downloadCmd.Flags().MarkHidden("initial-chunk-size")
	_ = downloadCmd.Flags().MarkHidden("sha256sum")
}
