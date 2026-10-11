// Command l4-live-probe runs a small live fixed-concurrency sweep for the L4
// design stage: controlled server rate + optional shared tc/netem bottleneck,
// internal downloader at fixed C, 1 MiB chunks. Experiment-only; no RL.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
)

func main() {
	sizeMib := flag.Int64("size-mib", 64, "file size MiB")
	rate := flag.Int64("rate", 8<<20, "server per-response rate bytes/s")
	link := flag.Int64("link", 0, "shared tc bandwidth cap bytes/s (0 = none)")
	latMs := flag.Int("lat-ms", 0, "tc latency ms")
	loss := flag.Float64("loss", 0.0, "tc loss percent")
	cs := flag.String("cs", "1,4,8,16", "comma-separated fixed concurrency values")
	label := flag.String("label", "regime", "regime label")
	out := flag.String("out", "testdata/diagnostics/l4_environment_design/live", "output dir")
	useSudo := flag.Bool("sudo", true, "use sudo -n for tc")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	size := *sizeMib << 20
	content := contentPattern(size)
	expected := sha256Bytes(content)
	var results []map[string]any
	for _, csStr := range strings.Split(*cs, ",") {
		var c int
		fmt.Sscanf(strings.TrimSpace(csStr), "%d", &c)
		if c < 1 {
			continue
		}
		rr := map[string]any{"label": *label, "concurrency": c, "file_size": size,
			"rate": *rate, "link": *link, "latency_ms": *latMs, "loss_percent": *loss}
		srv := server.New(content)
		srv.SetRate(*rate)
		ts := httptest.NewServer(srv)
		netApplied := false
		if *link > 0 || *latMs > 0 || *loss > 0 {
			cfg := network.Config{BandwidthBps: *link * 8, Latency: time.Duration(*latMs) * time.Millisecond, LossPercent: *loss}
			if err := cfg.Validate(); err != nil {
				ts.Close()
				rr["err"] = err.Error()
				results = append(results, rr)
				continue
			}
			if err := nc.Apply("lo", cfg); err != nil {
				ts.Close()
				rr["err"] = "netem: " + err.Error()
				results = append(results, rr)
				continue
			}
			netApplied = true
		}
		dir, _ := os.MkdirTemp("", "l4-live-")
		outPath := filepath.Join(dir, "out.bin")
		dl, err := downloader.New(downloader.Config{
			URL: ts.URL, OutputPath: outPath, TotalSize: size,
			Concurrency: c, ChunkSize: 1 << 20, MinConcurrency: c, MaxConcurrency: c,
			Timeout: 60 * time.Second, MaxRetries: 3,
		})
		if err != nil {
			rr["err"] = err.Error()
		} else {
			start := time.Now()
			runErr := dl.Download(context.Background())
			dur := float64(time.Since(start).Microseconds()) / 1000.0
			sha, got := "", int64(0)
			if b, err := os.ReadFile(outPath); err == nil {
				sha = sha256Bytes(b)
				got = int64(len(b))
			}
			rr["duration_ms"] = dur
			rr["wallclock_bps"] = float64(size) / (dur / 1000.0)
			rr["output_size"] = got
			rr["sha_ok"] = sha == expected
			if runErr != nil {
				rr["err"] = runErr.Error()
			}
			fmt.Printf("[%s C%d] %.1fms %.2f MiB/s valid=%v\n", *label, c, dur, float64(size)/(dur/1000.0)/(1<<20), sha == expected && runErr == nil)
		}
		if netApplied {
			_ = nc.Clear("lo")
		}
		ts.Close()
		os.RemoveAll(dir)
		results = append(results, rr)
	}
	b, _ := json.MarshalIndent(map[string]any{"regime": *label, "results": results}, "", "  ")
	writePath := filepath.Join(*out, fmt.Sprintf("live_%s.json", *label))
	if err := os.WriteFile(writePath, append(b, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\n", writePath)
}

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "l4-live-probe:", err)
	os.Exit(1)
}
