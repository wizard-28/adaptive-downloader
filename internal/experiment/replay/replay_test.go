package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/server"
)

// TestMMChildHelper is the child entry point. It is inert unless the parent
// harness sets REPLAY_CHILD=1 in the child environment (the mm-link child is a
// new instance of this test binary).
func TestMMChildHelper(t *testing.T) {
	if os.Getenv("REPLAY_CHILD") != "1" {
		t.Skip("mm-link child helper (inert outside the replay harness)")
	}
	atoi := func(k string) int { v, _ := strconv.Atoi(os.Getenv(k)); return v }
	atoi64 := func(k string) int64 { v, _ := strconv.ParseInt(os.Getenv(k), 10, 64); return v }
	dur := func(k string) time.Duration { v, _ := time.ParseDuration(os.Getenv(k)); return v }
	deadline := func(k string) (time.Duration, bool) {
		v := os.Getenv(k)
		if v == "" {
			return 0, false
		}
		d, none, err := ParseDeadline(v)
		if err != nil {
			fmt.Fprintln(os.Stderr, "replay child:", err)
			os.Exit(2)
		}
		return d, none
	}
	requestTimeout, noRequestTimeout := deadline("REPLAY_TIMEOUT")
	dialTimeout, noDialTimeout := deadline("REPLAY_DIAL_TIMEOUT")

	_, err := RunDownloaderChild(context.Background(), ChildConfig{
		Port:             atoi("REPLAY_PORT"),
		OutputPath:       os.Getenv("REPLAY_OUTPUT"),
		SummaryPath:      os.Getenv("REPLAY_SUMMARY"),
		EventsPath:       os.Getenv("REPLAY_EVENTS"),
		TotalSize:        atoi64("REPLAY_TOTAL"),
		ChunkSize:        atoi64("REPLAY_CHUNK"),
		Concurrency:      atoi("REPLAY_C"),
		Timeout:          requestTimeout,
		NoRequestTimeout: noRequestTimeout,
		DialTimeout:      dialTimeout,
		NoDialTimeout:    noDialTimeout,
		MaxRuntime:       dur("REPLAY_MAX"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay child:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// childBuilder returns a Config.Child callback that runs this test binary in
// child mode with the given download configuration.
func childBuilder(t *testing.T, total, chunk int64, concurrency int, output, summary string) func(int) ([]string, []string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return func(port int) ([]string, []string) {
		argv := []string{exe, "-test.run=^TestMMChildHelper$"}
		env := []string{
			"REPLAY_CHILD=1",
			"REPLAY_PORT=" + strconv.Itoa(port),
			"REPLAY_OUTPUT=" + output,
			"REPLAY_SUMMARY=" + summary,
			"REPLAY_TOTAL=" + strconv.FormatInt(total, 10),
			"REPLAY_CHUNK=" + strconv.FormatInt(chunk, 10),
			"REPLAY_C=" + strconv.Itoa(concurrency),
			"REPLAY_TIMEOUT=none",
			"REPLAY_DIAL_TIMEOUT=none",
			"REPLAY_MAX=2m",
		}
		return argv, env
	}
}

func writeTrace(t *testing.T, path string, stamps []int64) {
	t.Helper()
	var b strings.Builder
	for _, s := range stamps {
		b.WriteString(strconv.FormatInt(s, 10))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
}

// activeGapTrace is a short active/gap/active synthetic trace (0-99 ms active,
// 100-199 ms gap, 200-299 ms active), one delivery opportunity per ms.
func activeGapTrace() []int64 {
	var s []int64
	for i := int64(0); i <= 99; i++ {
		s = append(s, i)
	}
	for i := int64(200); i <= 299; i++ {
		s = append(s, i)
	}
	return s
}

// denseTrace repeats each millisecond perMS times, giving a high-capacity
// short trace (50 packets/ms ~= 75 MB/s at 1500 B/packet).
func denseTrace(ms, perMS int) []int64 {
	var s []int64
	for i := 0; i < ms; i++ {
		for j := 0; j < perMS; j++ {
			s = append(s, int64(i))
		}
	}
	return s
}

func TestReadTrace(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.link")
	writeTrace(t, good, []int64{0, 1, 2, 5})
	info, err := ReadTrace(good)
	if err != nil {
		t.Fatalf("ReadTrace: %v", err)
	}
	if info.Lines != 4 || info.Duration != 5*time.Millisecond {
		t.Fatalf("info = %+v, want 4 lines / 5ms", info)
	}

	mixed := filepath.Join(dir, "mixed.link")
	if err := os.WriteFile(mixed, []byte("# comment\n\n0\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err = ReadTrace(mixed)
	if err != nil {
		t.Fatalf("ReadTrace(mixed): %v", err)
	}
	if info.Lines != 2 || info.Duration != 3*time.Millisecond {
		t.Fatalf("mixed info = %+v, want 2 lines / 3ms", info)
	}

	for name, content := range map[string]string{
		"malformed":     "0\nnope\n",
		"negative":      "0\n-4\n",
		"empty":         "\n\n",
		"zero duration": "0\n0\n",
	} {
		path := filepath.Join(dir, name+".link")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadTrace(path); err == nil {
			t.Errorf("ReadTrace(%s) = nil error, want failure", name)
		}
	}
	if _, err := ReadTrace(filepath.Join(dir, "missing.link")); err == nil {
		t.Error("ReadTrace(missing) = nil error, want failure")
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "t.link")
	writeTrace(t, trace, []int64{0, 1})
	base := Config{
		UpTrace:      trace,
		DownTrace:    trace,
		WorkloadSize: 1 << 20,
		ChunkSize:    1 << 18,
		Concurrency:  1,
		MaxRuntime:   time.Minute,
		Child: func(int) ([]string, []string) {
			return []string{"/bin/true"}, nil
		},
	}
	cases := map[string]func(*Config){
		"missing trace":    func(c *Config) { c.UpTrace = "" },
		"bad workload":     func(c *Config) { c.WorkloadSize = 0 },
		"bad chunk":        func(c *Config) { c.ChunkSize = 0 },
		"bad concurrency":  func(c *Config) { c.Concurrency = 0 },
		"no watchdog":      func(c *Config) { c.MaxRuntime = 0 },
		"no child builder": func(c *Config) { c.Child = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			res, err := Run(context.Background(), cfg)
			if err == nil {
				t.Fatal("err = nil, want configuration failure")
			}
			if res.Outcome != OutcomeInvalidConfiguration {
				t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeInvalidConfiguration)
			}
		})
	}
}

func TestRunMMLinkStartFailed(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "t.link")
	writeTrace(t, trace, []int64{0, 50})
	res, err := Run(context.Background(), Config{
		UpTrace:      trace,
		DownTrace:    trace,
		WorkloadSize: 1 << 20,
		ChunkSize:    1 << 18,
		Concurrency:  1,
		MaxRuntime:   time.Minute,
		MMLinkBinary: filepath.Join(dir, "no-such-mm-link"),
		Child: func(int) ([]string, []string) {
			return []string{"/bin/true"}, nil
		},
	})
	if err == nil {
		t.Fatal("err = nil, want start failure")
	}
	if res.Outcome != OutcomeMMLinkStartFailed {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeMMLinkStartFailed)
	}
}

func TestRunDownloaderStartFailed(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "t.link")
	writeTrace(t, trace, []int64{0, 50})
	res, err := Run(context.Background(), Config{
		UpTrace:      trace,
		DownTrace:    trace,
		WorkloadSize: 1 << 20,
		ChunkSize:    1 << 18,
		Concurrency:  1,
		MaxRuntime:   30 * time.Second,
		Child: func(int) ([]string, []string) {
			return []string{filepath.Join(dir, "no-such-child")}, nil
		},
	})
	if err == nil {
		t.Fatal("err = nil, want child start failure")
	}
	if res.Outcome != OutcomeDownloaderStartFailed {
		t.Fatalf("outcome = %s, want %s (output: %s)", res.Outcome, OutcomeDownloaderStartFailed, res.MMLink.Output)
	}
}

func TestRunWatchdogTimeout(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "t.link")
	writeTrace(t, trace, []int64{0, 50})
	start := time.Now()
	res, err := Run(context.Background(), Config{
		UpTrace:          trace,
		DownTrace:        trace,
		WorkloadSize:     1 << 20,
		ChunkSize:        1 << 18,
		Concurrency:      1,
		MaxRuntime:       800 * time.Millisecond,
		TerminationGrace: time.Second,
		ServerAddr:       "127.0.0.1:0",
		Child: func(int) ([]string, []string) {
			// A child that defers mm-link's EOF signal makes mm-link wait,
			// which is exactly what the independent watchdog must cover.
			return []string{"/bin/sh", "-c", "trap : TERM HUP INT; sleep 30"}, nil
		},
	})
	if err == nil {
		t.Fatal("err = nil, want watchdog error")
	}
	if res.Outcome != OutcomeWatchdogTimeout {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeWatchdogTimeout)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("watchdog took %v to return", elapsed)
	}
	if len(res.MMLink.LeftoverPIDs) != 0 {
		t.Fatalf("leftover pids after watchdog: %v", res.MMLink.LeftoverPIDs)
	}
	assertNoMMLinkProcess(t)
}

func TestRunDownloaderChildAgainstLogicalServer(t *testing.T) {
	const total = int64(1 << 20)
	srv, err := server.NewLogical(total)
	if err != nil {
		t.Fatalf("NewLogical: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	sum, err := RunDownloaderChild(context.Background(), ChildConfig{
		Host:        "127.0.0.1",
		Port:        port,
		OutputPath:  filepath.Join(dir, "out.bin"),
		SummaryPath: filepath.Join(dir, "summary.json"),
		TotalSize:   total,
		ChunkSize:   256 << 10,
		Concurrency: 2,
		Timeout:     10 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunDownloaderChild: %v", err)
	}
	if !sum.WorkloadExhausted {
		t.Fatalf("workload not exhausted: %+v", sum)
	}
	if sum.BytesReceived != total || sum.Successful != 4 || sum.Events < 4 {
		t.Fatalf("summary = %+v, want %d bytes and 4 successes", sum, total)
	}
	if sum.DistinctConnections < 1 || sum.DistinctConnections > 2 {
		t.Fatalf("distinct connections = %d, want 1..2", sum.DistinctConnections)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", total-1))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	want, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("downloaded output does not match the logical object")
	}
}

func TestRunSmokeConcurrencies(t *testing.T) {
	before := networkSnapshot(t)
	trace := activeGapTrace()

	for _, c := range []int{1, 4, 16} {
		c := c
		t.Run(fmt.Sprintf("C=%d", c), func(t *testing.T) {
			dir := t.TempDir()
			up := filepath.Join(dir, "up.link")
			down := filepath.Join(dir, "down.link")
			writeTrace(t, up, trace)
			writeTrace(t, down, trace)
			summary := filepath.Join(dir, "summary.json")
			output := filepath.Join(dir, "out.bin")
			downLog := filepath.Join(dir, "down.log")

			res, err := Run(context.Background(), Config{
				UpTrace:          up,
				DownTrace:        down,
				WorkloadSize:     128 << 20,
				ChunkSize:        256 << 10,
				Concurrency:      c,
				Timeout:          10 * time.Second,
				MaxRuntime:       60 * time.Second,
				TerminationGrace: 2 * time.Second,
				Child:            childBuilder(t, 128<<20, 256<<10, c, output, summary),
				SummaryPath:      summary,
				ServerAddr:       "0.0.0.0:0",
				DownlinkLogPath:  downLog,
			})
			if res.Outcome != OutcomeSuccessTraceEOF {
				t.Fatalf("outcome = %s err = %v endpoint=%v residual=%v\nmm-link output:\n%s",
					res.Outcome, err, res.EndpointValid, res.TraceEndpointResidual, res.MMLink.Output)
			}
			if !res.EndpointValid {
				t.Fatalf("endpoint evidence invalid: expected=%v observed=%v residual=%v",
					res.TraceExpectedDuration, res.TraceObservedEndpoint, res.TraceEndpointResidual)
			}
			if res.ChildSummary == nil {
				t.Fatal("no child summary written")
			}
			if res.ChildSummary.BytesReceived <= 0 {
				t.Fatalf("no incremental bytes received: %+v", res.ChildSummary)
			}
			if res.ChildSummary.WorkloadExhausted {
				t.Fatalf("workload exhausted during a short trace: %+v", res.ChildSummary)
			}
			// Trace EOF cancels the outstanding request through the child
			// context; with no request deadline configured this must be a
			// non-fatal harness cancellation, and the run must still be EOF.
			if !res.ChildSummary.Canceled || res.ChildSummary.Fatal {
				t.Fatalf("child summary = %+v, want canceled non-fatal at trace EOF", res.ChildSummary)
			}
			// At higher C, requests can still be in flight at trace EOF, so
			// only require that the downloader actually issued Range requests;
			// partial bytes are proven by ChildSummary.BytesReceived above.
			if res.ServerStats.TotalRequests < 1 {
				t.Fatalf("server saw no requests: %+v", res.ServerStats)
			}
			if n := res.ChildSummary.DistinctConnections; n < 1 || n > c {
				t.Fatalf("distinct connections = %d, want 1..%d", n, c)
			}
			if len(res.MMLink.LeftoverPIDs) != 0 {
				t.Fatalf("leftover pids: %v", res.MMLink.LeftoverPIDs)
			}
			assertNoMMLinkProcess(t)
		})
	}

	after := networkSnapshot(t)
	if before != after {
		t.Errorf("host network state changed across smoke runs:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestRunWorkloadExhausted(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "up.link")
	down := filepath.Join(dir, "down.link")
	trace := denseTrace(2000, 50)
	writeTrace(t, up, trace)
	writeTrace(t, down, trace)
	summary := filepath.Join(dir, "summary.json")
	output := filepath.Join(dir, "out.bin")
	const total = int64(1 << 20)

	res, err := Run(context.Background(), Config{
		UpTrace:          up,
		DownTrace:        down,
		WorkloadSize:     total,
		ChunkSize:        256 << 10,
		Concurrency:      4,
		Timeout:          10 * time.Second,
		MaxRuntime:       60 * time.Second,
		TerminationGrace: 2 * time.Second,
		Child:            childBuilder(t, total, 256<<10, 4, output, summary),
		SummaryPath:      summary,
		ServerAddr:       "0.0.0.0:0",
	})
	if err != nil && res.Outcome != OutcomeWorkloadExhausted {
		t.Fatalf("outcome = %s err = %v\nmm-link output:\n%s", res.Outcome, err, res.MMLink.Output)
	}
	if res.Outcome != OutcomeWorkloadExhausted {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeWorkloadExhausted)
	}
	if res.ChildSummary == nil || !res.ChildSummary.WorkloadExhausted {
		t.Fatalf("summary = %+v, want workload exhausted", res.ChildSummary)
	}
	if res.ChildSummary.BytesReceived != total {
		t.Fatalf("bytes received = %d, want %d", res.ChildSummary.BytesReceived, total)
	}
	assertNoMMLinkProcess(t)
}

func TestExitCodesAreDistinct(t *testing.T) {
	outcomes := []Outcome{
		OutcomeSuccessTraceEOF,
		OutcomeInvalidConfiguration,
		OutcomeMMLinkStartFailed,
		OutcomeDownloaderStartFailed,
		OutcomeWatchdogTimeout,
		OutcomeWorkloadExhausted,
		OutcomeCleanupFailed,
		OutcomeDownloaderExited,
		OutcomeSummaryMissing,
		OutcomeCanceled,
		OutcomeDownloaderFatalError,
		OutcomeEndpointNotReached,
	}
	seen := make(map[int]Outcome, len(outcomes))
	for _, o := range outcomes {
		code := ExitCode(o)
		if prev, ok := seen[code]; ok {
			t.Errorf("outcomes %s and %s share exit code %d", prev, o, code)
		}
		seen[code] = o
	}
	if got := ExitCode(OutcomeSuccessTraceEOF); got != 0 {
		t.Errorf("success exit code = %d, want 0", got)
	}
}

func assertNoMMLinkProcess(t *testing.T) {
	t.Helper()
	out, err := exec.Command("pgrep", "-x", "mm-link").Output()
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		t.Fatalf("mm-link processes remain: %s", out)
	}
}

func networkSnapshot(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	cmds := [][]string{
		{"ip", "-o", "link", "show"},
		{"ip", "-o", "addr", "show"},
		{"tc", "qdisc", "show"},
		{"ip", "netns", "list"},
	}
	for _, c := range cmds {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			t.Logf("network snapshot %v failed: %v (%s)", c, err, out)
			continue
		}
		fmt.Fprintf(&b, "$ %s\n%s\n", strings.Join(c, " "), out)
	}
	return b.String()
}

// writeChildSummaryFile writes a child summary exactly as the child would.
func writeChildSummaryFile(t *testing.T, path string, s ChildSummary) {
	t.Helper()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
}

// writeDownlinkLog writes a minimal mm-link --downlink-log with one packet at
// trace-ms 0 and one at lastMs. Header lines and non-'#' records are ignored
// by ParseTraceLog, matching the real format.
func writeDownlinkLog(t *testing.T, path string, lastMs int64) {
	t.Helper()
	content := fmt.Sprintf("# mahimahi mm-link (Downlink)\n0 # 1504\n%d # 1504\n%d + 64\n",
		lastMs, lastMs)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write downlink log: %v", err)
	}
}

// writeFakeMMLink writes an executable /bin/sh script used instead of the real
// mm-link in the endpoint-classification tests.
func writeFakeMMLink(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-mm-link")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake mm-link: %v", err)
	}
	return path
}

// TestEndpointResidualArithmetic covers the downlink endpoint residual and
// tolerance boundary, including missing evidence.
func TestEndpointResidualArithmetic(t *testing.T) {
	cases := []struct {
		name      string
		expected  time.Duration
		observed  time.Duration
		have      bool
		wantResid time.Duration
		wantValid bool
	}{
		{"exact", 1000 * time.Millisecond, 1000 * time.Millisecond, true, 0, true},
		{"inside tolerance", 1000 * time.Millisecond, 950 * time.Millisecond, true, 50 * time.Millisecond, true},
		{"boundary minus", 1000 * time.Millisecond, 900 * time.Millisecond, true, 100 * time.Millisecond, true},
		{"outside tolerance", 1000 * time.Millisecond, 899 * time.Millisecond, true, 101 * time.Millisecond, false},
		{"observed beyond", 1000 * time.Millisecond, 1050 * time.Millisecond, true, -50 * time.Millisecond, true},
		{"observed beyond boundary", 1000 * time.Millisecond, 1100 * time.Millisecond, true, -100 * time.Millisecond, true},
		{"observed beyond outside", 1000 * time.Millisecond, 1101 * time.Millisecond, true, -101 * time.Millisecond, false},
		{"missing evidence", 1000 * time.Millisecond, 0, false, 1000 * time.Millisecond, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			residual, valid := endpointEvidence(tc.expected, tc.observed, tc.have)
			if residual != tc.wantResid {
				t.Fatalf("residual = %v, want %v", residual, tc.wantResid)
			}
			if valid != tc.wantValid {
				t.Fatalf("valid = %v, want %v", valid, tc.wantValid)
			}
		})
	}
}

// TestSyntheticEarlyReplayCasesAtoD drives the harness with a deterministic
// fake mm-link and pre-written child summaries/logs. Together with
// TestSyntheticEarlyReplayCaseE and TestSyntheticEarlyReplayCaseF it covers
// the required synthetic cases A–F:
//
//	A. real trace EOF + no fatal child error + endpoint reached -> EOF success;
//	B. early (clean) child exit before the endpoint + mm-link rc=0 -> NOT EOF;
//	C. fatal downloader child + mm-link rc=0 -> NOT EOF;
//	D. mm-link rc=0 + no endpoint evidence -> NOT EOF;
//	E. per-request timeout then successful retry -> child success;
//	F. per-request timeouts + retry exhaustion -> child non-zero, replay NOT EOF.
func TestSyntheticEarlyReplayCasesAtoD(t *testing.T) {
	const (
		traceMs  = int64(200)
		duration = 200 * time.Millisecond
	)
	cases := []struct {
		name        string
		summary     ChildSummary
		writeLog    bool
		logLastMs   int64
		wantOutcome Outcome
		wantErr     bool
		wantStatus  ChildStatus
		wantValid   bool
		wantObserve time.Duration
		wantResid   time.Duration
	}{
		{
			name: "A_real_eof_clean_child",
			summary: ChildSummary{
				Final: true, Canceled: true, Err: "context canceled",
				Requests: 3, Successful: 2,
			},
			writeLog: true, logLastMs: traceMs,
			wantOutcome: OutcomeSuccessTraceEOF, wantStatus: ChildStatusCanceled,
			wantValid: true, wantObserve: duration, wantResid: 0,
		},
		{
			name: "B_early_clean_child_exit",
			summary: ChildSummary{
				Final: true, Requests: 1, Successful: 0,
			},
			writeLog: true, logLastMs: 40,
			wantOutcome: OutcomeEndpointNotReached, wantErr: true,
			wantStatus: ChildStatusClean, wantValid: false,
			wantObserve: 40 * time.Millisecond, wantResid: 160 * time.Millisecond,
		},
		{
			name: "C_fatal_child_with_mmlink_rc0",
			summary: ChildSummary{
				Final: true, Fatal: true,
				Err:      "downloader: retry limit exceeded: range [0,1048575] after 2 attempts",
				Requests: 2, Failed: 2,
			},
			writeLog: true, logLastMs: traceMs,
			wantOutcome: OutcomeDownloaderFatalError, wantErr: true,
			wantStatus: ChildStatusFatal, wantValid: true,
			wantObserve: duration, wantResid: 0,
		},
		{
			name: "D_missing_endpoint_evidence",
			summary: ChildSummary{
				Final: true, Canceled: true, Err: "context canceled", Requests: 2,
			},
			writeLog:    false,
			wantOutcome: OutcomeEndpointNotReached, wantErr: true,
			wantStatus: ChildStatusCanceled, wantValid: false,
			wantObserve: 0, wantResid: duration,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			up := filepath.Join(dir, "t.up")
			down := filepath.Join(dir, "t.down")
			writeTrace(t, up, []int64{0, 100, traceMs})
			writeTrace(t, down, []int64{0, 100, traceMs})
			summary := filepath.Join(dir, "summary.json")
			writeChildSummaryFile(t, summary, tc.summary)
			downLog := filepath.Join(dir, "down.log")
			if tc.writeLog {
				writeDownlinkLog(t, downLog, tc.logLastMs)
			}
			fake := writeFakeMMLink(t, dir, "exit 0")

			res, err := Run(context.Background(), Config{
				UpTrace: up, DownTrace: down,
				WorkloadSize: 1 << 20, ChunkSize: 1 << 18, Concurrency: 1,
				MaxRuntime:   10 * time.Second,
				MMLinkBinary: fake,
				SummaryPath:  summary,
				ServerAddr:   "127.0.0.1:0",
				Child: func(int) ([]string, []string) {
					return []string{"/bin/true"}, nil
				},
				DownlinkLogPath: downLog,
			})

			if res.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %s, want %s (err=%v)", res.Outcome, tc.wantOutcome, err)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr=%v", err, tc.wantErr)
			}
			if got := ExitCode(res.Outcome); got != ExitCode(tc.wantOutcome) {
				t.Fatalf("exit code = %d, want %d", got, ExitCode(tc.wantOutcome))
			}
			if res.TraceExpectedDuration != duration {
				t.Fatalf("expected duration = %v, want %v", res.TraceExpectedDuration, duration)
			}
			if res.TraceObservedEndpoint != tc.wantObserve {
				t.Fatalf("observed endpoint = %v, want %v", res.TraceObservedEndpoint, tc.wantObserve)
			}
			if res.TraceEndpointResidual != tc.wantResid {
				t.Fatalf("residual = %v, want %v", res.TraceEndpointResidual, tc.wantResid)
			}
			if res.EndpointValid != tc.wantValid {
				t.Fatalf("EndpointValid = %v, want %v", res.EndpointValid, tc.wantValid)
			}
			if res.MMLinkExit != 0 {
				t.Fatalf("MMLinkExit = %d, want 0 (fake mm-link)", res.MMLinkExit)
			}
			if res.ChildStatus != tc.wantStatus {
				t.Fatalf("ChildStatus = %s, want %s", res.ChildStatus, tc.wantStatus)
			}
			if res.ChildSummary == nil {
				t.Fatal("child summary not read")
			}
			if tc.wantOutcome == OutcomeDownloaderFatalError {
				if !strings.Contains(res.ChildError, "retry limit exceeded") {
					t.Fatalf("ChildError = %q, want retry-limit text", res.ChildError)
				}
				if err == nil || !strings.Contains(err.Error(), "retry limit exceeded") {
					t.Fatalf("err = %v, want retry-limit text", err)
				}
			}
		})
	}
}

// TestReplayMissingSummaryIsNotEOF covers a trace-EOF-eligible mm-link exit
// with no child summary at all: the outcome must be DOWNLOADER_SUMMARY_MISSING,
// never SUCCESS_TRACE_REACHED_EOF.
func TestReplayMissingSummaryIsNotEOF(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "t.up")
	down := filepath.Join(dir, "t.down")
	writeTrace(t, up, []int64{0, 100, 200})
	writeTrace(t, down, []int64{0, 100, 200})
	fake := writeFakeMMLink(t, dir, "exit 0")
	downLog := filepath.Join(dir, "down.log")
	writeDownlinkLog(t, downLog, 200)

	res, err := Run(context.Background(), Config{
		UpTrace: up, DownTrace: down,
		WorkloadSize: 1 << 20, ChunkSize: 1 << 18, Concurrency: 1,
		MaxRuntime:   10 * time.Second,
		MMLinkBinary: fake,
		SummaryPath:  filepath.Join(dir, "absent-summary.json"),
		ServerAddr:   "127.0.0.1:0",
		Child: func(int) ([]string, []string) {
			return []string{"/bin/true"}, nil
		},
		DownlinkLogPath: downLog,
	})
	if err == nil {
		t.Fatal("err = nil, want summary-missing failure")
	}
	if res.Outcome != OutcomeSummaryMissing {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeSummaryMissing)
	}
	if res.ChildSummary != nil {
		t.Fatal("child summary unexpectedly read")
	}
	if res.ChildStatus != ChildStatusMissing {
		t.Fatalf("ChildStatus = %s, want %s", res.ChildStatus, ChildStatusMissing)
	}
}

// TestChildExitStatuses covers the child process contract: a clean completion
// returns no error, a fatal downloader failure returns non-nil, and a
// harness-initiated cancellation is not fatal.
func TestChildExitStatuses(t *testing.T) {
	t.Run("normal success", func(t *testing.T) {
		srv, err := server.NewLogical(1 << 18)
		if err != nil {
			t.Fatal(err)
		}
		ts := httptest.NewServer(srv)
		defer ts.Close()
		dir := t.TempDir()

		sum, err := RunDownloaderChild(context.Background(), ChildConfig{
			Host: "127.0.0.1", Port: serverPort(t, ts),
			OutputPath:  filepath.Join(dir, "out.bin"),
			SummaryPath: filepath.Join(dir, "summary.json"),
			TotalSize:   1 << 18, ChunkSize: 1 << 18, Concurrency: 1,
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("RunDownloaderChild = %v, want success", err)
		}
		if !sum.Final || !sum.WorkloadExhausted || sum.Fatal || sum.Canceled || sum.Err != "" {
			t.Fatalf("summary = %+v, want clean final completion", sum)
		}
	})

	t.Run("fatal retry limit", func(t *testing.T) {
		ts := alwaysSlowServer(t, 300*time.Millisecond)
		defer ts.Close()
		dir := t.TempDir()

		sum, err := RunDownloaderChild(context.Background(), ChildConfig{
			Host: "127.0.0.1", Port: serverPort(t, ts),
			OutputPath:  filepath.Join(dir, "out.bin"),
			SummaryPath: filepath.Join(dir, "summary.json"),
			TotalSize:   1 << 18, ChunkSize: 1 << 18, Concurrency: 1,
			Timeout: 40 * time.Millisecond,
		})
		if err == nil {
			t.Fatal("RunDownloaderChild = nil, want fatal error")
		}
		if !errors.Is(err, downloader.ErrRetryLimitExceeded) {
			t.Fatalf("err = %v, want ErrRetryLimitExceeded", err)
		}
		if !sum.Final || !sum.Fatal || sum.Canceled {
			t.Fatalf("summary = %+v, want fatal non-canceled", sum)
		}
		if !strings.Contains(sum.Err, "retry limit exceeded") {
			t.Fatalf("summary err = %q, want retry-limit text", sum.Err)
		}
		// The on-disk summary must preserve the error too.
		onDisk, rerr := readChildSummary(filepath.Join(dir, "summary.json"))
		if rerr != nil {
			t.Fatalf("read summary: %v", rerr)
		}
		if !onDisk.Fatal || !strings.Contains(onDisk.Err, "retry limit exceeded") {
			t.Fatalf("persisted summary = %+v, want fatal with error", onDisk)
		}
	})

	t.Run("cancellation is not fatal", func(t *testing.T) {
		var once sync.Once
		started := make(chan struct{})
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { close(started) })
			<-r.Context().Done()
		}))
		defer ts.Close()
		dir := t.TempDir()

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		var sum ChildSummary
		var err error
		go func() {
			defer close(done)
			sum, err = RunDownloaderChild(ctx, ChildConfig{
				Host: "127.0.0.1", Port: serverPort(t, ts),
				OutputPath:  filepath.Join(dir, "out.bin"),
				SummaryPath: filepath.Join(dir, "summary.json"),
				TotalSize:   1 << 18, ChunkSize: 1 << 18, Concurrency: 1,
				Timeout: 10 * time.Second,
			})
		}()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("child never issued a request")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("child did not stop after cancellation")
		}
		if err != nil {
			t.Fatalf("canceled child returned error: %v", err)
		}
		if !sum.Final || !sum.Canceled || sum.Fatal {
			t.Fatalf("summary = %+v, want canceled non-fatal", sum)
		}
	})
}

// TestSyntheticEarlyReplayCaseE covers a per-request timeout followed by a
// successful retry: the child must finish cleanly and exit zero.
func TestSyntheticEarlyReplayCaseE(t *testing.T) {
	ts := slowThenFastServer(t, 300*time.Millisecond)
	defer ts.Close()
	dir := t.TempDir()

	sum, err := RunDownloaderChild(context.Background(), ChildConfig{
		Host: "127.0.0.1", Port: serverPort(t, ts),
		OutputPath:  filepath.Join(dir, "out.bin"),
		SummaryPath: filepath.Join(dir, "summary.json"),
		TotalSize:   1 << 18, ChunkSize: 1 << 18, Concurrency: 1,
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RunDownloaderChild = %v, want success after retry", err)
	}
	if !sum.Final || !sum.WorkloadExhausted || sum.Fatal || sum.Err != "" {
		t.Fatalf("summary = %+v, want clean completion", sum)
	}
	if sum.Requests != 2 || sum.Successful != 1 || sum.Failed != 1 {
		t.Fatalf("summary = %+v, want 2 requests (1 timeout + 1 success)", sum)
	}
}

// TestSyntheticEarlyReplayCaseF is the end-to-end case: a child that exhausts
// its retries after per-request timeouts exits non-zero, and the replay must
// be classified as a fatal downloader error, never as trace EOF.
func TestSyntheticEarlyReplayCaseF(t *testing.T) {
	ts := alwaysSlowServer(t, 400*time.Millisecond)
	defer ts.Close()

	dir := t.TempDir()
	up := filepath.Join(dir, "t.up")
	down := filepath.Join(dir, "t.down")
	writeTrace(t, up, []int64{0, 100, 200})
	writeTrace(t, down, []int64{0, 100, 200})
	summary := filepath.Join(dir, "summary.json")
	events := filepath.Join(dir, "events.jsonl")
	output := filepath.Join(dir, "out.bin")
	downLog := filepath.Join(dir, "down.log")
	writeDownlinkLog(t, downLog, 200)
	fake := writeFakeMMLink(t, dir,
		"while [ \"$#\" -gt 0 ] && [ \"$1\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"")

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := func(int) ([]string, []string) {
		return []string{exe, "-test.run=^TestMMChildHelper$"}, []string{
			"REPLAY_CHILD=1",
			"MAHIMAHI_BASE=127.0.0.1",
			"REPLAY_PORT=" + strconv.Itoa(serverPort(t, ts)),
			"REPLAY_OUTPUT=" + output,
			"REPLAY_SUMMARY=" + summary,
			"REPLAY_EVENTS=" + events,
			"REPLAY_TOTAL=" + strconv.Itoa(1<<18),
			"REPLAY_CHUNK=" + strconv.Itoa(1<<18),
			"REPLAY_C=1",
			"REPLAY_TIMEOUT=50ms",
			"REPLAY_DIAL_TIMEOUT=none",
			"REPLAY_MAX=10s",
		}
	}

	res, err := Run(context.Background(), Config{
		UpTrace: up, DownTrace: down,
		WorkloadSize: 1 << 20, ChunkSize: 1 << 18, Concurrency: 1,
		MaxRuntime: 10 * time.Second, TerminationGrace: 2 * time.Second,
		MMLinkBinary: fake, Child: child,
		SummaryPath: summary, DownlinkLogPath: downLog,
		ServerAddr: "127.0.0.1:0",
	})
	if err == nil {
		t.Fatal("err = nil, want non-EOF failure")
	}
	if res.Outcome == OutcomeSuccessTraceEOF {
		t.Fatal("retry exhaustion was misclassified as trace EOF")
	}
	if res.Outcome != OutcomeDownloaderFatalError {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeDownloaderFatalError)
	}
	if res.MMLinkExit == 0 {
		t.Fatal("fatal child must not leave mm-link exit status 0")
	}
	if res.ChildStatus != ChildStatusFatal {
		t.Fatalf("ChildStatus = %s, want %s", res.ChildStatus, ChildStatusFatal)
	}
	if !strings.Contains(res.ChildError, "retry limit exceeded") {
		t.Fatalf("ChildError = %q, want retry-limit text", res.ChildError)
	}
	if !strings.Contains(err.Error(), "retry limit exceeded") {
		t.Fatalf("err = %v, want retry-limit text", err)
	}
}

// slowThenFastServer answers the first request after delay and all later
// requests immediately, for the timeout-then-retry test.
func slowThenFastServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	var calls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		writeSingleRangeBody(t, w)
	}))
}

// alwaysSlowServer never answers before the delay, for retry-exhaustion tests.
func alwaysSlowServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		writeSingleRangeBody(t, w)
	}))
}

// writeSingleRangeBody answers a request for the single range covering a
// 256 KiB object (the tests configure TotalSize == ChunkSize).
func writeSingleRangeBody(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	const size = 1 << 18
	body := bytes.Repeat([]byte{0x5a}, size)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", size-1, size))
	w.Header().Set("Content-Length", strconv.Itoa(size))
	w.WriteHeader(http.StatusPartialContent)
	if _, err := w.Write(body); err != nil {
		t.Logf("write range body: %v", err)
	}
}

func serverPort(t *testing.T, ts *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}
	return port
}

// TestParseDeadline covers the explicit deadline policy syntax: "none" means
// no deadline; positive durations are diagnostic overrides; everything else is
// rejected.
func TestParseDeadline(t *testing.T) {
	cases := []struct {
		in       string
		want     time.Duration
		disabled bool
		wantErr  bool
	}{
		{"none", 0, true, false},
		{"NONE", 0, true, false},
		{" none ", 0, true, false},
		{"120s", 120 * time.Second, false, false},
		{"600s", 600 * time.Second, false, false},
		{"", 0, false, true},
		{"0s", 0, false, true},
		{"-5s", 0, false, true},
		{"nope", 0, false, true},
	}
	for _, tc := range cases {
		got, disabled, err := ParseDeadline(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseDeadline(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if got != tc.want || disabled != tc.disabled {
			t.Errorf("ParseDeadline(%q) = (%v, %v), want (%v, %v)", tc.in, got, disabled, tc.want, tc.disabled)
		}
	}
	if DeadlineString(0, true) != "none" {
		t.Errorf("DeadlineString(none) = %q", DeadlineString(0, true))
	}
	if DeadlineString(120*time.Second, false) != "2m0s" {
		t.Errorf("DeadlineString(120s) = %q", DeadlineString(120*time.Second, false))
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file %s did not appear within %v", path, timeout)
}

// TestWatchdogOnlyWatchdogCancelsBlockedRequest is synthetic test 2: with no
// request deadline configured, the parent watchdog is the only bound. A child
// blocked on an HTTP request must be canceled through the run context, the
// run must end WATCHDOG_TIMEOUT (non-EOF), and cleanup must leave nothing
// behind.
func TestWatchdogOnlyWatchdogCancelsBlockedRequest(t *testing.T) {
	ts := alwaysSlowServer(t, time.Hour)
	defer ts.Close()

	dir := t.TempDir()
	up := filepath.Join(dir, "t.up")
	down := filepath.Join(dir, "t.down")
	writeTrace(t, up, []int64{0, 100, 200})
	writeTrace(t, down, []int64{0, 100, 200})
	summary := filepath.Join(dir, "summary.json")
	events := filepath.Join(dir, "events.jsonl")
	output := filepath.Join(dir, "out.bin")
	downLog := filepath.Join(dir, "down.log")
	writeDownlinkLog(t, downLog, 40) // short endpoint: never EOF
	fake := writeFakeMMLink(t, dir,
		"while [ \"$#\" -gt 0 ] && [ \"$1\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"")

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := func(int) ([]string, []string) {
		return []string{exe, "-test.run=^TestMMChildHelper$"}, []string{
			"REPLAY_CHILD=1",
			"MAHIMAHI_BASE=127.0.0.1",
			"REPLAY_PORT=" + strconv.Itoa(serverPort(t, ts)),
			"REPLAY_OUTPUT=" + output,
			"REPLAY_SUMMARY=" + summary,
			"REPLAY_EVENTS=" + events,
			"REPLAY_TOTAL=" + strconv.Itoa(1<<18),
			"REPLAY_CHUNK=" + strconv.Itoa(1<<18),
			"REPLAY_C=1",
			"REPLAY_TIMEOUT=none",
			"REPLAY_DIAL_TIMEOUT=none",
			"REPLAY_MAX=1h", // the child must not self-terminate; the parent bounds it
		}
	}

	res, err := Run(context.Background(), Config{
		UpTrace: up, DownTrace: down,
		WorkloadSize: 1 << 20, ChunkSize: 1 << 18, Concurrency: 1,
		MaxRuntime: 1200 * time.Millisecond, TerminationGrace: time.Second,
		MMLinkBinary: fake, Child: child, SummaryPath: summary, DownlinkLogPath: downLog,
		ServerAddr: "127.0.0.1:0",
	})
	if err == nil {
		t.Fatal("err = nil, want watchdog failure")
	}
	if res.Outcome != OutcomeWatchdogTimeout {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeWatchdogTimeout)
	}
	if res.EndpointValid {
		t.Fatal("endpoint must not be valid for a watchdog-aborted run")
	}
	if len(res.MMLink.LeftoverPIDs) != 0 {
		t.Fatalf("leftover pids after watchdog: %v", res.MMLink.LeftoverPIDs)
	}
	assertNoMMLinkProcess(t)
}

// TestCanceledChildBeforeTraceEOFFails is synthetic test 4: a child canceled
// before the trace endpoint must never be classified as EOF.
func TestCanceledChildBeforeTraceEOFFails(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "t.up")
	down := filepath.Join(dir, "t.down")
	writeTrace(t, up, []int64{0, 100, 200})
	writeTrace(t, down, []int64{0, 100, 200})
	summary := filepath.Join(dir, "summary.json")
	writeChildSummaryFile(t, summary, ChildSummary{
		Final: true, Canceled: true, Err: "context canceled", Requests: 2, Successful: 1,
	})
	downLog := filepath.Join(dir, "down.log")
	writeDownlinkLog(t, downLog, 40) // 40 ms observed vs 200 ms expected
	fake := writeFakeMMLink(t, dir, "exit 0")

	res, err := Run(context.Background(), Config{
		UpTrace: up, DownTrace: down,
		WorkloadSize: 1 << 20, ChunkSize: 1 << 18, Concurrency: 1,
		MaxRuntime:   10 * time.Second,
		MMLinkBinary: fake, SummaryPath: summary, DownlinkLogPath: downLog,
		ServerAddr: "127.0.0.1:0",
		Child: func(int) ([]string, []string) {
			return []string{"/bin/true"}, nil
		},
	})
	if err == nil {
		t.Fatal("err = nil, want non-EOF failure")
	}
	if res.Outcome != OutcomeEndpointNotReached {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeEndpointNotReached)
	}
	if res.ChildStatus != ChildStatusCanceled {
		t.Fatalf("ChildStatus = %s, want %s", res.ChildStatus, ChildStatusCanceled)
	}
}

// TestExternalCancellationBeforeTraceEOFIsNotSuccess is synthetic test C of the
// EOF distinction: an operator/caller cancellation before trace EOF yields
// CANCELED, never EOF.
func TestExternalCancellationBeforeTraceEOFIsNotSuccess(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "t.up")
	down := filepath.Join(dir, "t.down")
	writeTrace(t, up, []int64{0, 100, 200})
	writeTrace(t, down, []int64{0, 100, 200})
	summary := filepath.Join(dir, "summary.json")
	marker := filepath.Join(dir, "mm-started")
	// exec keeps a single process (no shell grandchild), so teardown has no
	// reap race with an orphaned child.
	fake := writeFakeMMLink(t, dir, fmt.Sprintf("touch %q\nexec sleep 30", marker))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var res Result
	var err error
	go func() {
		defer close(done)
		res, err = Run(ctx, Config{
			UpTrace: up, DownTrace: down,
			WorkloadSize: 1 << 20, ChunkSize: 1 << 18, Concurrency: 1,
			MaxRuntime: 30 * time.Second, TerminationGrace: time.Second,
			MMLinkBinary: fake, SummaryPath: summary,
			ServerAddr: "127.0.0.1:0",
			Child: func(int) ([]string, []string) {
				return []string{"/bin/true"}, nil
			},
		})
	}()
	waitForFile(t, marker, 5*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("run did not stop after external cancellation")
	}
	if err == nil {
		t.Fatal("err = nil, want cancellation failure")
	}
	if res.Outcome != OutcomeCanceled {
		t.Fatalf("outcome = %s, want %s", res.Outcome, OutcomeCanceled)
	}
	if res.Outcome == OutcomeSuccessTraceEOF {
		t.Fatal("external cancellation must not be EOF")
	}
	assertNoMMLinkProcess(t)
}

// TestChildSwitchInjection verifies the harness-only concurrency switch used by
// the tau campaign: adaptive bounds, a scheduled target change, and summary
// evidence.
func TestChildSwitchInjection(t *testing.T) {
	srv, err := server.NewLogical(1 << 40)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	dir := t.TempDir()

	sum, _ := RunDownloaderChild(context.Background(), ChildConfig{
		Host: "127.0.0.1", Port: serverPort(t, ts),
		OutputPath:  filepath.Join(dir, "out.bin"),
		SummaryPath: filepath.Join(dir, "summary.json"),
		EventsPath:  filepath.Join(dir, "events.jsonl"),
		TotalSize:   1 << 40, ChunkSize: 1 << 20, Concurrency: 1,
		MinConcurrency: 1, MaxConcurrency: 4,
		SwitchAt: 200 * time.Millisecond, SwitchTo: 4,
		NoRequestTimeout: true, NoDialTimeout: true,
		MaxRuntime: 1200 * time.Millisecond,
	})
	if !sum.SwitchApplied || sum.SwitchTo != 4 || sum.SwitchAtNanos != int64(200*time.Millisecond) {
		t.Fatalf("switch evidence = %+v", sum)
	}
	if sum.FinalConcurrency != 4 {
		t.Fatalf("final concurrency = %d, want 4", sum.FinalConcurrency)
	}
}

// TestChildSwitchValidation rejects switch configurations that cannot be
// honored.
func TestChildSwitchValidation(t *testing.T) {
	base := ChildConfig{Port: 1, OutputPath: "o", SummaryPath: "s", TotalSize: 1, ChunkSize: 1, Concurrency: 1}
	cases := map[string]ChildConfig{
		"no adaptive bounds":   {SwitchAt: time.Second, SwitchTo: 2},
		"target out of bounds": {MinConcurrency: 1, MaxConcurrency: 4, SwitchAt: time.Second, SwitchTo: 8},
		"single bound":         {MinConcurrency: 0, MaxConcurrency: 4},
	}
	for name, tc := range cases {
		cfg := base
		cfg.MinConcurrency = tc.MinConcurrency
		cfg.MaxConcurrency = tc.MaxConcurrency
		cfg.SwitchAt = tc.SwitchAt
		cfg.SwitchTo = tc.SwitchTo
		if _, err := RunDownloaderChild(context.Background(), cfg); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}
