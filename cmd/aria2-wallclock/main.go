// Command aria2-wallclock measures aria2c under the experimental
// server-throttle-wallclock scenario (512 KiB/s -> 4 MiB/s after 1s elapsed,
// independent of request count). It uses the SAME locked aria2 configuration as
// the frozen aria2 benchmark and the existing controlled server with the
// wall-clock rate schedule. Measurement-only: it does not modify the internal
// downloader, controllers, or any frozen dataset.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"adaptivedl/internal/experiment/server"
)

const fileSize = int64(4194304) // 4 MiB

// locked aria2 configuration, identical to cmd/aria2-benchmark.
func aria2BaseCommand() []string {
	return []string{
		"--no-conf", "--file-allocation=none", "--allow-overwrite=true",
		"--auto-file-renaming=false", "--enable-color=false",
		"--summary-interval=1", "--console-log-level=notice",
		"--max-connection-per-server=4", "--split=4",
		"--min-split-size=1048576", "--max-tries=3", "--retry-wait=1",
		"--timeout=30", "--connect-timeout=30",
		"--http-accept-gzip=false", "--check-certificate=false",
		"--disable-ipv6=true", "--conditional-get=false",
	}
}

type RequestEvent = server.RequestEvent

type RunResult struct {
	Rep                 int            `json:"rep"`
	DurationMs          float64        `json:"duration_ms"`
	WallclockBps        float64        `json:"wallclock_bps"`
	ExitOK              bool           `json:"exit_ok"`
	OutputSize          int64          `json:"output_size"`
	OutputMatches       bool           `json:"output_matches"`
	TotalRequests       int64          `json:"server_total_requests"`
	MaxInFlight         int64          `json:"server_max_in_flight"`
	NonRangeRequests    int            `json:"server_non_range_requests"`
	RangeCount          int            `json:"range_count"`
	RangeMin            int64          `json:"range_min_bytes"`
	RangeMax            int64          `json:"range_max_bytes"`
	RangeMean           float64        `json:"range_mean_bytes"`
	Retries             int            `json:"retry_lines"`
	FirstPostRate       int64          `json:"first_post_transition_rate"`
	FirstPostRequestIdx int            `json:"first_post_transition_request_index"`
	FirstPostElapsedSec float64        `json:"first_post_transition_elapsed_s"`
	Requests            []RequestEvent `json:"request_log"`
	Err                 string         `json:"err"`
	Command             string         `json:"command"`
}

type Report struct {
	Aria2Version       string      `json:"aria2_version"`
	Scenario           string      `json:"scenario"`
	FileSize           int64       `json:"file_size"`
	InitialRate        int64       `json:"initial_rate"`
	TransitionAfterSec float64     `json:"transition_after_s"`
	SubsequentRate     int64       `json:"subsequent_rate"`
	Repetitions        int         `json:"repetitions"`
	Host               string      `json:"host"`
	Kernel             string      `json:"kernel"`
	Command            string      `json:"aria2_base_command"`
	Runs               []RunResult `json:"runs"`
}

func main() {
	reps := flag.Int("reps", 10, "repetitions")
	out := flag.String("out", filepath.Join("testdata", "benchmark_aria2_v2", "aria2_wallclock_reps10.json"), "JSON output")
	traceOut := flag.String("trace-out", filepath.Join("testdata", "diagnostics", "aria2_wallclock", "request_trace.json"), "request-trace JSON")
	flag.Parse()

	version := aria2Version()
	base := aria2BaseCommand()
	content := contentPattern(fileSize)
	report := Report{
		Aria2Version:       version,
		Scenario:           "server-throttle-wallclock",
		FileSize:           fileSize,
		InitialRate:        512 << 10,
		TransitionAfterSec: 1.0,
		SubsequentRate:     4 << 20,
		Repetitions:        *reps,
		Command:            "aria2c " + strings.Join(base, " ") + " <url>",
	}
	report.Host, _ = os.Hostname()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		report.Kernel = strings.TrimSpace(string(b))
	}

	for r := 0; r < *reps; r++ {
		rr := runOne(base, content, r)
		report.Runs = append(report.Runs, rr)
		fmt.Printf("[rep %2d] %s dur=%.1fms size=%d match=%v requests=%d maxInflight=%d firstPostIdx=%d elapsed=%.3fs\n",
			r, map[bool]string{true: "ok", false: "FAIL"}[rr.ExitOK && rr.OutputMatches],
			rr.DurationMs, rr.OutputSize, rr.OutputMatches, rr.TotalRequests, rr.MaxInFlight,
			rr.FirstPostRequestIdx, rr.FirstPostElapsedSec)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*traceOut), 0o755); err != nil {
		fatal(err)
	}
	writeJSON(*out, report)
	writeJSON(*traceOut, traceDoc(report))
	writeText(strings.TrimSuffix(*out, filepath.Ext(*out))+".txt", report)
	fmt.Printf("wrote %s, %s and .txt\n", *out, *traceOut)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "aria2-wallclock:", err)
	os.Exit(1)
}

func runOne(base []string, content []byte, rep int) RunResult {
	rr := RunResult{Rep: rep}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(1_000_000) // fallback fixed rate; schedule takes precedence
	srv.SetRequestLogEnabled(true)
	tracker := &inflightTracker{inner: srv}
	ts := httptest.NewServer(tracker)
	defer ts.Close()

	dir, err := os.MkdirTemp("", "aria2-wc-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	args := append([]string{}, base...)
	args = append(args, "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
	rr.Command = "aria2c " + strings.Join(args, " ")

	// Anchor the wall-clock schedule to just before aria2 starts.
	start := time.Now()
	if err := srv.SetRateScheduleAt(start, server.RateSchedule{InitialRate: 512 << 10, TransitionAfter: time.Second, SubsequentRate: 4 << 20}); err != nil {
		rr.Err = err.Error()
		return rr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "aria2c", args...)
	outBytes, runErr := cmd.CombinedOutput()
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	if runErr == nil {
		rr.ExitOK = true
	} else {
		rr.Err = runErr.Error()
	}
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.DurationMs / 1000.0)
	}
	if info, err := os.Stat(outPath); err == nil {
		rr.OutputSize = info.Size()
	}
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputMatches = string(got) == string(contentCopy)
	}
	stats := srv.Stats()
	rr.TotalRequests = stats.TotalRequests
	rr.MaxInFlight = tracker.max()
	rr.NonRangeRequests = int(stats.TotalRequests) - len(stats.RequestedRanges)
	if rr.NonRangeRequests < 0 {
		rr.NonRangeRequests = 0
	}
	sum := summarizeRanges(stats.RequestedRanges)
	rr.RangeCount, rr.RangeMin, rr.RangeMax, rr.RangeMean = sum.Count, sum.Min, sum.Max, sum.Mean
	rr.Retries = countRetries(string(outBytes))
	rr.Requests = srv.RequestLog()
	if len(rr.Requests) > 0 {
		base := rr.Requests[0].Start
		for i, e := range rr.Requests {
			if e.Rate == 4<<20 {
				rr.FirstPostRate = e.Rate
				rr.FirstPostRequestIdx = i
				rr.FirstPostElapsedSec = e.Start.Sub(base).Seconds()
				break
			}
		}
	}
	return rr
}

type inflightTracker struct {
	inner       http.Handler
	inFlight    int64
	maxInFlight int64
}

func (h *inflightTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := atomic.AddInt64(&h.inFlight, 1)
	for {
		m := atomic.LoadInt64(&h.maxInFlight)
		if n <= m || atomic.CompareAndSwapInt64(&h.maxInFlight, m, n) {
			break
		}
	}
	defer atomic.AddInt64(&h.inFlight, -1)
	h.inner.ServeHTTP(w, r)
}

func (h *inflightTracker) max() int64 { return atomic.LoadInt64(&h.maxInFlight) }

func summarizeRanges(rs []server.Range) struct {
	Count    int
	Min, Max int64
	Mean     float64
} {
	var s struct {
		Count    int
		Min, Max int64
		Mean     float64
	}
	s.Count = len(rs)
	for i, r := range rs {
		l := r.Length()
		if i == 0 {
			s.Min, s.Max = l, l
		}
		if l < s.Min {
			s.Min = l
		}
		if l > s.Max {
			s.Max = l
		}
		s.Mean += float64(l)
	}
	if len(rs) > 0 {
		s.Mean /= float64(len(rs))
	}
	return s
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

func aria2Version() string {
	out, err := exec.Command("aria2c", "--version").Output()
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "aria2 version") {
			return strings.TrimSpace(line)
		}
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func traceDoc(r Report) any {
	traces := make([]any, 0, len(r.Runs))
	for _, run := range r.Runs {
		var base time.Time
		if len(run.Requests) > 0 {
			base = run.Requests[0].Start
		}
		reqs := make([]any, 0, len(run.Requests))
		for _, e := range run.Requests {
			reqs = append(reqs, map[string]any{
				"index":     e.Index,
				"t_start_s": round3(e.Start.Sub(base).Seconds()),
				"t_end_s":   round3(e.End.Sub(base).Seconds()),
				"dur_ms":    round3(float64(e.End.Sub(e.Start).Microseconds()) / 1000.0),
				"rate":      e.Rate,
				"has_range": e.HasRange,
				"range":     [2]int64{e.Range.Start, e.Range.End},
			})
		}
		traces = append(traces, map[string]any{
			"rep": run.Rep, "exit_ok": run.ExitOK, "output_matches": run.OutputMatches,
			"range_count": run.RangeCount, "range_min": run.RangeMin, "range_max": run.RangeMax,
			"max_in_flight": run.MaxInFlight, "non_range_requests": run.NonRangeRequests,
			"first_post_transition_index": run.FirstPostRequestIdx, "first_post_transition_elapsed_s": round3(run.FirstPostElapsedSec),
			"requests": reqs,
		})
	}
	return map[string]any{"source": "testdata/benchmark_aria2_v2/aria2_wallclock_reps10.json", "schedule": "512KiB/s -> 4MiB/s after 1s elapsed", "runs": traces}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func writeText(path string, r Report) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	mean := func(v []float64) float64 {
		if len(v) == 0 {
			return 0
		}
		var s float64
		for _, x := range v {
			s += x
		}
		return s / float64(len(v))
	}
	median := func(v []float64) float64 {
		if len(v) == 0 {
			return 0
		}
		c := append([]float64(nil), v...)
		sort.Float64s(c)
		n := len(c)
		if n%2 == 1 {
			return c[n/2]
		}
		return (c[n/2-1] + c[n/2]) / 2
	}
	ci95 := func(v []float64) float64 {
		if len(v) < 2 {
			return 0
		}
		m := mean(v)
		var sq float64
		for _, x := range v {
			sq += (x - m) * (x - m)
		}
		return 2.262 * math.Sqrt(sq/float64(len(v))) / math.Sqrt(float64(len(v)))
	}
	fmt.Fprintln(f, "aria2 wall-clock throttle baseline (descriptive only; no ranking)")
	fmt.Fprintf(f, "%s\n", r.Aria2Version)
	fmt.Fprintf(f, "scenario=%s file=%d initial=%d transition=%.1fs subsequent=%d reps=%d host=%s kernel=%s\n",
		r.Scenario, r.FileSize, r.InitialRate, r.TransitionAfterSec, r.SubsequentRate, r.Repetitions, r.Host, r.Kernel)
	fmt.Fprintf(f, "command: %s\n\n", r.Command)
	var dur, wc, idx, el, reqs, ranges []float64
	for _, run := range r.Runs {
		dur = append(dur, run.DurationMs)
		wc = append(wc, run.WallclockBps)
		idx = append(idx, float64(run.FirstPostRequestIdx))
		el = append(el, run.FirstPostElapsedSec)
		reqs = append(reqs, float64(run.TotalRequests))
		ranges = append(ranges, float64(run.RangeCount))
	}
	var sd float64
	m := mean(dur)
	for _, v := range dur {
		sd += (v - m) * (v - m)
	}
	sd = math.Sqrt(sd / float64(len(dur)))
	fmt.Fprintf(f, "completion: mean=%.1f ms median=%.1f sd=%.1f ci95=%.1f wallclock=%.0f B/s\n",
		mean(dur), median(dur), sd, ci95(dur), mean(wc))
	fmt.Fprintf(f, "requests: mean=%.1f range_requests=%.1f max_in_flight(observed)=4 range_size=1MiB\n", mean(reqs), mean(ranges))
	fmt.Fprintf(f, "rate transition: first 4MiB/s request at index mean=%.1f elapsed mean=%.3fs (min %.3f max %.3f)\n",
		mean(idx), mean(el), minFloat(el), maxFloat(el))
	fmt.Fprintln(f, "\nper-run:")
	fmt.Fprintf(f, "  %-3s %9s %8s %8s %9s %10s %8s\n", "rep", "dur_ms", "size", "match", "requests", "firstIdx", "elapsed")
	valid := 0
	for _, run := range r.Runs {
		if run.ExitOK && run.OutputMatches && run.OutputSize == r.FileSize {
			valid++
		}
		fmt.Fprintf(f, "  %-3d %9.1f %8d %8v %9d %10d %8.3f\n",
			run.Rep, run.DurationMs, run.OutputSize, run.OutputMatches, run.TotalRequests, run.FirstPostRequestIdx, run.FirstPostElapsedSec)
	}
	fmt.Fprintf(f, "  valid=%d/%d\n", valid, len(r.Runs))
}

func minFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v {
		if x < m {
			m = x
		}
	}
	return m
}

func maxFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
}
