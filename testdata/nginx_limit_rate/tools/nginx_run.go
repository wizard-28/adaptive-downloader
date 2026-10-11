// Command nginx_run executes ONE download of a static file served by a stock
// NGINX instance (limit_rate / limit_rate_after) with either our fixed-policy
// downloader or aria2c, under the same tc/netem as the D2 benchmark.
// Output is verified by SHA-256 against -expect-sha.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
)

type result struct {
	RunID           string         `json:"run_id"`
	System          string         `json:"system"`
	Config          map[string]any `json:"config"`
	CompletionMs    float64        `json:"completion_ms"`
	OutputSize      int64          `json:"output_size_bytes"`
	OutputSHA256    string         `json:"output_sha256"`
	ContentVerified bool           `json:"content_verified"`
	NetemStatus     string         `json:"netem_status"`
	Aria2Exit       int            `json:"aria2_exit"`
	ClientRequests  int64          `json:"client_requests"`
	ClientFailed    int64          `json:"client_failed"`
	ClientRetries   int64          `json:"client_retries"`
	Err             string         `json:"err"`
}

func main() {
	system := flag.String("system", "", "ours|aria2")
	url := flag.String("url", "", "file URL")
	size := flag.Int64("file-size", 0, "file size bytes")
	expect := flag.String("expect-sha", "", "expected SHA-256 of the file")
	c := flag.Int("c", 16, "ours: concurrency")
	chunk := flag.Int64("chunk", 4<<20, "ours: chunk bytes")
	aria2Args := flag.String("aria2-args", "", "aria2 flags (URL appended)")
	out := flag.String("out", "", "result JSON")
	runID := flag.String("run-id", "", "run id")
	flag.Parse()
	if *system == "" || *url == "" || *out == "" || *size <= 0 || *expect == "" {
		fmt.Fprintln(os.Stderr, "nginx_run: missing required flags")
		os.Exit(2)
	}
	res := result{RunID: *runID, System: *system, NetemStatus: "applied",
		Config: map[string]any{"url": *url, "file_size_bytes": *size}}
	var nc *network.Controller
	var iface string
	shaped := false
	finish := func(code int) {
		// os.Exit skips deferred calls, so shaping is cleared explicitly here.
		if shaped {
			if cerr := nc.Clear(iface); cerr != nil {
				res.Err += " | netem clear: " + cerr.Error()
				code = 3
			}
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		_ = os.WriteFile(*out, append(b, '\n'), 0o644)
		fmt.Printf("run %s system=%s completion=%.3fs verified=%v err=%q\n", res.RunID, res.System, res.CompletionMs/1000, res.ContentVerified, res.Err)
		os.Exit(code)
	}

	nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	iface, err := nc.InterfaceForHost("127.0.0.1")
	if err != nil {
		res.Err = "interface: " + err.Error()
		res.NetemStatus = "unavailable"
		finish(3)
	}
	if err := nc.Apply(iface, network.Config{Latency: 40 * time.Millisecond, LossPercent: 0.4}); err != nil {
		res.Err = "netem apply: " + err.Error()
		res.NetemStatus = "unavailable"
		finish(3)
	}
	shaped = true

	dir, err := os.MkdirTemp("", "nginx-run-")
	if err != nil {
		res.Err = err.Error()
		finish(3)
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	start := time.Now()
	switch *system {
	case "ours":
		res.Config["concurrency"], res.Config["chunk_bytes"] = *c, *chunk
		dl, derr := downloader.New(downloader.Config{URL: *url, OutputPath: outPath, TotalSize: *size,
			Concurrency: *c, ChunkSize: *chunk, Timeout: 30 * time.Second, MaxRetries: 3})
		if derr != nil {
			res.Err = derr.Error()
			break
		}
		if e := dl.Download(context.Background()); e != nil {
			res.Err = e.Error()
		}
		snap := dl.Metrics()
		res.ClientRequests, res.ClientFailed = snap.Aggregate.TotalRequests, snap.Aggregate.FailedRequests
		for _, r := range snap.Records {
			if r.Attempt > 1 {
				res.ClientRetries++
			}
		}
	case "aria2":
		res.Config["aria2_args"] = *aria2Args
		args := append(strings.Fields(*aria2Args), "-d", dir, "-o", "out.bin", "--interface=lo", *url)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		_, aerr := exec.CommandContext(ctx, "aria2c", args...).CombinedOutput()
		if aerr != nil {
			res.Err = "aria2: " + aerr.Error()
			if ee, ok := aerr.(*exec.ExitError); ok {
				res.Aria2Exit = ee.ExitCode()
			}
		}
	default:
		res.Err = "unknown system"
	}
	res.CompletionMs = float64(time.Since(start).Microseconds()) / 1000.0

	if st, e := os.Stat(outPath); e == nil {
		res.OutputSize = st.Size()
		f, _ := os.Open(outPath)
		h := sha256.New()
		_, _ = io.Copy(h, f)
		f.Close()
		res.OutputSHA256 = hex.EncodeToString(h.Sum(nil))
		res.ContentVerified = res.OutputSize == *size && res.OutputSHA256 == *expect
	}
	if res.Err != "" || !res.ContentVerified {
		finish(3)
	}
	finish(0)
}
