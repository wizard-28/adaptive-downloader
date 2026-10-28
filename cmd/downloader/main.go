// Command downloader downloads one file with the internal range downloader.
//
// It is the demonstration entry point, not the experiment harness: it uses a
// fixed concurrency and chunk size, verifies the result (size, optional SHA-256)
// and reports timing. For adaptive policies, paired runs and controlled server
// models, see cmd/experiment and the studies under testdata/.
//
// Example:
//
//	go run ./cmd/downloader -url http://127.0.0.1:8080/file.bin \
//	    -out /tmp/file.bin -size 67108864 -concurrency 8 -chunk 1048576
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"adaptivedl/internal/downloader"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "downloader:", err)
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

var errUsage = errors.New("usage")

func run(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("downloader", flag.ContinueOnError)
	url := fs.String("url", "", "source URL (required)")
	out := fs.String("out", "download.bin", "output file path")
	size := fs.Int64("size", 0, "total file size in bytes; 0 probes the server for it")
	concurrency := fs.Int("concurrency", 4, "number of concurrent workers")
	chunk := fs.Int64("chunk", 1<<20, "chunk size in bytes")
	timeout := fs.Duration("timeout", 30*time.Second, "per-request HTTP timeout")
	retries := fs.Int("retries", 3, "max retries per range")
	wantSum := fs.String("sha256", "", "expected SHA-256 (hex); the download fails if it does not match")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if *url == "" || *size < 0 {
		fs.Usage()
		return fmt.Errorf("%w: -url is required and -size must not be negative", errUsage)
	}

	if *size == 0 {
		n, err := probe(ctx, &http.Client{Timeout: *timeout}, *url)
		if err != nil {
			return err
		}
		*size = n
		fmt.Fprintf(stdout, "server supports range requests; size %d bytes\n", n)
	}

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
		return err
	}
	start := time.Now()
	if err := dl.Download(ctx); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	elapsed := time.Since(start)
	fmt.Fprintf(stdout, "downloaded %d bytes to %s in %.1f s (%.2f MB/s)\n",
		*size, *out, elapsed.Seconds(), float64(*size)/elapsed.Seconds()/1e6)

	if *wantSum != "" {
		got, err := fileSHA256(*out)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, *wantSum) {
			return fmt.Errorf("sha256 mismatch: got %s, want %s", got, *wantSum)
		}
		fmt.Fprintf(stdout, "sha256 OK %s\n", got)
	}
	return nil
}

// probe asks for the first byte with a Range request. It succeeds only when
// the server answers 206 with a Content-Range that states the total size, so
// a server without range support is rejected before any download starts.
func probe(ctx context.Context, client *http.Client, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("probe: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("probe: server does not support range requests (status %d)", resp.StatusCode)
	}
	cr := resp.Header.Get("Content-Range")
	slash := strings.LastIndexByte(cr, '/')
	if !strings.HasPrefix(cr, "bytes 0-0/") || slash < 0 {
		return 0, fmt.Errorf("probe: unexpected Content-Range %q", cr)
	}
	n, err := strconv.ParseInt(cr[slash+1:], 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("probe: server did not state a total size in Content-Range %q", cr)
	}
	return n, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
