// Command l2-aria2-probe measures the real request granularity and concurrency
// of an aria2 configuration against the controlled experiment server. It is an
// experiment-only tool for the L2 environment-design stage; it does not modify
// the downloader, the frozen aria2-F baseline, or any frozen artifact.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"adaptivedl/internal/experiment/server"
)

const fileSize = int64(512 << 20)

func main() {
	split := flag.Int("split", 16, "aria2 --split")
	maxConn := flag.Int("max-conn", 16, "aria2 --max-connection-per-server")
	minSplit := flag.Int64("min-split", 1<<20, "aria2 --min-split-size")
	outDir := flag.String("out", filepath.Join("testdata", "diagnostics", "l2_environment_design", "aria2_probe"), "output directory")
	flag.Parse()
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}
	content := contentPattern(fileSize)
	expected := sha256Bytes(content)

	srv := server.New(content)
	srv.SetRequestLogEnabled(true)
	srv.SetRate(0) // unlimited: measure granularity/concurrency, not throughput
	ts := httptest.NewServer(srv)
	defer ts.Close()
	dir, err := os.MkdirTemp("", "l2-aria2-probe-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	args := []string{
		"--no-conf", "--file-allocation=none", "--allow-overwrite=true",
		"--auto-file-renaming=false", "--enable-color=false", "--summary-interval=1",
		"--console-log-level=notice", fmt.Sprintf("--min-split-size=%d", *minSplit),
		"--max-tries=3", "--retry-wait=1", "--timeout=30", "--connect-timeout=30",
		"--http-accept-gzip=false", "--check-certificate=false", "--disable-ipv6=true",
		"--conditional-get=false", fmt.Sprintf("--max-connection-per-server=%d", *maxConn),
		fmt.Sprintf("--split=%d", *split), "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL,
	}
	start := time.Now()
	outBytes, runErr := exec.Command("aria2c", args...).CombinedOutput()
	dur := float64(time.Since(start).Microseconds()) / 1000.0
	sha, size := "", int64(0)
	if got, err := os.ReadFile(outPath); err == nil {
		size = int64(len(got))
		sha = sha256Bytes(got)
	}
	events := srv.RequestLog()
	lens, rates := []int64{}, map[string]int{}
	maxInflight, area, span := 0, 0.0, 0.0
	type pt struct {
		t     time.Time
		delta int
	}
	var pts []pt
	for _, e := range events {
		if e.HasRange {
			lens = append(lens, e.Range.Length())
			rates[fmt.Sprintf("%d", e.Rate)]++
		}
		if !e.End.IsZero() && e.End.After(e.Start) {
			pts = append(pts, pt{e.Start, +1}, pt{e.End, -1})
		}
	}
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].t.Equal(pts[j].t) {
			return pts[i].delta < pts[j].delta
		}
		return pts[i].t.Before(pts[j].t)
	})
	cur := 0
	for i, p := range pts {
		if i > 0 {
			area += float64(cur) * p.t.Sub(pts[i-1].t).Seconds()
		}
		cur += p.delta
		if cur > maxInflight {
			maxInflight = cur
		}
	}
	if len(pts) > 1 {
		span = pts[len(pts)-1].t.Sub(pts[0].t).Seconds()
	}
	meanInflight := 0.0
	if span > 0 {
		meanInflight = area / span
	}
	result := map[string]any{
		"split": *split, "max_conn": *maxConn, "min_split": *minSplit,
		"args": args, "duration_ms": dur, "exit_err": errString(runErr),
		"output_size": size, "output_sha_ok": sha == expected,
		"total_requests": eventsCount(events), "range_requests": len(lens),
		"range_size_bytes": sortedUnique(lens), "rate_histogram": rates,
		"max_in_flight": maxInflight, "mean_in_flight": meanInflight,
		"non_range_requests": int(eventsCount(events)) - len(lens),
		"retries_log_lines":  countRetries(string(outBytes)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(b))
	if err := os.WriteFile(filepath.Join(*outDir, fmt.Sprintf("probe_split%d_maxconn%d.json", *split, *maxConn)), append(b, '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func eventsCount(events []server.RequestEvent) int64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Index
}

func sortedUnique(v []int64) []int64 {
	m := map[int64]bool{}
	for _, x := range v {
		m[x] = true
	}
	out := []int64{}
	for x := range m {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func countRetries(log string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "retry") || strings.Contains(low, "restarting") {
			n++
		}
	}
	return n
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
	fmt.Fprintln(os.Stderr, "l2-aria2-probe:", err)
	os.Exit(1)
}
