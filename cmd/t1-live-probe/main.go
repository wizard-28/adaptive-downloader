// Command t1-live-probe runs live request-level server-throttle experiments:
// fixed-concurrency or fixed-chunk runs (and optionally an action-constrained
// oracle) over a pre-registered multi-phase server-rate schedule with
// request-level pacing. Experiment-only; no RL training; frozen artifacts
// untouched. T2 uses it with fixed C=16 and varying chunk sizes.
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
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/policy"
)

type reqEvent struct {
	StartMs  float64 `json:"start_ms"`
	EndMs    float64 `json:"end_ms"`
	Rate     int64   `json:"rate"`
	Bytes    int64   `json:"bytes"`
	HasRange bool    `json:"has_range"`
}

type runResult struct {
	Mode                 string             `json:"mode"`
	C                    int                `json:"concurrency"`
	ChunkBytes           int64              `json:"chunk_bytes"`
	FileSize             int64              `json:"file_size"`
	NormalRate           int64              `json:"normal_rate"`
	ThrottleRate         int64              `json:"throttle_rate"`
	BoundariesMs         []float64          `json:"boundaries_ms"`
	DurationMs           float64            `json:"duration_ms"`
	OutputSize           int64              `json:"output_size"`
	SHAOK                bool               `json:"sha_ok"`
	Err                  string             `json:"err"`
	Requests             []reqEvent         `json:"requests"`
	BytesByStartPhase    map[string]int64   `json:"bytes_by_start_phase"`
	RequestsByStartPhase map[string]int     `json:"requests_by_start_phase"`
	TimeByWallPhaseMs    map[string]float64 `json:"time_by_wall_phase_ms"`
	MaxInFlight          int                `json:"max_in_flight"`
}

type oraclePolicy struct {
	start    time.Time
	schedule []struct {
		at time.Duration
		c  int
	}
	initialC int
}

func (o *oraclePolicy) SelectAction(ctx policy.Context) policy.Action {
	el := time.Since(o.start)
	target := o.initialC
	for _, s := range o.schedule {
		if el >= s.at {
			target = s.c
		}
	}
	d := target - ctx.Concurrency
	if d > 4 {
		d = 4
	}
	if d < -4 {
		d = -4
	}
	if d > 0 {
		return policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: int64(d)}
	}
	if d < 0 {
		return policy.Action{Kind: policy.ActionDecreaseConcurrency, Value: int64(-d)}
	}
	return policy.NoChange()
}
func (o *oraclePolicy) Update(policy.Context, policy.Action, float64) {}

func main() {
	sizeMib := flag.Int64("size-mib", 512, "file size MiB")
	normal := flag.Int64("normal-rate", 8<<20, "normal per-response rate bytes/s")
	throttled := flag.Int64("throttle-rate", 128<<10, "throttled per-response rate bytes/s")
	t1 := flag.Float64("t1", 30, "first throttle start (s)")
	t2 := flag.Float64("t2", 75, "first recovery start (s)")
	t3 := flag.Float64("t3", 0, "second throttle start (s, 0 = none)")
	t4 := flag.Float64("t4", 0, "second recovery start (s, 0 = none)")
	cs := flag.String("cs", "1,2,4,8,12,16", "fixed concurrency values")
	chunkBytes := flag.Int64("chunk-bytes", 1<<20, "request chunk size bytes")
	timeoutS := flag.Int("timeout-s", 60, "request timeout seconds")
	oracle := flag.Bool("oracle", false, "run the fixed-C oracle (C schedule)")
	targets := flag.String("targets", "16", "oracle C schedule as time:c pairs, e.g. 0:16,30:4")
	label := flag.String("label", "t1", "run label")
	out := flag.String("out", "testdata/diagnostics/t1_server_throttle/live", "output dir")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	size := *sizeMib << 20
	content := contentPattern(size)
	expected := sha256Bytes(content)
	secs := func(d float64) time.Duration { return time.Duration(d * float64(time.Second)) }
	bounds := []float64{}
	for _, v := range []float64{*t1, *t2, *t3, *t4} {
		if v > 0 {
			bounds = append(bounds, v)
		}
	}

	run := func(mode string, c int, pol policy.Policy) runResult {
		rr := runResult{Mode: mode, C: c, ChunkBytes: *chunkBytes, FileSize: size,
			NormalRate: *normal, ThrottleRate: *throttled, BoundariesMs: []float64{},
			BytesByStartPhase: map[string]int64{}, RequestsByStartPhase: map[string]int{},
			TimeByWallPhaseMs: map[string]float64{}}
		for _, b := range bounds {
			rr.BoundariesMs = append(rr.BoundariesMs, b*1000)
		}
		srv := server.New(content)
		srv.SetRate(*normal)
		srv.SetRequestLogEnabled(true)
		ts := httptest.NewServer(srv)
		defer ts.Close()
		dir, err := os.MkdirTemp("", "t1-")
		if err != nil {
			rr.Err = err.Error()
			return rr
		}
		defer os.RemoveAll(dir)
		cfg := downloader.Config{URL: ts.URL, OutputPath: filepath.Join(dir, "out.bin"),
			TotalSize: size, ChunkSize: *chunkBytes, MinConcurrency: 1, MaxConcurrency: 16,
			Timeout: time.Duration(*timeoutS) * time.Second, MaxRetries: 3}
		if pol != nil {
			cfg.Policy = pol
			cfg.Concurrency = 1
		} else {
			cfg.Concurrency = c
			cfg.MinConcurrency = c
			cfg.MaxConcurrency = c
		}
		dl, err := downloader.New(cfg)
		if err != nil {
			rr.Err = err.Error()
			return rr
		}
		start := time.Now()
		if op, ok := pol.(*oraclePolicy); ok {
			op.start = start
		}
		stop := make(chan struct{})
		go func() {
			events := []struct {
				at   float64
				rate int64
			}{}
			if *t1 > 0 {
				events = append(events, struct {
					at   float64
					rate int64
				}{*t1, *throttled})
			}
			if *t2 > 0 {
				events = append(events, struct {
					at   float64
					rate int64
				}{*t2, *normal})
			}
			if *t3 > 0 {
				events = append(events, struct {
					at   float64
					rate int64
				}{*t3, *throttled})
			}
			if *t4 > 0 {
				events = append(events, struct {
					at   float64
					rate int64
				}{*t4, *normal})
			}
			sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })
			var last float64
			for _, e := range events {
				tm := time.NewTimer(secs(e.at - last))
				last = e.at
				select {
				case <-stop:
					tm.Stop()
					return
				case <-tm.C:
					srv.SetRate(e.rate)
				}
			}
		}()
		runErr := dl.Download(context.Background())
		rr.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
		close(stop)
		if runErr != nil {
			rr.Err = runErr.Error()
		}
		got, _ := os.ReadFile(filepath.Join(dir, "out.bin"))
		rr.OutputSize = int64(len(got))
		rr.SHAOK = sha256Bytes(got) == expected
		events := srv.RequestLog()
		type pt struct {
			t     time.Time
			delta int
		}
		var pts []pt
		phaseOf := func(ms float64) string {
			idx := 1
			for _, b := range bounds {
				if ms >= b*1000 {
					idx++
				}
			}
			return fmt.Sprintf("p%d", idx)
		}
		for _, e := range events {
			if !e.HasRange {
				continue
			}
			startOff := e.Start.Sub(start).Seconds() * 1000
			ph := phaseOf(startOff)
			rr.RequestsByStartPhase[ph]++
			rr.BytesByStartPhase[ph] += e.Range.Length()
			rr.Requests = append(rr.Requests, reqEvent{StartMs: startOff,
				EndMs: e.End.Sub(start).Seconds() * 1000, Rate: e.Rate,
				Bytes: e.Range.Length(), HasRange: true})
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
		for _, p := range pts {
			cur += p.delta
			if cur > rr.MaxInFlight {
				rr.MaxInFlight = cur
			}
		}
		dur := rr.DurationMs / 1000
		edges := append([]float64{0}, bounds...)
		for i := range edges {
			lo := edges[i]
			hi := dur
			if i+1 < len(edges) && edges[i+1] < dur {
				hi = edges[i+1]
			}
			v := hi - lo
			if v < 0 {
				v = 0
			}
			rr.TimeByWallPhaseMs[fmt.Sprintf("p%d", i+1)] = v * 1000
		}
		return rr
	}

	var results []runResult
	if *oracle {
		sched := []struct {
			at time.Duration
			c  int
		}{}
		initC := 1
		for _, part := range strings.Split(*targets, ",") {
			kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
			if len(kv) != 2 {
				continue
			}
			var at float64
			var cc int
			fmt.Sscanf(kv[0], "%f", &at)
			fmt.Sscanf(kv[1], "%d", &cc)
			if at == 0 && len(sched) == 0 {
				initC = cc
			}
			sched = append(sched, struct {
				at time.Duration
				c  int
			}{secs(at), cc})
		}
		rr := run("oracle", 0, &oraclePolicy{schedule: sched, initialC: initC})
		results = append(results, rr)
		fmt.Printf("[oracle] %.1fms bytes-by-start-phase=%v\n", rr.DurationMs, rr.BytesByStartPhase)
	} else {
		for _, s := range strings.Split(*cs, ",") {
			var c int
			fmt.Sscanf(strings.TrimSpace(s), "%d", &c)
			if c < 1 {
				continue
			}
			rr := run("fixed", c, nil)
			results = append(results, rr)
			fmt.Printf("[C%d chunk=%d] %.1fms started=%v bytes=%v sha=%v maxfl=%d\n",
				c, rr.ChunkBytes, rr.DurationMs, rr.RequestsByStartPhase, rr.BytesByStartPhase, rr.SHAOK, rr.MaxInFlight)
		}
	}
	b, _ := json.MarshalIndent(map[string]any{"label": *label, "results": results}, "", "  ")
	p := filepath.Join(*out, fmt.Sprintf("%s.json", *label))
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\n", p)
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
	fmt.Fprintln(os.Stderr, "t1-live-probe:", err)
	os.Exit(1)
}
