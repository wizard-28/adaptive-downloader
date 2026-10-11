package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"adaptivedl/internal/downloader"
)

func main() {
	url := flag.String("url", "", "source URL (required)")
	out := flag.String("out", "download.bin", "output file path")
	size := flag.Int64("size", 0, "total file size in bytes (required)")
	concurrency := flag.Int("concurrency", 4, "number of concurrent workers")
	chunk := flag.Int64("chunk", 1<<20, "chunk size in bytes")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request HTTP timeout")
	retries := flag.Int("retries", 3, "max retries per range")
	flag.Parse()

	if *url == "" || *size <= 0 {
		fmt.Fprintln(os.Stderr, "downloader: -url and a positive -size are required")
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dl, err := downloader.New(downloader.Config{
		URL:         *url,
		OutputPath:  *out,
		TotalSize:   *size,
		Concurrency: *concurrency,
		ChunkSize:   *chunk,
		Timeout:     *timeout,
		MaxRetries:  *retries,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "downloader:", err)
		os.Exit(1)
	}

	if err := dl.Download(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "download failed:", err)
		os.Exit(1)
	}

	fmt.Printf("downloaded %d bytes to %s\n", *size, *out)
}
