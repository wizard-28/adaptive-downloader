// Command aria2-calibration-v2 selects ONE global aria2 configuration for the
// 16 MiB / 1 MiB-granularity evaluation, using a separate validation set. It is
// calibration only: it does not touch the DQN, LinUCB, downloader, server, or any
// frozen dataset, and it does not inspect final-test conditions.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
)

const fileSize = int64(16 * 1024 * 1024) // 16 MiB

type CandidateConfig struct {
	Name         string `json:"name"`
	MaxConns     int    `json:"max_connection_per_server"`
	Split        int    `json:"split"`
	MinSplitSize int64  `json:"min_split_size"`
}

func candidates() []CandidateConfig {
	return []CandidateConfig{
		{Name: "A", MaxConns: 1, Split: 16, MinSplitSize: 1048576},
		{Name: "B", MaxConns: 2, Split: 16, MinSplitSize: 1048576},
		{Name: "C", MaxConns: 4, Split: 16, MinSplitSize: 1048576},
		{Name: "D", MaxConns: 4, Split: 8, MinSplitSize: 1048576},
		{Name: "E", MaxConns: 4, Split: 4, MinSplitSize: 1048576},
		// F is an explicit deviation added after the specified A-E set failed the
		// section-4 granularity check: aria2 bounds its split count by
		// --max-connection-per-server, so A-E yield 4 MiB pieces at best. Only
		// maxconn >= 16 yields ~16 x 1 MiB ranges at min-split-size = 1 MiB.
		{Name: "F", MaxConns: 16, Split: 16, MinSplitSize: 1048576},
	}
}

type ValidationProfile struct {
	Name            string
	InitialRate     int64
	SubsequentRate  int64
	TransitionAfter time.Duration
	LatencyMs       int
	LossPercent     float64
}

func profiles() []ValidationProfile {
	return []ValidationProfile{
		{Name: "V16-1", InitialRate: 1 << 20, SubsequentRate: 1 << 20},
		{Name: "V16-2", InitialRate: 4 << 20, SubsequentRate: 4 << 20, LatencyMs: 25},
		{Name: "V16-3", InitialRate: 2 << 20, SubsequentRate: 2 << 20, LossPercent: 1},
		{Name: "V16-4", InitialRate: 1 << 20, SubsequentRate: 6 << 20, TransitionAfter: 2 * time.Second},
		{Name: "V16-5", InitialRate: 1536 << 10, SubsequentRate: 5 << 20, TransitionAfter: 3 * time.Second, LatencyMs: 25, LossPercent: 1},
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
	Config         string       `json:"config"`
	Profile        string       `json:"profile"`
	Rep            int          `json:"rep"`
	DurationMs     float64      `json:"duration_ms"`
	WallclockBps   float64      `json:"wallclock_bps"`
	ExitOK         bool         `json:"exit_ok"`
	OutputSize     int64        `json:"output_size"`
	OutputMatches  bool         `json:"output_matches"`
	TotalRequests  int64        `json:"server_total_requests"`
	MaxInFlight    int64        `json:"server_max_in_flight"`
	NonRangeReqs   int          `json:"server_non_range_requests"`
	Range          RangeSummary `json:"range_requests"`
	PreTransition  int          `json:"requests_before_transition"`
	PostTransition int          `json:"requests_at_or_after_transition"`
	FirstPostS     float64      `json:"first_post_transition_elapsed_s"`
	Retries        int          `json:"aria2_retry_lines"`
	Netem          string       `json:"netem_status"`
	Err            string       `json:"err"`
	Command        string       `json:"command"`
}

type CellResult struct {
	Config  string      `json:"config"`
	Profile string      `json:"profile"`
	Runs    []RunResult `json:"runs"`
}

type Report struct {
	Aria2Version string              `json:"aria2_version"`
	FileSize     int64               `json:"file_size"`
	Repetitions  int                 `json:"repetitions"`
	Candidates   []CandidateConfig   `json:"candidates"`
	Profiles     []ValidationProfile `json:"profiles"`
	Cells        []CellResult        `json:"cells"`
	Selection    Selection           `json:"selection"`
}

type Selection struct {
	Criterion string                 `json:"criterion"`
	Selected  string                 `json:"selected_config"`
	Pooled    map[string]PooledStats `json:"pooled_by_config"`
}

type PooledStats struct {
	Count    int     `json:"count"`
	MeanMs   float64 `json:"mean_ms"`
	MedianMs float64 `json:"median_ms"`
	SDMs     float64 `json:"sd_ms"`
	MeanBps  float64 `json:"mean_wallclock_bps"`
	Retries  int     `json:"retries"`
	Failures int     `json:"failures"`
}

func main() {
	reps := flag.Int("reps", 5, "repetitions per (config, profile)")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	out := flag.String("out", filepath.Join("testdata", "diagnostics", "aria2_calibration_v2", "validation_results_v2.json"), "JSON output")
	traceOut := flag.String("trace-out", filepath.Join("testdata", "diagnostics", "aria2_calibration_v2", "request_trace_v2.json"), "request trace JSON")
	configFilter := flag.String("configs", "", "comma-separated candidate config names to run (default all)")
	profileFilter := flag.String("profiles", "", "comma-separated profile names to run (default all)")
	flag.Parse()

	if _, err := exec.LookPath("aria2c"); err != nil {
		fatal(err)
	}
	version := aria2Version()
	base := aria2BaseCommand()

	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	content := contentPattern(fileSize)

	runCandidates := filterCandidates(candidates(), *configFilter)
	runProfiles := filterProfiles(profiles(), *profileFilter)

	rep := Report{Aria2Version: version, FileSize: fileSize, Repetitions: *reps,
		Candidates: runCandidates, Profiles: runProfiles}
	for _, cfg := range runCandidates {
		for _, pr := range runProfiles {
			cell := CellResult{Config: cfg.Name, Profile: pr.Name}
			for r := 0; r < *reps; r++ {
				rr := runOne(cfg, pr, base, content, r, nc)
				cell.Runs = append(cell.Runs, rr)
				fmt.Printf("[%s %s rep %d] %s dur=%9.1fms reqs=%d ranges=%d maxInFlight=%d\n",
					cfg.Name, pr.Name, r, map[bool]string{true: "ok", false: "FAIL"}[rr.ExitOK && rr.OutputMatches],
					rr.DurationMs, rr.TotalRequests, rr.Range.Count, rr.MaxInFlight)
			}
			rep.Cells = append(rep.Cells, cell)
		}
	}
	rep.Selection = selectConfig(rep)

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*traceOut), 0o755); err != nil {
		fatal(err)
	}
	writeJSON(*out, rep)
	writeJSON(*traceOut, traceDoc(rep))
	writeText(strings.TrimSuffix(*out, filepath.Ext(*out))+".txt", rep)

	sel := candidateByName(rep.Selection.Selected)
	baseline := map[string]any{
		"label":               "FINAL ARIA2 BASELINE CONFIG v2 (16 MiB evaluation)",
		"aria2_version":       version,
		"command":             "aria2c " + strings.Join(aria2Args(base, sel), " ") + " <url>",
		"selected_config":     sel,
		"selection_criterion": rep.Selection.Criterion,
		"pooled_validation":   rep.Selection.Pooled,
		"validation_dataset":  *out,
	}
	baselineBytes := append(mustJSONValue(baseline), '\n')
	if err := os.WriteFile(filepath.Join(filepath.Dir(*out), "selected_baseline_v2.json"), baselineBytes, 0o644); err != nil {
		fatal(err)
	}
	sum := sha256.Sum256(baselineBytes)
	if err := os.WriteFile(filepath.Join(filepath.Dir(*out), "selected_baseline_v2.sha256"), []byte(hex.EncodeToString(sum[:])+"\n"), 0o644); err != nil {
		fatal(err)
	}
	writeJSON(filepath.Join(filepath.Dir(*out), "validation_config_v2.json"), map[string]any{
		"label":     "aria2 baseline calibration v2 validation set (NOT the final test set)",
		"file_size": fileSize, "repetitions": *reps,
		"candidates": rep.Candidates, "profiles": rep.Profiles,
		"note": "wall-clock transitions only; 16 MiB; 1 MiB min split",
	})
	fmt.Printf("selected %s; wrote artifacts\n", rep.Selection.Selected)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "aria2-calibration-v2:", err)
	os.Exit(1)
}

func aria2BaseCommand() []string {
	return []string{
		"--no-conf", "--file-allocation=none", "--allow-overwrite=true",
		"--auto-file-renaming=false", "--enable-color=false",
		"--summary-interval=1", "--console-log-level=notice",
		"--min-split-size=1048576", "--max-tries=3", "--retry-wait=1",
		"--timeout=30", "--connect-timeout=30",
		"--http-accept-gzip=false", "--check-certificate=false",
		"--disable-ipv6=true", "--conditional-get=false",
	}
}

func aria2Args(base []string, c CandidateConfig) []string {
	args := append([]string{}, base...)
	args = append(args, "--max-connection-per-server="+strconv.Itoa(c.MaxConns), "--split="+strconv.Itoa(c.Split))
	return args
}

func runOne(cfg CandidateConfig, pr ValidationProfile, base []string, content []byte, rep int, nc *network.Controller) RunResult {
	rr := RunResult{Config: cfg.Name, Profile: pr.Name, Rep: rep, Netem: "none"}
	contentCopy := append([]byte(nil), content...)
	srv := server.New(contentCopy)
	srv.SetRate(pr.InitialRate)
	srv.SetRequestLogEnabled(true)
	tracker := &inflightTracker{inner: srv}
	ts := httptest.NewServer(tracker)
	defer ts.Close()

	if pr.LossPercent > 0 || pr.LatencyMs > 0 {
		if err := nc.Apply("lo", network.Config{Latency: time.Duration(pr.LatencyMs) * time.Millisecond, LossPercent: pr.LossPercent}); err != nil {
			rr.Netem = "unavailable"
			rr.Err = "netem apply: " + err.Error()
			return rr
		}
		defer func() { _ = nc.Clear("lo") }()
		rr.Netem = "applied"
	}

	dir, err := os.MkdirTemp("", "aria2-cal2-")
	if err != nil {
		rr.Err = err.Error()
		return rr
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")
	args := aria2Args(base, cfg)
	args = append(args, "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
	rr.Command = "aria2c " + strings.Join(args, " ")

	start := time.Now()
	if pr.TransitionAfter > 0 {
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{InitialRate: pr.InitialRate, TransitionAfter: pr.TransitionAfter, SubsequentRate: pr.SubsequentRate}); err != nil {
			rr.Err = err.Error()
			return rr
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	outBytes, runErr := exec.CommandContext(ctx, "aria2c", args...).CombinedOutput()
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
	rr.NonRangeReqs = int(stats.TotalRequests) - len(stats.RequestedRanges)
	if rr.NonRangeReqs < 0 {
		rr.NonRangeReqs = 0
	}
	rr.Range = summarizeRanges(stats.RequestedRanges)
	rr.Retries = countRetries(string(outBytes))
	if pr.TransitionAfter > 0 {
		log := srv.RequestLog()
		if len(log) > 0 {
			t0 := log[0].Start
			for _, e := range log {
				el := e.Start.Sub(t0)
				if el >= pr.TransitionAfter {
					rr.PostTransition++
					if rr.FirstPostS == 0 {
						rr.FirstPostS = el.Seconds()
					}
				} else {
					rr.PreTransition++
				}
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

func summarizeRanges(rs []server.Range) RangeSummary {
	s := RangeSummary{Count: len(rs)}
	const maxList = 30
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

func selectConfig(rep Report) Selection {
	pooled := map[string]PooledStats{}
	for _, c := range rep.Candidates {
		var runs []RunResult
		for _, cell := range rep.Cells {
			if cell.Config == c.Name {
				runs = append(runs, cell.Runs...)
			}
		}
		pooled[c.Name] = pooledStats(runs)
	}
	names := []string{}
	for _, c := range rep.Candidates {
		names = append(names, c.Name)
	}
	if len(names) == 0 {
		return Selection{Criterion: "no candidates run", Pooled: pooled}
	}
	sel := names[0]
	for _, n := range names[1:] {
		if better(pooled[n], pooled[sel]) {
			sel = n
		}
	}
	return Selection{
		Criterion: "lowest pooled mean completion time; tie-break lower pooled median, then lower pooled retries",
		Selected:  sel, Pooled: pooled,
	}
}

func filterCandidates(all []CandidateConfig, csv string) []CandidateConfig {
	if csv == "" {
		return all
	}
	want := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []CandidateConfig
	for _, c := range all {
		if want[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

func filterProfiles(all []ValidationProfile, csv string) []ValidationProfile {
	if csv == "" {
		return all
	}
	want := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		want[strings.TrimSpace(s)] = true
	}
	var out []ValidationProfile
	for _, p := range all {
		if want[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

func better(a, b PooledStats) bool {
	if math.Abs(a.MeanMs-b.MeanMs) > 1e-9 {
		return a.MeanMs < b.MeanMs
	}
	if math.Abs(a.MedianMs-b.MedianMs) > 1e-9 {
		return a.MedianMs < b.MedianMs
	}
	return a.Retries < b.Retries
}

func pooledStats(runs []RunResult) PooledStats {
	var dur, bps []float64
	retries, failures := 0, 0
	for _, r := range runs {
		dur = append(dur, r.DurationMs)
		bps = append(bps, r.WallclockBps)
		retries += r.Retries
		if !(r.ExitOK && r.OutputMatches && r.OutputSize == fileSize) {
			failures++
		}
	}
	return PooledStats{Count: len(runs), MeanMs: mean(dur), MedianMs: median(dur),
		SDMs: stdev(dur), MeanBps: mean(bps), Retries: retries, Failures: failures}
}

func traceDoc(rep Report) any {
	var cells []any
	for _, cell := range rep.Cells {
		var runs []any
		for _, r := range cell.Runs {
			runs = append(runs, map[string]any{
				"rep": r.Rep, "exit_ok": r.ExitOK, "output_matches": r.OutputMatches,
				"total_requests": r.TotalRequests, "max_in_flight": r.MaxInFlight,
				"non_range_requests": r.NonRangeReqs, "range_count": r.Range.Count,
				"range_min": r.Range.Min, "range_max": r.Range.Max, "ranges": r.Range.List,
				"pre_transition": r.PreTransition, "post_transition": r.PostTransition,
			})
		}
		cells = append(cells, map[string]any{"config": cell.Config, "profile": cell.Profile, "runs": runs})
	}
	return map[string]any{"source": "testdata/diagnostics/aria2_calibration_v2/validation_results_v2.json", "cells": cells}
}

func writeText(path string, rep Report) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, "aria2 baseline calibration v2 (16 MiB; validation only; no performance claim)")
	fmt.Fprintf(f, "%s file=%d reps=%d\n\n", rep.Aria2Version, rep.FileSize, rep.Repetitions)
	fmt.Fprintf(f, "  %-4s %-6s %9s %9s %9s %9s %7s %6s %6s\n", "cfg", "prof", "mean_ms", "median", "sd", "ci95", "wall", "retr", "fail")
	valid, total := 0, 0
	for _, cell := range rep.Cells {
		var dur, bps []float64
		retries, failures := 0, 0
		for _, r := range cell.Runs {
			dur = append(dur, r.DurationMs)
			bps = append(bps, r.WallclockBps)
			retries += r.Retries
			total++
			if r.ExitOK && r.OutputMatches && r.OutputSize == fileSize {
				valid++
			} else {
				failures++
			}
		}
		fmt.Fprintf(f, "  %-4s %-6s %9.1f %9.1f %9.1f %9.1f %7.0f %6d %6d\n",
			cell.Config, cell.Profile, mean(dur), median(dur), stdev(dur), ci95(dur), mean(bps), retries, failures)
	}
	fmt.Fprintf(f, "\nvalid=%d/%d\n\nselection: %s\nselected=%s\n\n", valid, total, rep.Selection.Criterion, rep.Selection.Selected)
	for _, c := range rep.Candidates {
		p := rep.Selection.Pooled[c.Name]
		fmt.Fprintf(f, "  %s: mean=%.1fms median=%.1fms bps=%.0f retries=%d failures=%d\n",
			c.Name, p.MeanMs, p.MedianMs, p.MeanBps, p.Retries, p.Failures)
	}
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
	return "unknown"
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

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func candidateByName(name string) CandidateConfig {
	for _, c := range candidates() {
		if c.Name == name {
			return c
		}
	}
	return CandidateConfig{}
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

func stdev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	var sq float64
	for _, x := range v {
		sq += (x - m) * (x - m)
	}
	return math.Sqrt(sq / float64(len(v)))
}

func ci95(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	return 2.262 * stdev(v) / math.Sqrt(float64(len(v)))
}

func writeJSON(path string, v any) {
	if err := os.WriteFile(path, append(mustJSONValue(v), '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func mustJSONValue(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	return b
}
