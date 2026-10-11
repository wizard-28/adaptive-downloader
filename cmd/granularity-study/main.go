// Command granularity-study runs a SMALL, controlled request-granularity
// sensitivity experiment. It uses the internal downloader in a static
// granularity-matched configuration (1 MiB chunks, concurrency 4) against the
// same controlled experiment server and network conditions as the frozen
// benchmarks, to help separate request-granularity effects from
// controller behavior in the aria2 comparison.
//
// It does NOT modify the downloader, controllers, experiment defaults, or any
// frozen dataset. It is measurement-only.
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
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
)

const (
	fileSize    = int64(4194304) // 4 MiB
	chunkSize   = int64(1048576) // 1 MiB
	concurrency = 4
)

type Scenario struct {
	Name            string
	InitialRate     int64
	ChangedRate     int64
	RateChangeAfter int64
	Network         *network.Config
	// Wall-clock schedule (optional). When ScheduleTransitionAfter > 0 the
	// server uses a time-based transition instead of the request-count switch.
	ScheduleTransitionAfter time.Duration
	ScheduleSubsequentRate  int64
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

// wallclockScenario is the granularity-matched internal control under the
// experimental wall-clock throttle (512 KiB/s -> 4 MiB/s after 1s elapsed).
func wallclockScenario() Scenario {
	return Scenario{
		Name:                    "server-throttle-wallclock",
		InitialRate:             512 << 10,
		ScheduleTransitionAfter: time.Second,
		ScheduleSubsequentRate:  4 << 20,
	}
}

type RangeSummary struct {
	Count int      `json:"count"`
	Min   int64    `json:"min_bytes"`
	Max   int64    `json:"max_bytes"`
	Mean  float64  `json:"mean_bytes"`
	List  []string `json:"first_ranges"`
}

type RunResult struct {
	Scenario      string       `json:"scenario"`
	Rep           int          `json:"rep"`
	DurationMs    float64      `json:"duration_ms"`
	WallclockBps  float64      `json:"wallclock_bps"`
	OutputMatches bool         `json:"output_matches"`
	NoDuplicate   bool         `json:"no_duplicate_ranges"`
	NoMissing     bool         `json:"no_missing_ranges"`
	TotalAttempts int64        `json:"total_attempts"`
	Range         RangeSummary `json:"server_range_requests"`
	MaxInFlight   int64        `json:"server_max_in_flight"`
	NonRangeReqs  int          `json:"server_non_range_requests"`
	Err           string       `json:"err"`
	Netem         string       `json:"netem_status"`
	// RequestLog is populated for the wall-clock scenario only.
	RequestLog    []server.RequestEvent `json:"request_log,omitempty"`
	RequestsPre1  int                   `json:"requests_started_before_1s,omitempty"`
	RequestsPost1 int                   `json:"requests_started_at_or_after_1s,omitempty"`
	FirstPostIdx  int                   `json:"first_post_transition_request_index,omitempty"`
	FirstPostSec  float64               `json:"first_post_transition_elapsed_s,omitempty"`
}

type ScenarioResult struct {
	Name   string
	Config Scenario
	Runs   []RunResult
}

type Report struct {
	Config      string           `json:"configuration"`
	FileSize    int64            `json:"file_size"`
	ChunkSize   int64            `json:"chunk_size"`
	Concurrency int              `json:"concurrency"`
	Repetitions int              `json:"repetitions"`
	Scenarios   []ScenarioResult `json:"scenarios"`
}

func main() {
	reps := flag.Int("reps", 10, "repetitions per scenario")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	scenario := flag.String("scenario", "", "scenario selector: empty = the five fixed scenarios (previous behavior); 'wallclock' = server-throttle-wallclock")
	out := flag.String("out", filepath.Join("testdata", "diagnostics", "request_granularity", "report.json"), "JSON report")
	traceOut := flag.String("trace-out", filepath.Join("testdata", "diagnostics", "request_granularity", "request_trace.json"), "request-trace JSON")
	flag.Parse()

	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}

	var scens []Scenario
	switch *scenario {
	case "":
		scens = scenarios()
	case "wallclock":
		scens = []Scenario{wallclockScenario()}
	default:
		fatal(fmt.Errorf("unknown scenario %q", *scenario))
	}

	rep := Report{
		Config:      "internal downloader, fixed policy, chunk=1048576 (min=max=initial), concurrency=4 (min=max), retries=3, timeout=30s",
		FileSize:    fileSize,
		ChunkSize:   chunkSize,
		Concurrency: concurrency,
		Repetitions: *reps,
	}
	content := contentPattern(fileSize)

	for _, sc := range scens {
		sr := ScenarioResult{Name: sc.Name, Config: sc}
		for r := 0; r < *reps; r++ {
			rr := runOne(sc, content, r, nc)
			sr.Runs = append(sr.Runs, rr)
			fmt.Printf("[%s rep %2d] %s dur=%8.1fms attempts=%d ranges=%d maxInflight=%d pre1=%d post1=%d\n",
				sc.Name, r, map[bool]string{true: "ok", false: "FAIL"}[rr.OutputMatches && rr.Err == ""],
				rr.DurationMs, rr.TotalAttempts, rr.Range.Count, rr.MaxInFlight, rr.RequestsPre1, rr.RequestsPost1)
		}
		rep.Scenarios = append(rep.Scenarios, sr)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*traceOut), 0o755); err != nil {
		fatal(err)
	}
	data, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*traceOut, requestTraceJSON(rep), 0o644); err != nil {
		fatal(err)
	}
	writeText(strings.TrimSuffix(*out, filepath.Ext(*out))+".txt", rep)
	fmt.Printf("wrote %s, %s and .txt\n", *out, *traceOut)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "granularity-study:", err)
	os.Exit(1)
}

func runOne(sc Scenario, content []byte, rep int, nc *network.Controller) RunResult {
	rr := RunResult{Scenario: sc.Name, Rep: rep, Netem: "none"}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(sc.InitialRate)
	if sc.RateChangeAfter > 0 {
		srv.SetRateAfter(sc.RateChangeAfter, sc.ChangedRate)
	}
	if sc.ScheduleTransitionAfter > 0 {
		srv.SetRequestLogEnabled(true)
	}
	tracker := &inflightTracker{inner: srv}
	ts := httptest.NewServer(tracker)
	defer ts.Close()

	if sc.Network != nil {
		if err := nc.Apply("lo", *sc.Network); err != nil {
			rr.Netem = "unavailable"
			rr.Err = "netem apply: " + err.Error()
			return rr
		}
		defer func() { _ = nc.Clear("lo") }()
		rr.Netem = "applied"
	}

	dir, err := os.MkdirTemp("", "granularity-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	dl, err := downloader.New(downloader.Config{
		URL: ts.URL, OutputPath: outPath, TotalSize: fileSize,
		Concurrency: concurrency, ChunkSize: chunkSize,
		MinConcurrency: concurrency, MaxConcurrency: concurrency,
		MinChunkSize: chunkSize, MaxChunkSize: chunkSize, InitialChunkSize: chunkSize, ChunkSizeStep: chunkSize,
		Timeout: 30 * time.Second, MaxRetries: 3, PolicyWindowSize: 4,
	})
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	start := time.Now()
	if sc.ScheduleTransitionAfter > 0 {
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{
			InitialRate:     sc.InitialRate,
			TransitionAfter: sc.ScheduleTransitionAfter,
			SubsequentRate:  sc.ScheduleSubsequentRate,
		}); err != nil {
			rr.Err = err.Error()
			return rr
		}
	}
	runErr := dl.Download(context.Background())
	rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	if runErr != nil {
		rr.Err = runErr.Error()
	}
	if rr.DurationMs > 0 {
		rr.WallclockBps = float64(fileSize) / (rr.DurationMs / 1000.0)
	}
	if got, err := os.ReadFile(outPath); err == nil {
		rr.OutputMatches = string(got) == string(contentCopy)
	}
	stats := srv.Stats()
	rr.Range = summarizeRanges(stats.RequestedRanges)
	rr.MaxInFlight = tracker.max()
	rr.NonRangeReqs = int(stats.TotalRequests) - len(stats.RequestedRanges)
	if rr.NonRangeReqs < 0 {
		rr.NonRangeReqs = 0
	}
	if sc.ScheduleTransitionAfter > 0 {
		rr.RequestLog = srv.RequestLog()
		if len(rr.RequestLog) > 0 {
			base := rr.RequestLog[0].Start
			rr.FirstPostIdx = -1
			for i, e := range rr.RequestLog {
				elapsed := e.Start.Sub(base)
				if elapsed < sc.ScheduleTransitionAfter {
					rr.RequestsPre1++
				} else {
					rr.RequestsPost1++
					if rr.FirstPostIdx < 0 {
						rr.FirstPostIdx = i
						rr.FirstPostSec = elapsed.Seconds()
					}
				}
			}
		}
	}
	snap := dl.Metrics()
	rr.TotalAttempts = snap.Aggregate.TotalAttempts
	rr.NoDuplicate, rr.NoMissing = verifyRanges(snap.Records, fileSize)
	return rr
}

func verifyRanges(records []metrics.Record, size int64) (noDuplicates, noMissing bool) {
	type seg struct{ s, e int64 }
	var segs []seg
	for _, r := range records {
		if r.Success {
			segs = append(segs, seg{r.RangeStart, r.RangeEnd})
		}
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].s < segs[j].s })
	noDuplicates, noMissing = true, true
	var prev int64 = -1
	for _, s := range segs {
		if s.s <= prev {
			noDuplicates = false
		}
		if s.s != prev+1 {
			noMissing = false
		}
		if s.e > prev {
			prev = s.e
		}
	}
	if prev != size-1 {
		noMissing = false
	}
	return
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

func summarizeRanges(rs []server.Range) RangeSummary {
	s := RangeSummary{Count: len(rs)}
	const maxList = 24
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
		if i < maxList {
			s.List = append(s.List, fmt.Sprintf("%d-%d(%d)", r.Start, r.End, l))
		}
	}
	if len(rs) > 0 {
		s.Mean /= float64(len(rs))
	}
	return s
}

func requestTraceJSON(rep Report) []byte {
	var b strings.Builder
	b.WriteString("{\n  \"note\": \"server-observed range requests per run (see report.json for full data)\",\n  \"scenarios\": [\n")
	for si, sc := range rep.Scenarios {
		b.WriteString("    {\"scenario\": " + jsonString(sc.Name) + ", \"runs\": [\n")
		for ri, rr := range sc.Runs {
			b.WriteString("      {\"rep\": " + fmt.Sprint(rr.Rep) + ", \"attempts\": " + fmt.Sprint(rr.TotalAttempts) +
				", \"range_count\": " + fmt.Sprint(rr.Range.Count) + ", \"range_min\": " + fmt.Sprint(rr.Range.Min) +
				", \"range_max\": " + fmt.Sprint(rr.Range.Max) + ", \"max_in_flight\": " + fmt.Sprint(rr.MaxInFlight) +
				", \"ranges\": [" + quoteList(rr.Range.List) + "]}")
			if ri < len(sc.Runs)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("    ]}")
		if si < len(rep.Scenarios)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return []byte(b.String())
}

func quoteList(xs []string) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = jsonString(x)
	}
	return strings.Join(out, ", ")
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

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
	fmt.Fprintln(f, "Request-granularity sensitivity study (descriptive only; no ranking)")
	fmt.Fprintf(f, "configuration: %s\n", r.Config)
	fmt.Fprintf(f, "file=%d reps=%d\n", r.FileSize, r.Repetitions)
	fmt.Fprintln(f)
	fmt.Fprintf(f, "%-16s %10s %10s %10s %9s %12s %8s %8s %8s %6s\n", "scenario", "mean_ms", "median_ms", "sd_ms", "ci95_ms", "wallclock", "attempts", "ranges", "maxInfl", "netem")
	valid, total := 0, 0
	for _, sc := range r.Scenarios {
		var dur, wc []float64
		attempts, ranges := 0, 0
		var maxInfl int64
		netem := "none"
		for _, run := range sc.Runs {
			dur = append(dur, run.DurationMs)
			wc = append(wc, run.WallclockBps)
			attempts += int(run.TotalAttempts)
			ranges += run.Range.Count
			if run.MaxInFlight > maxInfl {
				maxInfl = run.MaxInFlight
			}
			netem = run.Netem
			total++
			if run.OutputMatches && run.NoDuplicate && run.NoMissing && run.Err == "" {
				valid++
			}
		}
		m := mean(dur)
		var sd float64
		for _, v := range dur {
			sd += (v - m) * (v - m)
		}
		sd = math.Sqrt(sd / float64(len(dur)))
		fmt.Fprintf(f, "%-16s %10.1f %10.1f %10.1f %9.1f %12.0f %8.1f %8.1f %8d %6s\n",
			sc.Name, mean(dur), median(dur), sd, ci95(dur), mean(wc),
			float64(attempts)/float64(len(sc.Runs)), float64(ranges)/float64(len(sc.Runs)), maxInfl, netem)
	}
	fmt.Fprintf(f, "\nvalid=%d/%d (output byte-match, no duplicate/missing ranges)\n", valid, total)
}
