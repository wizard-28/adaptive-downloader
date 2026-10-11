// Command aria2-benchmark runs a SEPARATE external-baseline measurement of
// aria2c against the same controlled experiment server and network conditions
// used by the internal six-mode benchmark. It does not modify the downloader,
// LinUCB, reward, features, exploration, server, or any frozen dataset.
//
// It starts an httptest server serving the scenario's content with the
// scenario's server-side rate knobs, applies the scenario's tc/netem condition,
// invokes aria2c once, verifies the output byte-for-byte, and records the
// server-observed HTTP range requests. Results are descriptive only.
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
)

const replicas = 4194304 // 4 MiB, matching benchmark v5

type Scenario struct {
	Name            string
	InitialRate     int64
	ChangedRate     int64
	RateChangeAfter int64
	Network         *network.Config
}

func scenarios() []Scenario {
	return []Scenario{
		{Name: "baseline", InitialRate: 4 << 20, ChangedRate: 4 << 20},
		{Name: "server-throttle", InitialRate: 512 << 10, ChangedRate: 4 << 20, RateChangeAfter: 4},
		{Name: "latency", InitialRate: 4 << 20, ChangedRate: 4 << 20, Network: &network.Config{Latency: 20 * time.Millisecond}},
		{Name: "loss", InitialRate: 4 << 20, ChangedRate: 4 << 20, Network: &network.Config{LossPercent: 1}},
		{Name: "combined", InitialRate: 1 << 20, ChangedRate: 4 << 20, Network: &network.Config{Latency: 20 * time.Millisecond, LossPercent: 1}},
	}
}

type RangeSummary struct {
	Count int      `json:"count"`
	Min   int64    `json:"min_bytes"`
	Max   int64    `json:"max_bytes"`
	Mean  float64  `json:"mean_bytes"`
	Start int64    `json:"first_start"`
	List  []string `json:"first_ranges"`
}

type RunResult struct {
	Scenario          string       `json:"scenario"`
	Rep               int          `json:"rep"`
	DurationMs        float64      `json:"duration_ms"`
	WallclockBps      float64      `json:"wallclock_bps"`
	ExitOK            bool         `json:"exit_ok"`
	OutputSize        int64        `json:"output_size"`
	OutputMatches     bool         `json:"output_matches"`
	ServerRequests    int64        `json:"server_total_requests"`
	ServerFailed      int64        `json:"server_failed_requests"`
	ServerMaxInFlight int64        `json:"server_max_in_flight_requests"`
	ServerNonRange    int          `json:"server_non_range_requests"`
	Range             RangeSummary `json:"range_requests"`
	MaxConnections    int          `json:"aria2_max_connections"`
	Retries           int          `json:"aria2_retries"`
	Netem             string       `json:"netem_status"`
	Err               string       `json:"err"`
	Command           string       `json:"command"`
}

type ScenarioResult struct {
	Name   string
	Config Scenario
	Runs   []RunResult
}

type Report struct {
	Aria2Version string           `json:"aria2_version"`
	FileSize     int64            `json:"file_size"`
	Repetitions  int              `json:"repetitions"`
	Host         string           `json:"host"`
	Kernel       string           `json:"kernel"`
	Scenarios    []ScenarioResult `json:"scenarios"`
	Command      string           `json:"aria2_base_command"`
}

func main() {
	reps := flag.Int("reps", 10, "repetitions per scenario")
	aria2 := flag.String("aria2", "aria2c", "aria2c binary")
	conns := flag.Int("connections", 4, "max connections per server (-x)")
	splits := flag.Int("splits", 4, "number of splits (-s)")
	minSplit := flag.Int("min-split", 1048576, "minimum split size in bytes (-k); aria2 requires >= 1 MiB")
	maxTries := flag.Int("max-tries", 3, "max tries (-m)")
	timeout := flag.Int("timeout", 30, "timeout seconds")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	out := flag.String("out", filepath.Join("testdata", "benchmark_aria2", "matrix_aria2_4MiB_reps10.json"), "JSON output")
	traceOut := flag.String("trace-out", filepath.Join("testdata", "diagnostics", "aria2", "request_trace.txt"), "request-trace text output")
	flag.Parse()

	if _, err := exec.LookPath(*aria2); err != nil {
		fatal(fmt.Errorf("aria2c not found: %w", err))
	}
	version := aria2Version(*aria2)

	var nc *network.Controller
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	} else {
		nc = network.NewController()
	}

	base := []string{
		"--no-conf", "--file-allocation=none", "--allow-overwrite=true",
		"--auto-file-renaming=false", "--enable-color=false",
		"--summary-interval=1", "--console-log-level=notice",
		"--max-connection-per-server=" + strconv.Itoa(*conns),
		"--split=" + strconv.Itoa(*splits),
		"--min-split-size=" + strconv.Itoa(*minSplit),
		"--max-tries=" + strconv.Itoa(*maxTries), "--retry-wait=1",
		"--timeout=" + strconv.Itoa(*timeout), "--connect-timeout=" + strconv.Itoa(*timeout),
		"--http-accept-gzip=false", "--check-certificate=false", "--disable-ipv6=true",
		"--conditional-get=false",
	}

	content := contentPattern(replicas)
	report := Report{Aria2Version: version, FileSize: replicas, Repetitions: *reps, Command: "aria2c " + strings.Join(base, " ") + " <url>"}
	report.Host, _ = os.Hostname()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		report.Kernel = strings.TrimSpace(string(b))
	}

	var trace strings.Builder
	trace.WriteString("aria2 request-trace (server-observed ranges; arrival order)\n")
	trace.WriteString("Source: controlled experiment server; ranges as recorded by server.Stats().RequestedRanges\n\n")

	for _, sc := range scenarios() {
		sr := ScenarioResult{Name: sc.Name, Config: sc}
		for r := 0; r < *reps; r++ {
			rr, traceLines := runOne(*aria2, base, sc, content, r, nc)
			sr.Runs = append(sr.Runs, rr)
			if r == 0 {
				trace.WriteString("== " + sc.Name + " (rep 0) ==\n")
				for _, l := range traceLines {
					trace.WriteString("  " + l + "\n")
				}
				trace.WriteString("\n")
			}
			fmt.Printf("[%s rep %d] %s dur=%.1fms size=%d match=%v ranges=%d maxCN=%d\n",
				sc.Name, r, map[bool]string{true: "ok", false: "FAIL"}[rr.ExitOK && rr.OutputMatches],
				rr.DurationMs, rr.OutputSize, rr.OutputMatches, rr.Range.Count, rr.MaxConnections)
		}
		report.Scenarios = append(report.Scenarios, sr)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*traceOut), 0o755); err != nil {
		fatal(err)
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*traceOut, []byte(trace.String()), 0o644); err != nil {
		fatal(err)
	}
	writeSummaryText(strings.TrimSuffix(*out, filepath.Ext(*out))+".txt", report)
	fmt.Printf("wrote %s and %s\n", *out, *traceOut)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "aria2-benchmark:", err)
	os.Exit(1)
}

func runOne(aria2 string, base []string, sc Scenario, content []byte, rep int, nc *network.Controller) (RunResult, []string) {
	rr := RunResult{Scenario: sc.Name, Rep: rep, Netem: "none"}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(sc.InitialRate)
	if sc.RateChangeAfter > 0 {
		srv.SetRateAfter(sc.RateChangeAfter, sc.ChangedRate)
	}
	tracker := &inflightTracker{inner: srv}
	ts := httptest.NewServer(tracker)
	defer ts.Close()

	if sc.Network != nil {
		if err := nc.Apply("lo", *sc.Network); err != nil {
			rr.Netem = "unavailable"
			rr.Err = "netem apply: " + err.Error()
			return rr, nil
		}
		defer func() { _ = nc.Clear("lo") }()
		rr.Netem = "applied"
	}

	dir, err := os.MkdirTemp("", "aria2-bench-")
	if err != nil {
		rr.Err = err.Error()
		return rr, nil
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	args := append([]string{}, base...)
	args = append(args, "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
	rr.Command = aria2 + " " + strings.Join(args, " ")

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, aria2, args...)
	start := time.Now()
	outBytes, runErr := cmd.CombinedOutput()
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	if runErr == nil {
		rr.ExitOK = true
	} else {
		rr.Err = runErr.Error()
	}
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(replicas) / (rr.DurationMs / 1000.0)
	}
	if info, err := os.Stat(outPath); err == nil {
		rr.OutputSize = info.Size()
	}
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputMatches = string(got) == string(contentCopy)
	}
	stats := srv.Stats()
	rr.ServerRequests = stats.TotalRequests
	rr.ServerFailed = stats.Failed
	rr.ServerMaxInFlight = tracker.max()
	rr.Range = summarizeRanges(stats.RequestedRanges)
	rr.ServerNonRange = int(stats.TotalRequests) - len(stats.RequestedRanges)
	if rr.ServerNonRange < 0 {
		rr.ServerNonRange = 0
	}
	rr.MaxConnections, rr.Retries = parseAria2Log(string(outBytes))

	var lines []string
	lines = append(lines, fmt.Sprintf("aria2 exit_ok=%v dur=%.1fms size=%d matches=%v", rr.ExitOK, rr.DurationMs, rr.OutputSize, rr.OutputMatches))
	lines = append(lines, fmt.Sprintf("server requests=%d failed=%d max_in_flight=%d non_range_requests=%d; range requests=%d min=%d max=%d mean=%.0f",
		stats.TotalRequests, stats.Failed, rr.ServerMaxInFlight, rr.ServerNonRange, rr.Range.Count, rr.Range.Min, rr.Range.Max, rr.Range.Mean))
	lines = append(lines, "ranges (arrival order): "+strings.Join(rr.Range.List, ", "))
	lines = append(lines, fmt.Sprintf("aria2 console max connections (CN)=%d; retry lines=%d", rr.MaxConnections, rr.Retries))
	return rr, lines
}

// inflightTracker wraps an http.Handler and records the maximum number of
// simultaneously in-flight requests observed at the server. It does not modify
// the experiment server.
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

func summarizeRanges(rs []server.Range) RangeSummary {
	s := RangeSummary{Count: len(rs)}
	const maxList = 24
	for i, r := range rs {
		l := r.Length()
		if i == 0 {
			s.Min = l
			s.Max = l
			s.Start = r.Start
		}
		if l < s.Min {
			s.Min = l
		}
		if l > s.Max {
			s.Max = l
		}
		s.Mean += float64(l)
		if i < maxList {
			s.List = append(s.List, fmt.Sprintf("%d-%d(%d)", r.Start, r.End, l))
		}
	}
	if len(rs) > 0 {
		s.Mean /= float64(len(rs))
	}
	if len(rs) > maxList {
		s.List = append(s.List, fmt.Sprintf("... +%d more", len(rs)-maxList))
	}
	return s
}

var cnRe = regexp.MustCompile(`CN:(\d+)`)

func parseAria2Log(log string) (maxConnections, retries int) {
	for _, line := range strings.Split(log, "\n") {
		if m := cnRe.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > maxConnections {
				maxConnections = v
			}
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "retry") || strings.Contains(low, "restarting") {
			retries++
		}
	}
	return
}

func aria2Version(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
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

func writeSummaryText(path string, r Report) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	ci95 := func(vals []float64) float64 {
		if len(vals) < 2 {
			return 0
		}
		m := mean(vals)
		var sq float64
		for _, v := range vals {
			sq += (v - m) * (v - m)
		}
		return 2.262 * math.Sqrt(sq/float64(len(vals))) / math.Sqrt(float64(len(vals)))
	}
	fmt.Fprintln(f, "aria2 external baseline (descriptive only; no ranking)")
	fmt.Fprintf(f, "%s\n", r.Aria2Version)
	fmt.Fprintf(f, "file size=%d reps=%d host=%s kernel=%s\n", r.FileSize, r.Repetitions, r.Host, r.Kernel)
	fmt.Fprintf(f, "command: %s\n", r.Command)
	fmt.Fprintln(f)
	fmt.Fprintf(f, "%-16s %10s %10s %10s %9s %12s %8s %8s %8s %6s %6s\n", "scenario", "mean_ms", "median_ms", "sd_ms", "ci95_ms", "wallclock", "ranges", "nonRange", "maxInFlight", "retr", "netem")
	for _, sc := range r.Scenarios {
		var dur, wc []float64
		for _, run := range sc.Runs {
			dur = append(dur, run.DurationMs)
			wc = append(wc, run.WallclockBps)
		}
		med := median(dur)
		var sd float64
		if len(dur) > 1 {
			m := mean(dur)
			for _, v := range dur {
				sd += (v - m) * (v - m)
			}
			sd = math.Sqrt(sd / float64(len(dur)))
		}
		ranges := 0
		nonRange := 0
		var maxInflight int64
		retr := 0
		netem := "none"
		for _, run := range sc.Runs {
			ranges += run.Range.Count
			nonRange += run.ServerNonRange
			if run.ServerMaxInFlight > maxInflight {
				maxInflight = run.ServerMaxInFlight
			}
			retr += run.Retries
			netem = run.Netem
		}
		fmt.Fprintf(f, "%-16s %10.1f %10.1f %10.1f %9.1f %12.0f %8.1f %8.1f %8d %6d %6s\n",
			sc.Name, mean(dur), med, sd, ci95(dur), mean(wc), float64(ranges)/float64(len(sc.Runs)), float64(nonRange)/float64(len(sc.Runs)), maxInflight, retr, netem)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "per-run validity:")
	valid := 0
	total := 0
	for _, sc := range r.Scenarios {
		for _, run := range sc.Runs {
			total++
			if run.ExitOK && run.OutputMatches && run.OutputSize == r.FileSize {
				valid++
			} else {
				fmt.Fprintf(f, "  INVALID %s rep %d exit=%v size=%d match=%v err=%s\n", run.Scenario, run.Rep, run.ExitOK, run.OutputSize, run.OutputMatches, run.Err)
			}
		}
	}
	fmt.Fprintf(f, "  valid=%d/%d\n", valid, total)
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func median(v []float64) float64 {
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
