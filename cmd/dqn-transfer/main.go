// Command dqn-transfer evaluates a frozen DQN candidate against the REAL
// controlled downloader (not the simulator), for transfer validation only. It
// runs the existing five scenarios with the locked downloader configuration and
// 5 repetitions each (25 runs). No aria2 comparison, no performance claim.
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
	"sort"
	"strings"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/rl/dqn"
)

const (
	fileSize    = int64(4194304)
	chunkSize   = int64(32768)
	minChunk    = int64(32768)
	maxChunk    = int64(131072)
	initChunk   = int64(32768)
	chunkStep   = int64(32768)
	concurrency = 1
	minConc     = 1
	maxConc     = 4
)

type Scenario struct {
	Name            string
	InitialRate     int64
	ChangedRate     int64
	RateChangeAfter int64
	ScheduleAfter   time.Duration // wall-clock transition (server-throttle-wallclock)
	Network         *network.Config
}

func scenarios() []Scenario {
	return []Scenario{
		{Name: "baseline", InitialRate: 4 << 20, ChangedRate: 4 << 20},
		{Name: "server-throttle-wallclock", InitialRate: 512 << 10, ScheduleAfter: time.Second, ChangedRate: 4 << 20},
		{Name: "latency", InitialRate: 4 << 20, ChangedRate: 4 << 20, Network: &network.Config{Latency: 20 * time.Millisecond}},
		{Name: "loss", InitialRate: 4 << 20, ChangedRate: 4 << 20, Network: &network.Config{LossPercent: 1}},
		{Name: "combined", InitialRate: 1 << 20, ChangedRate: 4 << 20, Network: &network.Config{Latency: 20 * time.Millisecond, LossPercent: 1}},
	}
}

type RunResult struct {
	Scenario      string         `json:"scenario"`
	Rep           int            `json:"rep"`
	DurationMs    float64        `json:"duration_ms"`
	WallclockBps  float64        `json:"wallclock_bps"`
	OutputMatches bool           `json:"output_matches"`
	NoDuplicate   bool           `json:"no_duplicate_ranges"`
	NoMissing     bool           `json:"no_missing_ranges"`
	TotalAttempts int64          `json:"total_attempts"`
	FinalConc     int            `json:"final_concurrency"`
	MaxConc       int            `json:"max_concurrency"`
	FinalChunk    int64          `json:"final_chunk_size"`
	MinChunk      int64          `json:"min_chunk_size"`
	MaxChunk      int64          `json:"max_chunk_size"`
	Retries       int64          `json:"retries"`
	Decisions     []dqn.Decision `json:"decisions"`
	Err           string         `json:"err"`
	Netem         string         `json:"netem_status"`
}

type Report struct {
	ModelSHA256 string      `json:"model_sha256"`
	ModelPath   string      `json:"model_path"`
	FileSize    int64       `json:"file_size"`
	Repetitions int         `json:"repetitions"`
	Mode        string      `json:"mode"`
	Runs        []RunResult `json:"runs"`
}

func main() {
	modelPath := flag.String("model", filepath.Join("testdata", "diagnostics", "dqn_validation", "final_dqn_candidate.json"), "frozen DQN model JSON")
	shaPath := flag.String("sha", filepath.Join("testdata", "diagnostics", "dqn_validation", "final_dqn_candidate.sha256"), "expected model SHA-256 file")
	reps := flag.Int("reps", 5, "repetitions per scenario")
	maxChunkFlag := flag.Int64("max-chunk", maxChunk, "maximum chunk size (bytes); 131072 preserves v1/v2, 1048576 for v3")
	fixedChunkFlag := flag.Int64("fixed-chunk", 0, "when > 0, pin chunk size to this value (min=max=initial=step); used for v4 (1048576)")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	out := flag.String("out", filepath.Join("testdata", "diagnostics", "dqn_validation", "live_transfer_results.json"), "JSON output")
	flag.Parse()

	model, err := dqn.LoadModel(*modelPath)
	if err != nil {
		fatal(err)
	}
	expected := readSHA(*shaPath)
	actual := sha256File(*modelPath)
	if expected != "" && expected != actual {
		fatal(fmt.Errorf("model SHA mismatch: expected %s got %s", expected, actual))
	}

	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}

	rep := Report{ModelSHA256: actual, ModelPath: *modelPath, FileSize: fileSize, Repetitions: *reps, Mode: "dqn"}
	content := contentPattern(fileSize)

	for _, sc := range scenarios() {
		for r := 0; r < *reps; r++ {
			rr := runOne(model, sc, content, r, nc, *maxChunkFlag, *fixedChunkFlag)
			rep.Runs = append(rep.Runs, rr)
			fmt.Printf("[%s rep %d] %s dur=%.1fms attempts=%d finalC=%d finalChunk=%d decisions=%d\n",
				sc.Name, r, map[bool]string{true: "ok", false: "FAIL"}[rr.OutputMatches && rr.Err == ""],
				rr.DurationMs, rr.TotalAttempts, rr.FinalConc, rr.FinalChunk, len(rr.Decisions))
		}
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	data, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fatal(err)
	}
	writeText(strings.TrimSuffix(*out, filepath.Ext(*out))+".txt", rep)
	fmt.Printf("wrote %s and .txt\n", *out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "dqn-transfer:", err)
	os.Exit(1)
}

// newLivePolicyForModel selects the adapter that matches the model's state spec
// version. A Candidate v5 model (state spec version 2) must use the v5
// observation path; the v1 concurrency adapter also accepts a 13-feature input,
// so using it for a v5 model would silently run the wrong feature layout.
func newLivePolicyForModel(m *dqn.Model, totalBytes int64) (*dqn.LivePolicy, error) {
	switch {
	case m.NumActions == dqn.NumConcurrencyActions && m.StateSpec.Version == dqn.StateSpecVersionV6:
		return dqn.NewLiveConcurrencyPolicyV6(m, totalBytes)
	case m.NumActions == dqn.NumConcurrencyActions && m.StateSpec.Version == dqn.StateSpecVersionV5:
		return dqn.NewLiveConcurrencyPolicyV5(m, totalBytes)
	case m.NumActions == dqn.NumConcurrencyActions:
		return dqn.NewLiveConcurrencyPolicy(m, totalBytes)
	default:
		return dqn.NewLivePolicy(m, totalBytes)
	}
}

func runOne(model *dqn.Model, sc Scenario, content []byte, rep int, nc *network.Controller, maxChunkSize, fixedChunk int64) RunResult {
	rr := RunResult{Scenario: sc.Name, Rep: rep, Netem: "none"}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(sc.InitialRate)
	if sc.ScheduleAfter <= 0 && sc.RateChangeAfter > 0 {
		srv.SetRateAfter(sc.RateChangeAfter, sc.ChangedRate)
	}
	ts := httptest.NewServer(srv)
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

	dir, err := os.MkdirTemp("", "dqn-transfer-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	p, err := newLivePolicyForModel(model, fileSize)
	if err != nil {
		rr.Err = err.Error()
		return rr
	}

	dl, err := downloader.New(downloader.Config{
		URL: ts.URL, OutputPath: outPath, TotalSize: fileSize,
		Concurrency: concurrency, ChunkSize: chunkSize,
		MinConcurrency: minConc, MaxConcurrency: maxConc,
		MinChunkSize: pick(fixedChunk, minChunk), MaxChunkSize: pick(fixedChunk, maxChunkSize),
		InitialChunkSize: pick(fixedChunk, initChunk), ChunkSizeStep: pick(fixedChunk, chunkStep),
		Timeout: 30 * time.Second, MaxRetries: 3, PolicyWindowSize: 4, Policy: p,
	})
	if err != nil {
		rr.Err = err.Error()
		return rr
	}

	start := time.Now()
	if sc.ScheduleAfter > 0 {
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{InitialRate: sc.InitialRate, TransitionAfter: sc.ScheduleAfter, SubsequentRate: sc.ChangedRate}); err != nil {
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
	snap := dl.Metrics()
	rr.TotalAttempts = snap.Aggregate.TotalAttempts
	rr.Retries = 0
	for _, rec := range snap.Records {
		if rec.Attempt > 1 {
			rr.Retries++
		}
	}
	rr.NoDuplicate, rr.NoMissing = verifyRanges(snap.Records, fileSize)
	rr.FinalConc = dl.Concurrency()
	rr.MaxConc = dl.Concurrency()
	rr.FinalChunk = dl.ChunkSize()
	rr.MinChunk, rr.MaxChunk = dl.ChunkSize(), dl.ChunkSize()
	rr.Decisions = p.Log()
	for _, d := range rr.Decisions {
		if d.ConcurrencyBefore > rr.MaxConc {
			rr.MaxConc = d.ConcurrencyBefore
		}
		if d.ChunkBefore < rr.MinChunk {
			rr.MinChunk = d.ChunkBefore
		}
		if d.ChunkBefore > rr.MaxChunk {
			rr.MaxChunk = d.ChunkBefore
		}
	}
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

func writeText(path string, r Report) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, "DQN real-downloader transfer validation (FINAL DQN CANDIDATE v1; no aria2 comparison)")
	fmt.Fprintf(f, "model_sha256: %s\nreps/scenario: %d file=%d\n\n", r.ModelSHA256, r.Repetitions, r.FileSize)
	valid := 0
	for _, run := range r.Runs {
		if run.OutputMatches && run.NoDuplicate && run.NoMissing && run.Err == "" {
			valid++
		}
	}
	fmt.Fprintf(f, "valid runs: %d/%d\n\n", valid, len(r.Runs))
	fmt.Fprintf(f, "%-26s %8s %9s %7s %7s %9s %7s\n", "scenario", "rep", "dur_ms", "att", "finalC", "finalChunk", "retries")
	for _, run := range r.Runs {
		fmt.Fprintf(f, "%-26s %8d %9.1f %7d %7d %9d %7d\n", run.Scenario, run.Rep, run.DurationMs, run.TotalAttempts, run.FinalConc, run.FinalChunk, run.Retries)
	}
}

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func readSHA(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(b), " ", 2)[0])
}

func sha256File(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// pick returns fixed when it is > 0 (a pinned chunk configuration), else the
// default value.
func pick(fixed, def int64) int64 {
	if fixed > 0 {
		return fixed
	}
	return def
}
