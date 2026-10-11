package replay

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A. Anchor arithmetic: an event maps to the expected trace time.
func TestTraceTimeArithmetic(t *testing.T) {
	base := time.Now()
	event := base.Add(1234 * time.Millisecond)
	if got := TraceTime(event, base); got != 1234*time.Millisecond {
		t.Fatalf("TraceTime = %v, want 1234ms", got)
	}
	// Monotonic (E): Add/Sub on time.Now-derived values never touches wall.
	later := time.Now().Add(50 * time.Millisecond)
	if got := TraceTime(later, time.Now()); got < 45*time.Millisecond || got > 60*time.Millisecond {
		t.Fatalf("monotonic duration out of range: %v", got)
	}
}

// B. Startup offset: the child starting after mm-link launch does not
// redefine t=0.
func TestStartupOffsetIndependentOfChildStart(t *testing.T) {
	launch := time.Now()
	anchor := PredictedTraceStart(launch)
	if want := launch.Add(StartupOffset); !anchor.Equal(want) {
		t.Fatalf("PredictedTraceStart = %v, want %v", anchor, want)
	}
	// The trace clock starts with mm-link; a later child start shifts the
	// event's child-relative time by the same amount, so the derived
	// launch->t=0 offset is unchanged. Trace t=0 is 100 ms after launch in
	// both cases; the known edge is at trace 1000 ms.
	const launchNano = int64(1_000_000_000)
	offEarly := StartupOffsetSample(launchNano, launchNano+50_000_000, 1050*time.Millisecond, 1000*time.Millisecond)
	offLate := StartupOffsetSample(launchNano, launchNano+200_000_000, 900*time.Millisecond, 1000*time.Millisecond)
	if offEarly != offLate {
		t.Fatalf("launch->t=0 offset depends on child start: %v vs %v", offEarly, offLate)
	}
	if offEarly != 100*time.Millisecond {
		t.Fatalf("offset = %v, want 100ms", offEarly)
	}
}

// C. First byte is a measurement, not the origin.
func TestFirstByteIsMeasurementNotOrigin(t *testing.T) {
	launchWall := time.Now()
	launch := launchWall
	summary := &ChildSummary{
		StartWallUnixNano:        launchWall.Add(40 * time.Millisecond).UnixNano(),
		HasFirstByte:             true,
		FirstByteSinceStartNanos: int64(900 * time.Millisecond),
		FirstByteWallUnixNano:    launchWall.Add(940 * time.Millisecond).UnixNano(),
	}
	a := buildAnchor(MMLinkResult{LaunchedAt: launch, EndedAt: launch.Add(3 * time.Second)}, 2*time.Second, summary, "")
	if a == nil {
		t.Fatal("nil anchor")
	}
	if !a.TraceStartParentMono.Equal(launch.Add(StartupOffset)) {
		t.Fatalf("trace start moved with first byte: %v", a.TraceStartParentMono)
	}
	if a.FirstByteTraceTime != 940*time.Millisecond-StartupOffset {
		t.Fatalf("FirstByteTraceTime = %v, want %v", a.FirstByteTraceTime, 940*time.Millisecond-StartupOffset)
	}
}

// D. EOF is represented separately from child completion.
func TestTraceEOFSeparateFromChildCompletion(t *testing.T) {
	launch := time.Now()
	a := buildAnchor(MMLinkResult{LaunchedAt: launch, EndedAt: launch.Add(2500 * time.Millisecond)}, 2200*time.Millisecond, nil, "")
	if a == nil {
		t.Fatal("nil anchor")
	}
	if a.TraceEOF != 2200*time.Millisecond {
		t.Fatalf("TraceEOF = %v, want trace duration", a.TraceEOF)
	}
	if a.ExitParentMono.IsZero() || !a.ExitParentMono.After(a.LaunchParentMono) {
		t.Fatalf("exit timestamp not recorded: %v", a.ExitParentMono)
	}
	if a.TraceEOF == a.ExitParentMono.Sub(a.LaunchParentMono) {
		t.Fatal("EOF trace time must not be derived from wall elapsed time")
	}
}

// F. Calibration statistics are computed correctly.
func TestSummarizeStats(t *testing.T) {
	in := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, 40 * time.Millisecond}
	st := Summarize(in)
	if st.N != 4 || st.Mean != 25*time.Millisecond || st.Min != 10*time.Millisecond || st.Max != 40*time.Millisecond {
		t.Fatalf("stats = %+v", st)
	}
	if st.Median != 30*time.Millisecond {
		t.Fatalf("median = %v, want 30ms", st.Median)
	}
	wantSD := time.Duration(11_180_339) // sqrt(125)*1e6 ns
	if d := st.StdDev - wantSD; d > time.Millisecond || d < -time.Millisecond {
		t.Fatalf("stddev = %v, want ~%v", st.StdDev, wantSD)
	}
	if got := Summarize(nil); got.N != 0 {
		t.Fatalf("empty summarize = %+v", got)
	}
}

// G. Tolerance boundary is inclusive and rejects larger errors.
func TestWithinToleranceBoundary(t *testing.T) {
	tol := AlignmentTolerance
	if !WithinTolerance(tol) || !WithinTolerance(-tol) {
		t.Fatalf("boundary residual rejected (tol=%v)", tol)
	}
	if WithinTolerance(tol+time.Nanosecond) || WithinTolerance(-tol-time.Nanosecond) {
		t.Fatalf("residual above tolerance accepted (tol=%v)", tol)
	}
}

// H. Raw timestamps are retained and derived fields are reproducible.
func TestAnchorRawAndDerivedReproducible(t *testing.T) {
	launch := time.Now()
	summary := &ChildSummary{StartWallUnixNano: launch.Add(30 * time.Millisecond).UnixNano()}
	mml := MMLinkResult{LaunchedAt: launch, EndedAt: launch.Add(time.Second)}
	a1 := buildAnchor(mml, 900*time.Millisecond, summary, "")
	a2 := buildAnchor(mml, 900*time.Millisecond, summary, "")
	if a1 == nil || a2 == nil {
		t.Fatal("nil anchor")
	}
	if !a1.LaunchParentMono.Equal(launch) || a1.ChildStartWallUnixNano != summary.StartWallUnixNano {
		t.Fatal("raw timestamps were not retained")
	}
	if !a1.TraceStartParentMono.Equal(a2.TraceStartParentMono) || a1.FirstByteTraceTime != a2.FirstByteTraceTime {
		t.Fatal("derived anchor is not reproducible from raw fields")
	}
}

func TestParseTraceLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "down.log")
	content := "# mm-link\n# queue\n0 # 1504\n80 # 1504\n80 # 1504\n1200 # 1504\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, lastMs, err := ParseTraceLog(path)
	if err != nil {
		t.Fatalf("ParseTraceLog: %v", err)
	}
	if lines != 4 || lastMs != 1200 {
		t.Fatalf("lines=%d lastMs=%d, want 4/1200", lines, lastMs)
	}
	if l, m, err := ParseTraceLog(filepath.Join(dir, "missing.log")); err != nil || l != 0 || m != 0 {
		t.Fatalf("missing log = (%d,%d,%v), want zeroes", l, m, err)
	}
}

// calibrationTrace is a short synthetic trace with two unmistakable delivery
// gaps: active [0,199] ms, gap to 1000 ms, active [1000,1199], gap to 2000,
// active [2000,2199]. The first post-gap event traces back to a known edge.
func calibrationTrace() []int64 {
	var s []int64
	appendActive := func(from, to int64) {
		for i := from; i <= to; i++ {
			s = append(s, i)
		}
	}
	appendActive(0, 199)
	appendActive(1000, 1199)
	appendActive(2000, 2199)
	return s
}

func childBuilderEvents(t *testing.T, concurrency int, events, output, summary string) func(int) ([]string, []string) {
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
			"REPLAY_EVENTS=" + events,
			"REPLAY_TOTAL=" + strconv.Itoa(64<<20),
			"REPLAY_CHUNK=" + strconv.Itoa(1<<20),
			"REPLAY_C=" + strconv.Itoa(concurrency),
			"REPLAY_TIMEOUT=none",
			"REPLAY_DIAL_TIMEOUT=none",
			"REPLAY_MAX=2m",
		}
		return argv, env
	}
}

func readEvents(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open events: %v", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var out []Event
	for {
		var e Event
		if err := dec.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decode event: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// TestAnchorCalibration repeatedly replays the same synthetic gap trace for
// C={1,4,16}, derives the launch->t=0 startup offset from the first post-gap
// edge, and validates it against the second edge. It is infrastructure
// calibration, not a performance measurement.
func TestAnchorCalibration(t *testing.T) {
	if testing.Short() {
		t.Skip("anchor calibration integration test skipped in -short mode")
	}
	trace := calibrationTrace()
	var offsets []time.Duration
	type sample struct {
		c      int
		rep    int
		off1   time.Duration
		off2   time.Duration
		firstT time.Duration
	}
	var samples []sample

	for _, c := range []int{1, 4, 16} {
		for rep := 0; rep < 3; rep++ {
			dir := t.TempDir()
			up := filepath.Join(dir, "up.link")
			writeTrace(t, up, trace)
			events := filepath.Join(dir, "events.jsonl")
			summary := filepath.Join(dir, "summary.json")
			output := filepath.Join(dir, "out.bin")
			downLog := filepath.Join(dir, "down.log")
			upLog := filepath.Join(dir, "up.log")

			res, err := Run(context.Background(), Config{
				UpTrace:          up,
				DownTrace:        up,
				WorkloadSize:     64 << 20,
				ChunkSize:        1 << 20,
				Concurrency:      c,
				Timeout:          10 * time.Second,
				MaxRuntime:       30 * time.Second,
				TerminationGrace: 2 * time.Second,
				Child:            childBuilderEvents(t, c, events, output, summary),
				SummaryPath:      summary,
				ServerAddr:       "0.0.0.0:0",
				UplinkLogPath:    upLog,
				DownlinkLogPath:  downLog,
			})
			if res.Outcome != OutcomeSuccessTraceEOF {
				t.Fatalf("C=%d rep=%d: outcome=%s err=%v", c, rep, res.Outcome, err)
			}
			if res.Anchor == nil {
				t.Fatalf("C=%d rep=%d: no anchor", c, rep)
			}
			evs := readEvents(t, events)
			resumes := ResumeTimes(evs, 300*time.Millisecond)
			if len(resumes) < 2 {
				t.Fatalf("C=%d rep=%d: found %d resume clusters in %d events", c, rep, len(resumes), len(evs))
			}
			if res.Anchor.DownlinkLogLines != int64(len(trace)) {
				t.Errorf("C=%d rep=%d: downlink log lines = %d, want %d", c, rep, res.Anchor.DownlinkLogLines, len(trace))
			}
			if res.Anchor.DownlinkLogLastMs < 2000 {
				t.Errorf("C=%d rep=%d: downlink log last ms = %d, want >= 2000", c, rep, res.Anchor.DownlinkLogLastMs)
			}
			// First byte is a measurement near trace t=0, not the origin. It
			// may land a few ms on either side of the predicted anchor due to
			// startup jitter.
			if ft := res.Anchor.FirstByteTraceTime; ft < -AlignmentTolerance || ft > 500*time.Millisecond {
				t.Errorf("C=%d rep=%d: first-byte trace time %v is outside the plausible measured range", c, rep, ft)
			}
			a := res.Anchor
			off1 := StartupOffsetSample(a.LaunchParentMono.UnixNano(), a.ChildStartWallUnixNano, time.Duration(resumes[0]), 1000*time.Millisecond)
			off2 := StartupOffsetSample(a.LaunchParentMono.UnixNano(), a.ChildStartWallUnixNano, time.Duration(resumes[1]), 2000*time.Millisecond)
			offsets = append(offsets, off1)
			samples = append(samples, sample{c, rep, off1, off2, a.FirstByteTraceTime})
			t.Logf("C=%2d rep=%d edge1000=%v edge2000=%v firstByteTrace=%v logLines=%d logLastMs=%d",
				c, rep, off1, off2, a.FirstByteTraceTime, a.DownlinkLogLines, a.DownlinkLogLastMs)
		}
	}

	st := Summarize(offsets)
	t.Logf("startup offset: n=%d mean=%v median=%v min=%v max=%v stddev=%v",
		st.N, st.Mean, st.Median, st.Min, st.Max, st.StdDev)
	for _, s := range samples {
		resid2 := s.off2 - StartupOffset
		resid1 := s.off1 - StartupOffset
		t.Logf("C=%2d rep=%d residual(edge1000)=%v residual(edge2000)=%v", s.c, s.rep, resid1, resid2)
		if !WithinTolerance(resid2) {
			t.Errorf("C=%d rep=%d: second-edge residual %v exceeds tolerance %v", s.c, s.rep, resid2, AlignmentTolerance)
		}
		if !WithinTolerance(resid1) {
			t.Errorf("C=%d rep=%d: first-edge residual %v exceeds tolerance %v", s.c, s.rep, resid1, AlignmentTolerance)
		}
	}
}
