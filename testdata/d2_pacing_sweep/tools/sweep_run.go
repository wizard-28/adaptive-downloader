// Command sweep_run is a parameterized copy of
// testdata/aria2_competitive_benchmark/tools/bench_run.go (frozen, unchanged).
// The only change is the -phases flag, which replaces the hard-coded D2
// schedule so the pacing-schedule sensitivity sweep can run other schedules.
// Phases syntax: "R0,R1@t1,R2@t2" in KiB/s and seconds (e.g. D2 is
// "640,160@2,2500@4"; a constant 2500 KiB/s schedule is "2500").
//
// Original description: Command bench_run executes ONE aria2-competitive benchmark run on the D2
// profile (exact historical configuration: 512 MiB, per-response server pacing
// 640 KiB/s -> 160 KiB/s at 2 s -> 2500 KiB/s at 4 s, tc/netem 40 ms + 0.4%).
//
// It is a memory-safe replication of the historical harness: the deterministic
// logical server (never materialized; identical pacing semantics) is used
// instead of the 512 MiB materialized pattern, because this container does not
// have the RAM for a 1 GiB content copy per run. Both systems receive exactly
// the same generated object.
//
// Systems:
//
//	ours   fixed-policy downloader with configurable concurrency and chunk size
//	v6     the exact frozen Candidate v6 live policy (historical "our" system)
//	aria2  aria2c with caller-supplied flags (the URL is appended)
//
// Execution artifact only; no repository source is modified.
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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/rl/dqn"
)

const (
	d2InitialRate  = 640 << 10
	d2ThrottleRate = 160 << 10
	d2RecoveryRate = 2500 << 10
	phaseSeconds   = 2.0
	v6SHA          = "daa7176793093bea102965700774609fd70d4bbb60523d2417302e2a7f0d9887"
	logicalSeed    = uint64(0x9E3779B97F4A7C15)
)

type reqRecord struct {
	Index      int64 `json:"index"`
	StartMs    int64 `json:"start_ms"`
	EndMs      int64 `json:"end_ms"`
	Rate       int64 `json:"rate_bytes_per_second"`
	RangeStart int64 `json:"range_start"`
	RangeEnd   int64 `json:"range_end"`
	HasRange   bool  `json:"has_range"`
}

type result struct {
	RunID               string         `json:"run_id"`
	System              string         `json:"system"`
	Config              map[string]any `json:"config"`
	CompletionMs        float64        `json:"completion_ms"`
	WallclockBps        float64        `json:"wallclock_bytes_per_second"`
	OutputSize          int64          `json:"output_size_bytes"`
	OutputSHA256        string         `json:"output_sha256"`
	ContentVerified     bool           `json:"content_verified"`
	CoverageNoMissing   bool           `json:"coverage_no_missing"`
	CoverageNoDuplicate bool           `json:"coverage_no_duplicates"`
	NetemStatus         string         `json:"netem_status"`
	ScheduleEvents      []string       `json:"schedule_events"`
	ServerRequests      []reqRecord    `json:"server_requests"`
	TotalRequests       int64          `json:"total_requests"`
	SuccessfulReqs      int64          `json:"successful_requests"`
	FailedReqs          int64          `json:"failed_requests"`
	ServerBytes         int64          `json:"server_bytes"`
	RangeCount          int            `json:"range_count"`
	Aria2Exit           int            `json:"aria2_exit"`
	Aria2Tail           string         `json:"aria2_output_tail"`
	ClientRequests      int64          `json:"client_requests"`
	ClientSuccess       int64          `json:"client_successful"`
	ClientFailed        int64          `json:"client_failed"`
	ClientRetries       int64          `json:"client_retries"`
	ClientBytes         int64          `json:"client_bytes"`
	Err                 string         `json:"err"`
}

// mixLogicalWord/fillLogical replicate internal/experiment/server/logical.go
// (splitmix64 finalizer) for output verification only.
func mixLogicalWord(word uint64) uint64 {
	z := word*0x9E3779B97F4A7C15 + logicalSeed
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func fillLogical(dst []byte, off int64) {
	for len(dst) > 0 {
		z := mixLogicalWord(uint64(off >> 3))
		shift := uint(off&7) * 8
		n := 8 - int(off&7)
		if n > len(dst) {
			n = len(dst)
		}
		for i := 0; i < n; i++ {
			dst[i] = byte(z >> (shift + uint(i)*8))
		}
		dst = dst[n:]
		off += int64(n)
	}
}

func verifyContent(path string, size int64) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	exp := make([]byte, 1<<20)
	var off int64
	for off < size {
		n, err := f.Read(buf)
		if n > 0 {
			fillLogical(exp[:n], off)
			h.Write(buf[:n])
			if !equalBytes(buf[:n], exp[:n]) {
				return false, hex.EncodeToString(h.Sum(nil))
			}
			off += int64(n)
		}
		if err != nil {
			break
		}
	}
	return off == size, hex.EncodeToString(h.Sum(nil))
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func main() {
	system := flag.String("system", "", "ours|v6|aria2")
	c := flag.Int("c", 4, "ours: concurrency")
	chunk := flag.Int64("chunk", 1<<20, "ours: chunk size bytes")
	modelPath := flag.String("model", "testdata/diagnostics/dqn_validation_v6/final_dqn_candidate_v6.json", "v6 model")
	aria2Args := flag.String("aria2-args", "", "aria2 flags (URL is appended)")
	fileSize := flag.Int64("file-size", 512<<20, "file size bytes")
	out := flag.String("out", "", "result JSON path")
	runID := flag.String("run-id", "", "run identifier")
	useSudo := flag.Bool("sudo", true, "apply tc/netem via sudo -n")
	schedule := flag.String("schedule", "d2", "d2 (phase-locked) or constant")
	phasesFlag := flag.String("phases", "", "rate schedule 'R0,R1@t1,...' in KiB/s and seconds; overrides -schedule")
	constRate := flag.Int64("const-rate", 2500<<10, "constant schedule rate (bytes/s)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	aria2Timeout := flag.Duration("aria2-timeout", 180*time.Second, "aria2 process timeout")
	retries := flag.Int("retries", 3, "max retries")
	verify := flag.Bool("verify", true, "verify output content against the logical generator")
	flag.Parse()
	if *system == "" || *out == "" || *runID == "" {
		fmt.Fprintln(os.Stderr, "bench_run: -system, -out, -run-id are required")
		os.Exit(2)
	}

	res := result{RunID: *runID, System: *system, NetemStatus: "applied"}
	res.Config = map[string]any{"file_size_bytes": *fileSize, "system": *system}
	if *system == "ours" {
		res.Config["concurrency"] = *c
		res.Config["chunk_bytes"] = *chunk
		res.Config["timeout_ms"] = timeout.Milliseconds()
		res.Config["max_retries"] = *retries
	}
	if *system == "v6" {
		res.Config["concurrency_bounds"] = []int{1, 16}
		res.Config["chunk_bytes"] = 1 << 20
		res.Config["model"] = *modelPath
	}
	if *system == "aria2" {
		res.Config["aria2_args"] = *aria2Args
	}

	srv, err := server.NewLogical(*fileSize)
	if err != nil {
		fail(res, *out, "server: "+err.Error())
	}
	type phase struct {
		at   time.Duration
		rate int64
	}
	initialRate := int64(d2InitialRate)
	var phases []phase
	if *phasesFlag != "" {
		for i, tok := range strings.Split(*phasesFlag, ",") {
			var kib int64
			var at float64
			if i == 0 {
				if _, err := fmt.Sscanf(tok, "%d", &kib); err != nil {
					fail(res, *out, "bad -phases initial rate: "+tok)
				}
				initialRate = kib << 10
				continue
			}
			if _, err := fmt.Sscanf(tok, "%d@%f", &kib, &at); err != nil {
				fail(res, *out, "bad -phases token: "+tok)
			}
			phases = append(phases, phase{time.Duration(at * float64(time.Second)), kib << 10})
		}
		res.Config["phases"] = *phasesFlag
	} else if *schedule == "d2" {
		phases = []phase{{2 * time.Second, d2ThrottleRate}, {4 * time.Second, d2RecoveryRate}}
	}
	srv.SetRate(initialRate)
	srv.SetRequestLogEnabled(true)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	nc := network.NewController()
	if *useSudo {
		nc = &network.Controller{Prefix: []string{"sudo", "-n"}}
	}
	iface, err := nc.InterfaceForHost("127.0.0.1")
	if err != nil {
		fail(res, *out, "interface: "+err.Error())
	}
	if err := nc.Apply(iface, network.Config{Latency: 40 * time.Millisecond, LossPercent: 0.4}); err != nil {
		res.NetemStatus = "unavailable"
		fail(res, *out, "netem apply: "+err.Error())
	}
	defer func() { _ = nc.Clear(iface) }()

	dir, err := os.MkdirTemp("", "aria2-comp-")
	if err != nil {
		fail(res, *out, err.Error())
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	stop := make(chan struct{})
	events := make(chan string, 4)
	start := time.Now()
	if *phasesFlag == "" && *schedule == "constant" {
		srv.SetRate(*constRate)
		res.Config["schedule"] = "constant"
		res.Config["constant_rate"] = *constRate
	}
	go func() {
		for _, ph := range phases {
			wait := ph.at - time.Since(start)
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-stop:
					timer.Stop()
					return
				}
			}
			srv.SetRate(ph.rate)
			events <- fmt.Sprintf("rate=%d at %s", ph.rate, ph.at)
		}
	}()

	var runErr error
	switch *system {
	case "ours":
		dl, derr := downloader.New(downloader.Config{
			URL: ts.URL, OutputPath: outPath, TotalSize: *fileSize,
			Concurrency: *c, ChunkSize: *chunk, Timeout: *timeout, MaxRetries: *retries,
		})
		if derr != nil {
			runErr = derr
			break
		}
		runErr = dl.Download(context.Background())
		snap := dl.Metrics()
		res.ClientRequests = snap.Aggregate.TotalRequests
		res.ClientSuccess = snap.Aggregate.SuccessfulRequests
		res.ClientFailed = snap.Aggregate.FailedRequests
		res.ClientBytes = snap.Aggregate.TotalBytes
		for _, r := range snap.Records {
			if r.Attempt > 1 {
				res.ClientRetries++
			}
		}
	case "v6":
		gotSHA, serr := fileSHA(*modelPath)
		if serr != nil || gotSHA != v6SHA {
			runErr = fmt.Errorf("v6 model hash mismatch: %s (want %s)", gotSHA, v6SHA)
			break
		}
		model, merr := dqn.LoadModel(*modelPath)
		if merr != nil {
			runErr = merr
			break
		}
		lp, perr := dqn.NewLiveConcurrencyPolicyV6(model, *fileSize)
		if perr != nil {
			runErr = perr
			break
		}
		dl, derr := downloader.New(downloader.Config{
			URL: ts.URL, OutputPath: outPath, TotalSize: *fileSize,
			Concurrency: 1, ChunkSize: 1 << 20,
			MinConcurrency: 1, MaxConcurrency: 16,
			Timeout: *timeout, MaxRetries: *retries, PolicyWindowSize: 4, Policy: lp,
		})
		if derr != nil {
			runErr = derr
			break
		}
		runErr = dl.Download(context.Background())
		snap := dl.Metrics()
		res.ClientRequests = snap.Aggregate.TotalRequests
		res.ClientSuccess = snap.Aggregate.SuccessfulRequests
		res.ClientFailed = snap.Aggregate.FailedRequests
		res.ClientBytes = snap.Aggregate.TotalBytes
		for _, r := range snap.Records {
			if r.Attempt > 1 {
				res.ClientRetries++
			}
		}
	case "aria2":
		args := strings.Fields(*aria2Args)
		args = append(args, "-d", dir, "-o", "out.bin", "--interface=lo", ts.URL)
		ctx, cancel := context.WithTimeout(context.Background(), *aria2Timeout)
		defer cancel()
		outBytes, aerr := exec.CommandContext(ctx, "aria2c", args...).CombinedOutput()
		if aerr != nil {
			runErr = fmt.Errorf("aria2: %v", aerr)
		}
		if ee, ok := aerr.(*exec.ExitError); ok {
			res.Aria2Exit = ee.ExitCode()
		}
		tail := string(outBytes)
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		res.Aria2Tail = tail
	default:
		fail(res, *out, "unknown system "+*system)
	}

	res.CompletionMs = float64(time.Since(start).Microseconds()) / 1000.0
	close(stop)
	close(events)
	for e := range events {
		res.ScheduleEvents = append(res.ScheduleEvents, e)
	}
	if runErr != nil {
		res.Err = runErr.Error()
	}
	if st, serr := os.Stat(outPath); serr == nil {
		res.OutputSize = st.Size()
	}
	if *verify && res.OutputSize == *fileSize {
		ok, hash := verifyContent(outPath, *fileSize)
		res.ContentVerified = ok
		res.OutputSHA256 = hash
	}

	for _, e := range srv.RequestLog() {
		res.ServerRequests = append(res.ServerRequests, reqRecord{
			Index: e.Index, StartMs: e.Start.Sub(start).Milliseconds(),
			EndMs: e.End.Sub(start).Milliseconds(), Rate: e.Rate,
			RangeStart: e.Range.Start, RangeEnd: e.Range.End, HasRange: e.HasRange,
		})
	}
	st := srv.Stats()
	res.TotalRequests = st.TotalRequests
	res.SuccessfulReqs = st.Successful
	res.FailedReqs = st.Failed
	res.ServerBytes = st.BytesServed
	res.RangeCount = len(st.RequestedRanges)
	res.CoverageNoMissing, res.CoverageNoDuplicate = coverage(res.ServerRequests, *fileSize)
	if res.CompletionMs > 0 {
		res.WallclockBps = float64(*fileSize) / (res.CompletionMs / 1000.0)
	}

	data, _ := json.MarshalIndent(res, "", "  ")
	if werr := os.WriteFile(*out, append(data, '\n'), 0o644); werr != nil {
		fmt.Fprintln(os.Stderr, "bench_run: write result:", werr)
		os.Exit(2)
	}
	fmt.Printf("run %s system=%s completion=%.3fs output=%d verified=%v err=%q\n",
		*runID, *system, res.CompletionMs/1000, res.OutputSize, res.ContentVerified, res.Err)
	if res.Err != "" || res.OutputSize != *fileSize {
		os.Exit(3)
	}
}

func coverage(reqs []reqRecord, size int64) (noMissing, noDup bool) {
	type seg struct{ s, e int64 }
	var segs []seg
	for _, r := range reqs {
		if r.HasRange {
			segs = append(segs, seg{r.RangeStart, r.RangeEnd})
		}
	}
	// sort by start
	for i := 1; i < len(segs); i++ {
		for j := i; j > 0 && segs[j].s < segs[j-1].s; j-- {
			segs[j], segs[j-1] = segs[j-1], segs[j]
		}
	}
	noMissing, noDup = true, true
	prev := int64(-1)
	for _, s := range segs {
		if s.s <= prev {
			noDup = false
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

func fileSHA(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func fail(res result, out, msg string) {
	res.Err = msg
	data, _ := json.MarshalIndent(res, "", "  ")
	if werr := os.WriteFile(out, append(data, '\n'), 0o644); werr != nil {
		fmt.Fprintln(os.Stderr, "bench_run: write result:", werr)
	}
	fmt.Fprintln(os.Stderr, "bench_run:", msg)
	os.Exit(3)
}
