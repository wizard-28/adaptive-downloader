package epoch

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSlicesExactBoundariesAndPartialTail(t *testing.T) {
	s := Slices(100*time.Second, 30*time.Second)
	if len(s) != 4 {
		t.Fatalf("slices = %d, want 4", len(s))
	}
	want := []Slice{
		{0, 30 * time.Second, 0, 30 * time.Second},
		{1, 30 * time.Second, 30 * time.Second, 60 * time.Second},
		{2, 30 * time.Second, 60 * time.Second, 90 * time.Second},
		{3, 30 * time.Second, 90 * time.Second, 100 * time.Second},
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("slices = %+v, want %+v", s, want)
	}
	if got := Slices(0, time.Second); got != nil {
		t.Fatalf("zero-duration slices = %+v, want nil", got)
	}
}

func TestAttributeUsesReadTimeAcrossBoundaries(t *testing.T) {
	s := Slices(100*time.Second, 30*time.Second)
	events := []TraceEvent{
		{Time: 5 * time.Second, Bytes: 100},
		{Time: 29 * time.Second, Bytes: 10},
		{Time: 30 * time.Second, Bytes: 7}, // half-open: belongs to epoch 1
		{Time: 99 * time.Second, Bytes: 3},
		{Time: 100 * time.Second, Bytes: 9}, // outside trace
	}
	got := Attribute(s, events)
	want := []int64{110, 7, 0, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attribute = %v, want %v", got, want)
	}
}

func TestByteFloorBoundary(t *testing.T) {
	s := []Slice{{0, time.Minute, 0, time.Minute}}
	if e := Evaluate("t", 1, s, []int64{ByteFloor}, ExclusionNone)[0]; !e.Valid {
		t.Fatalf("exactly floor must pass: %+v", e)
	}
	e := Evaluate("t", 1, s, []int64{ByteFloor - 1}, ExclusionNone)[0]
	if e.Valid || e.Exclusion != ExclusionByteFloor {
		t.Fatalf("below floor must fail: %+v", e)
	}
}

func TestObservedFractionBoundary(t *testing.T) {
	at := []Slice{{0, 100 * time.Second, 0, 80 * time.Second}}
	if e := Evaluate("t", 1, at, []int64{ByteFloor}, ExclusionNone)[0]; !e.Valid {
		t.Fatalf("exactly 0.80 must pass: %+v", e)
	}
	below := []Slice{{0, 100 * time.Second, 0, 79 * time.Second}}
	e := Evaluate("t", 1, below, []int64{ByteFloor}, ExclusionNone)[0]
	if e.Valid || e.Exclusion != ExclusionObservedFraction {
		t.Fatalf("below 0.80 must fail: %+v", e)
	}
}

func TestRusesObservedDuration(t *testing.T) {
	s := []Slice{{0, 120 * time.Second, 0, 30 * time.Second}}
	e := Evaluate("t", 1, s, []int64{ByteFloor}, ExclusionNone)[0]
	want := float64(ByteFloor) / 30.0
	if e.R != want {
		t.Fatalf("R = %v, want %v (observed divisor)", e.R, want)
	}
}

func TestSharedValidityPropagatesToAllC(t *testing.T) {
	s := []Slice{
		{0, time.Minute, 0, time.Minute},
		{1, time.Minute, time.Minute, 2 * time.Minute},
	}
	perC := map[int][]Epoch{
		1: Evaluate("t", 1, s, []int64{ByteFloor, ByteFloor}, ExclusionNone),
		4: Evaluate("t", 4, s, []int64{ByteFloor, ByteFloor - 1}, ExclusionNone),
	}
	shared := ApplyShared(perC)
	if len(shared) != 1 || !shared[0] {
		t.Fatalf("shared = %v, want {0:true}", shared)
	}
	if e := perC[1][1]; e.Valid || e.Exclusion != ExclusionOtherC {
		t.Fatalf("cross-C exclusion not applied: %+v", e)
	}
	if e := perC[4][1]; e.Exclusion != ExclusionByteFloor {
		t.Fatalf("original reason lost: %+v", e)
	}
}

func TestEpochBoundariesAreTraceTimeAnchored(t *testing.T) {
	for _, nominal := range DefaultProtocol().Candidates {
		s := Slices(125*time.Second, nominal)
		for i, e := range s {
			if e.Start != time.Duration(i)*nominal {
				t.Fatalf("nominal %v epoch %d start = %v", nominal, i, e.Start)
			}
		}
	}
}

func TestDeterminism(t *testing.T) {
	s := Slices(95*time.Second, 30*time.Second)
	events := []TraceEvent{{Time: time.Second, Bytes: 5}, {Time: 31 * time.Second, Bytes: 6}}
	a1, a2 := Attribute(s, events), Attribute(s, events)
	if !reflect.DeepEqual(a1, a2) {
		t.Fatal("attribute not deterministic")
	}
	e1 := Evaluate("t", 1, s, a1, ExclusionNone)
	e2 := Evaluate("t", 1, s, a2, ExclusionNone)
	if !reflect.DeepEqual(e1, e2) {
		t.Fatal("evaluate not deterministic")
	}
}

func TestPilotAccountingAndSelection(t *testing.T) {
	counts := map[Exclusion]int{ExclusionByteFloor: 2, ExclusionOtherC: 1}
	c := NewCandidate(30*time.Second, 10, 7, counts)
	if c.SurvivalFraction != 0.7 || c.ByteFloorExclusions != 2 || c.TotalExclusions != 3 {
		t.Fatalf("candidate = %+v", c)
	}
	cands := []Candidate{
		{Nominal: 30 * time.Second, ValidEpochs: 2, SurvivalFraction: 1.0},
		{Nominal: 60 * time.Second, ValidEpochs: 5, SurvivalFraction: 0.75},
		{Nominal: 90 * time.Second, ValidEpochs: 4, SurvivalFraction: 0.9},
	}
	got, ok := SelectDuration(cands)
	if !ok || got != 90*time.Second {
		t.Fatalf("selection = %v/%v, want 90s", got, ok)
	}
	if _, ok := SelectDuration([]Candidate{{Nominal: time.Second, ValidEpochs: 2, SurvivalFraction: 1}}); ok {
		t.Fatal("selection must require >= 3 valid epochs")
	}
}

func TestPilotRevisionRules(t *testing.T) {
	base := DefaultProtocol()
	permitted := base
	permitted.Candidates = []time.Duration{45 * time.Second, 60 * time.Second}
	permitted.ByteFloor = ByteFloor / 2
	if err := ValidateRevision(base, permitted); err != nil {
		t.Fatalf("permitted revision rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Protocol){
		"concurrency":  func(p *Protocol) { p.Concurrency = []int{1, 4} },
		"chunk":        func(p *Protocol) { p.ChunkSize = 2 << 20 },
		"fraction":     func(p *Protocol) { p.MinObservedFraction = 0.5 },
		"no candidate": func(p *Protocol) { p.Candidates = nil },
		"floor":        func(p *Protocol) { p.ByteFloor = 0 },
	} {
		p := base
		mutate(&p)
		if err := ValidateRevision(base, p); err == nil {
			t.Errorf("%s revision accepted, want rejection", name)
		}
	}
}

func TestSelectPilotTracesRule(t *testing.T) {
	all := []NativeTrace{
		{ID: "short-fast", Duration: 140 * time.Second, MeanBytesPerSecond: 3e6},
		{ID: "long-fast", Duration: 800 * time.Second, MeanBytesPerSecond: 2e6},
		{ID: "long-slow", Duration: 1000 * time.Second, MeanBytesPerSecond: 1e5},
		{ID: "long-mid", Duration: 900 * time.Second, MeanBytesPerSecond: 1e6},
	}
	good, degraded, ok := SelectPilotTraces(all, 450*time.Second)
	if !ok || good.ID != "long-fast" || degraded.ID != "long-slow" {
		t.Fatalf("classification = %+v / %+v / %v", good, degraded, ok)
	}
	if good.Classification != "good" || degraded.Classification != "degraded" {
		t.Fatalf("classification labels missing: %+v %+v", good, degraded)
	}
}

func TestScanTraceCorpus(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.down"), []byte("0\n100\n200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ScanTraceCorpus(dir)
	if err != nil {
		t.Fatalf("ScanTraceCorpus: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" || got[0].Duration != 200*time.Millisecond || got[0].Lines != 3 {
		t.Fatalf("scan = %+v", got)
	}
	if want := 3 * 1500 / 0.2; got[0].MeanBytesPerSecond != want {
		t.Fatalf("mean rate = %v, want %v", got[0].MeanBytesPerSecond, want)
	}
}

func TestWatchdogOnlyProtocolRevision(t *testing.T) {
	old := LegacyProtocolV1()
	if old.Version != ProtocolVersionV1 || old.Timeout.Request != 120*time.Second || old.Timeout.Dial != 30*time.Second {
		t.Fatalf("legacy v1 protocol not representable: %+v", old)
	}
	if old.Timeout.RequestDisabled || old.Timeout.DialDisabled {
		t.Fatalf("legacy v1 must use numeric deadlines: %+v", old.Timeout)
	}
	next := DefaultProtocol()
	if next.Version != ProtocolVersionV2 || !next.Timeout.WatchdogOnly() {
		t.Fatalf("default protocol not watchdog-only: %+v", next)
	}
	if next.Timeout.ParentWatchdog != DefaultWatchdog || next.Timeout.ChildWatchdog != DefaultWatchdog {
		t.Fatalf("watchdogs = %+v, want %v", next.Timeout, DefaultWatchdog)
	}
	if err := ValidateTimeoutPolicyRevision(old, next); err != nil {
		t.Fatalf("valid watchdog-only revision rejected: %v", err)
	}
	if !equalInts(old.Concurrency, next.Concurrency) || old.ChunkSize != next.ChunkSize ||
		old.ByteFloor != next.ByteFloor || old.MinObservedFraction != next.MinObservedFraction ||
		!equalDurations(old.Candidates, next.Candidates) {
		t.Fatal("revision changed a frozen experimental dimension")
	}

	reject := map[string]func(*Protocol){
		"same version":      func(p *Protocol) { p.Version = ProtocolVersionV1 },
		"not watchdog-only": func(p *Protocol) { p.Timeout.Request = 600 * time.Second },
		"changed watchdog":  func(p *Protocol) { p.Timeout.ParentWatchdog = time.Minute },
		"changed C":         func(p *Protocol) { p.Concurrency = []int{1, 2} },
		"changed chunk":     func(p *Protocol) { p.ChunkSize = 2 << 20 },
		"changed floor":     func(p *Protocol) { p.ByteFloor = 1 },
		"changed epochs":    func(p *Protocol) { p.Candidates = []time.Duration{time.Minute} },
		"changed fraction":  func(p *Protocol) { p.MinObservedFraction = 0.5 },
	}
	for name, mutate := range reject {
		p := next
		mutate(&p)
		if err := ValidateTimeoutPolicyRevision(old, p); err == nil {
			t.Errorf("%s accepted, want rejection", name)
		}
	}

	// Ordinary revisions remain same-version and may not touch the policy.
	permitted := next
	permitted.Candidates = []time.Duration{45 * time.Second}
	permitted.ByteFloor = ByteFloor / 2
	if err := ValidateRevision(next, permitted); err != nil {
		t.Fatalf("ordinary revision rejected: %v", err)
	}
	policyChange := next
	policyChange.Timeout.Request = time.Second
	if err := ValidateRevision(next, policyChange); err == nil {
		t.Fatal("ValidateRevision accepted a timeout-policy change")
	}
	versionChange := next
	versionChange.Version = ProtocolVersionV1
	if err := ValidateRevision(next, versionChange); err == nil {
		t.Fatal("ValidateRevision accepted a version change")
	}
}
