package pilot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"adaptivedl/internal/experiment/epoch"
)

func TestPlanIsTenRunMatrix(t *testing.T) {
	good := TracePair{ID: "good", UpPath: "/g.up", DownPath: "/g.down"}
	deg := TracePair{ID: "deg", UpPath: "/d.up", DownPath: "/d.down"}
	specs := Plan(good, deg, DefaultCSet)
	if len(specs) != 10 {
		t.Fatalf("plan = %d runs, want 10", len(specs))
	}
	for _, id := range []string{"good", "deg"} {
		n := 0
		for _, s := range specs {
			if s.TraceID == id {
				n++
			}
		}
		if n != 5 {
			t.Fatalf("trace %s runs = %d, want 5", id, n)
		}
	}
	for _, s := range specs {
		wantUp := "/g.up"
		wantDown := "/g.down"
		if s.TraceID == "deg" {
			wantUp, wantDown = "/d.up", "/d.down"
		}
		if s.UpPath != wantUp || s.DownPath != wantDown {
			t.Fatalf("run %s C=%d uses up=%s down=%s", s.TraceID, s.C, s.UpPath, s.DownPath)
		}
	}
	ordered := SortedSpecs(specs)
	if ordered[0].TraceID != "deg" && ordered[0].TraceID != "good" {
		t.Fatal("order must be deterministic")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTracePair(t *testing.T) {
	dir := t.TempDir()
	up := filepath.Join(dir, "tram.up")
	down := filepath.Join(dir, "tram.down")
	writeFile(t, up, "0\n10\n")
	writeFile(t, down, "0\n20\n")

	for _, input := range []string{
		filepath.Join(dir, "tram"),
		filepath.Join(dir, "tram.up"),
		filepath.Join(dir, "tram.down"),
	} {
		pair, err := ResolveTracePair(input)
		if err != nil {
			t.Fatalf("ResolveTracePair(%q): %v", input, err)
		}
		if pair.ID != "tram" || pair.UpPath != up || pair.DownPath != down {
			t.Fatalf("ResolveTracePair(%q) = %+v", input, pair)
		}
	}

	if _, err := ResolveTracePair(""); err == nil {
		t.Fatal("empty family accepted")
	}
	only := filepath.Join(dir, "only")
	writeFile(t, only+".down", "0\n20\n")
	if _, err := ResolveTracePair(only); err == nil {
		t.Fatal("missing .up accepted")
	}
	if _, err := ResolveTracePair(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing pair accepted")
	}
}

func TestTracePairRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	writeFile(t, a+".up", "0\n10\n")
	writeFile(t, a+".down", "0\n10\n")
	writeFile(t, b+".down", "0\n10\n")

	if _, err := ResolveTracePair(a); err != nil {
		t.Fatalf("same-family pair rejected: %v", err)
	}
	pair, err := TracePairFromPaths(a+".up", b+".down")
	if err == nil {
		t.Fatalf("mismatched families accepted: %+v", pair)
	}
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "a.down"), "0\n10\n")
	if _, err := TracePairFromPaths(a+".up", filepath.Join(other, "a.down")); err == nil {
		t.Fatal("cross-directory pair accepted")
	}
	if _, err := TracePairFromPaths(a+".up", ""); err == nil {
		t.Fatal("missing member accepted")
	}
}

func TestRunDirAndResume(t *testing.T) {
	base := t.TempDir()
	dir := RunDir(base, "verizon", 4)
	if dir != filepath.Join(base, "verizon_C4") {
		t.Fatalf("rundir = %s", dir)
	}
	if r, err := LoadResult(dir); err != nil || r != nil {
		t.Fatalf("missing result = (%v,%v), want (nil,nil)", r, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ok := &RunResult{TraceID: "verizon", C: 4, Status: "ok"}
	if err := WriteJSON(ResultPath(dir), ok); err != nil {
		t.Fatal(err)
	}
	got, err := LoadResult(dir)
	if err != nil || got == nil || got.Status != "ok" {
		t.Fatalf("resume load = (%v,%v)", got, err)
	}
	if err := WriteJSON(FailurePath(dir, 1), map[string]string{"status": "failed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(FailurePath(dir, 1)); err != nil {
		t.Fatalf("failure record not preserved: %v", err)
	}
}

func writeEvents(t *testing.T, path string, lines []string) {
	t.Helper()
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStreamEventsMapsTraceTimeAndAccumulatesAllDurations(t *testing.T) {
	const launch = int64(1_000_000_000)
	anchor := &AnchorMeta{LaunchParentWallUnixNano: launch, StartupOffsetNanos: int64(60 * time.Millisecond), TraceDurationNanos: int64(100 * time.Second)}
	// Events at trace times 10s, 40s (crosses 30s boundary), 70s, 99s.
	ev := func(sec int64, bytes int64) string {
		wall := launch + int64(60*time.Millisecond) + sec*int64(time.Second)
		return `{"wall_unix_nano":` + itoa(wall) + `,"delta":` + itoa(bytes) + `}`
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvents(t, path, []string{ev(10, 1<<20), ev(40, 1<<20), ev(70, 1<<20), ev(99, 1<<20)})

	acc, total, out, err := StreamEvents(path, anchor, 100*time.Second, DefaultCandidates)
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	if total != 4<<20 || out != 0 {
		t.Fatalf("total=%d outOfRange=%d", total, out)
	}
	if acc[int64(30*time.Second)][0] != 1<<20 || acc[int64(30*time.Second)][1] != 1<<20 {
		t.Fatalf("30s buckets = %v", acc[int64(30*time.Second)])
	}
	if len(acc[int64(120*time.Second)]) != 1 {
		t.Fatalf("120s partial epoch count = %d", len(acc[int64(120*time.Second)]))
	}
}

func TestBuildRunResultSharedValidityAndSelection(t *testing.T) {
	const launch = int64(1_000_000_000)
	anchor := &AnchorMeta{LaunchParentWallUnixNano: launch, StartupOffsetNanos: 0, TraceDurationNanos: int64(300 * time.Second)}
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	var lines []string
	for sec := int64(0); sec < 300; sec++ {
		wall := launch + sec*int64(time.Second)
		lines = append(lines, `{"wall_unix_nano":`+itoa(wall)+`,"delta":`+itoa(10<<20)+`}`) // 10 MiB/s
	}
	writeEvents(t, path, lines)

	perC := map[int]*RunResult{}
	for _, c := range DefaultCSet {
		rr, err := BuildRunResult(RunSpec{TraceID: "t", UpPath: "u", DownPath: "p", C: c}, 300*time.Second, anchor, path, DefaultCandidates, epoch.ExclusionNone)
		if err != nil {
			t.Fatal(err)
		}
		perC[c] = rr
	}
	ts, err := CombineTrace("t", perC, DefaultCandidates)
	if err != nil {
		t.Fatal(err)
	}
	cand := ts.Candidates["30s"]
	if cand.CandidateEpochs != 10 || cand.ValidEpochs != 10 {
		t.Fatalf("30s candidate = %+v", cand)
	}
	rep := SummarizePilot([]TraceSummary{ts}, DefaultCandidates, nil)
	if !rep.Passed || rep.Selected != "30s" {
		t.Fatalf("selection = %+v", rep)
	}
}

func TestEmptyEventsBelowFloorBlocked(t *testing.T) {
	anchor := &AnchorMeta{LaunchParentWallUnixNano: 1, StartupOffsetNanos: 0, TraceDurationNanos: int64(120 * time.Second)}
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	writeEvents(t, path, []string{`{"wall_unix_nano":1,"delta":1}`})
	perC := map[int]*RunResult{}
	for _, c := range DefaultCSet {
		rr, err := BuildRunResult(RunSpec{TraceID: "t", UpPath: "u", DownPath: "p", C: c}, 120*time.Second, anchor, path, DefaultCandidates, epoch.ExclusionNone)
		if err != nil {
			t.Fatal(err)
		}
		perC[c] = rr
	}
	ts, err := CombineTrace("t", perC, DefaultCandidates)
	if err != nil {
		t.Fatal(err)
	}
	rep := SummarizePilot([]TraceSummary{ts}, DefaultCandidates, nil)
	if rep.Passed {
		t.Fatalf("pilot must be blocked with insufficient bytes: %+v", rep)
	}
}

func TestMissingAnchorIsNotFabricated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	writeEvents(t, path, []string{`{"wall_unix_nano":1,"delta":10}`})
	rr, err := BuildRunResult(RunSpec{TraceID: "t", UpPath: "u", DownPath: "p", C: 1}, 50*time.Second, nil, path, DefaultCandidates, epoch.ExclusionNone)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rr.Durations["30s"].Epochs {
		if e.Valid || e.Exclusion != epoch.ExclusionAnchorAlignment {
			t.Fatalf("epoch not anchor-invalid: %+v", e)
		}
	}
	rep := SummarizePilot(nil, DefaultCandidates, []string{"t_C1"})
	if rep.Passed || rep.Selected != "" {
		t.Fatalf("incomplete pilot must not pass: %+v", rep)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
