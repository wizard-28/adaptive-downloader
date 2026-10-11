package tau

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func ratesFrom(vals ...float64) []EpochRate {
	out := make([]EpochRate, len(vals))
	for i, v := range vals {
		out[i] = EpochRate{Index: i, StartMs: int64(i) * 120000, EndMs: int64(i+1) * 120000, RateBytesPerSecond: v}
	}
	return out
}

// TestByteFloorIdenticalAcrossC verifies B_e is the same for every C and that
// t = B_e/R.
func TestByteFloorIdenticalAcrossC(t *testing.T) {
	if ByteFloor != 5<<20 {
		t.Fatalf("ByteFloor = %d, want 5 MiB", ByteFloor)
	}
	// The same B_e is used regardless of which C produced the rate.
	for _, rate := range []float64{10, 1000, 1e6} {
		got, err := EpochTransferSeconds(rate)
		if err != nil {
			t.Fatal(err)
		}
		want := float64(ByteFloor) / rate
		if got != want {
			t.Fatalf("t(%g) = %v, want %v", rate, got, want)
		}
	}
	if _, err := EpochTransferSeconds(0); err == nil {
		t.Fatal("zero rate accepted")
	}
}

// TestAllowedTransitionLadder covers the frozen action ladder.
func TestAllowedTransitionLadder(t *testing.T) {
	for _, c := range Ladder {
		if !AllowedTransition(c, c) {
			t.Errorf("stay %d->%d rejected", c, c)
		}
	}
	for _, p := range [][2]int{{1, 2}, {2, 1}, {2, 4}, {4, 2}, {4, 8}, {8, 4}, {8, 16}, {16, 8}} {
		if !AllowedTransition(p[0], p[1]) {
			t.Errorf("adjacent %d->%d rejected", p[0], p[1])
		}
	}
	for _, p := range [][2]int{{1, 4}, {1, 8}, {1, 16}, {16, 1}, {2, 8}, {4, 16}, {0, 1}, {3, 4}} {
		if AllowedTransition(p[0], p[1]) {
			t.Errorf("jump %d->%d accepted", p[0], p[1])
		}
	}
}

// TestValidateSchedule covers one-step-per-epoch and the dwell rule.
func TestValidateSchedule(t *testing.T) {
	valid := [][]int{
		{1, 1, 1},
		{1, 2, 2, 4, 4, 8, 8, 16},
		{16, 16, 8, 8, 4, 4, 2, 2, 1},
		{2, 4, 4},
	}
	for _, s := range valid {
		if err := ValidateSchedule(s); err != nil {
			t.Errorf("valid schedule %v rejected: %v", s, err)
		}
	}
	invalid := map[string][]int{
		"empty":        {},
		"off ladder":   {1, 3, 3},
		"direct jump":  {1, 4, 4},
		"reverse jump": {16, 4, 4},
		"dwell":        {1, 2, 4, 4},
		"dwell middle": {2, 2, 4, 2},
	}
	for name, s := range invalid {
		if err := ValidateSchedule(s); err == nil {
			t.Errorf("%s schedule %v accepted", name, s)
		}
	}
}

// TestClassifyDeterministic uses a hand-computed fixture.
func TestClassifyDeterministic(t *testing.T) {
	r := ratesFrom(10, 20, 15, 40, 5, 25, 50, 30)
	got := Classify(r)
	// k=2: degraded={4,0}, good={6,3}; epochs 3 and 6 are top-tertile but
	// recovering takes precedence, so no epoch receives the good label.
	want := []Regime{RegimeDegraded, RegimeRecovering, RegimeMiddle, RegimeRecovering, RegimeDegraded, RegimeRecovering, RegimeRecovering, RegimeMiddle}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	if again := Classify(r); !reflect.DeepEqual(again, got) {
		t.Fatal("classification is not deterministic")
	}
}

// TestClassifyTiesAreDisjointAndDeterministic covers complete ties.
func TestClassifyTiesAreDisjointAndDeterministic(t *testing.T) {
	r := ratesFrom(10, 10, 10, 10)
	got := Classify(r)
	if got[0] != RegimeDegraded {
		t.Fatalf("epoch 0 = %s, want degraded", got[0])
	}
	degraded, good := 0, 0
	for _, l := range got {
		switch l {
		case RegimeDegraded:
			degraded++
		case RegimeGood:
			good++
		}
	}
	if degraded != 1 || good != 1 {
		t.Fatalf("tie split = degraded %d good %d, want 1/1", degraded, good)
	}
}

// TestSelectInjectionsDeterministic covers earliest + next distinct per regime
// in the frozen regime order, and absent regimes contributing nothing.
func TestSelectInjectionsDeterministic(t *testing.T) {
	r := ratesFrom(10, 20, 15, 40, 5, 25, 50, 30)
	labels := Classify(r)
	got := SelectInjections("t", r, labels)
	want := []Injection{
		{TraceID: "t", Regime: RegimeDegraded, EpochIndex: 0, StartMs: 0, EndMs: 120000},
		{TraceID: "t", Regime: RegimeDegraded, EpochIndex: 4, StartMs: 480000, EndMs: 600000},
		{TraceID: "t", Regime: RegimeRecovering, EpochIndex: 1, StartMs: 120000, EndMs: 240000},
		{TraceID: "t", Regime: RegimeRecovering, EpochIndex: 3, StartMs: 360000, EndMs: 480000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("injections = %+v, want %+v", got, want)
	}
	if again := SelectInjections("t", r, labels); !reflect.DeepEqual(again, got) {
		t.Fatal("selection is not deterministic")
	}
	// A single-epoch trace selects its one degraded epoch and no
	// good/recovering injection (absent regimes are never invented).
	one := ratesFrom(10)
	gotOne := SelectInjections("m", one, Classify(one))
	if len(gotOne) != 1 || gotOne[0].Regime != RegimeDegraded || gotOne[0].EpochIndex != 0 {
		t.Fatalf("single-epoch trace selected %+v", gotOne)
	}
}

func obs(from, to int, regime Regime, tau float64) Observation {
	return Observation{FromC: from, ToC: to, Regime: regime, TauSeconds: tau, Valid: true}
}

// TestAggregateThresholdExactly applies the 2.0 threshold exactly.
func TestAggregateThresholdExactly(t *testing.T) {
	// medians: degraded 2.0, good 1.0 -> ratio exactly 2.0 -> regime-specific.
	exact := []Observation{
		obs(1, 2, RegimeDegraded, 2.0),
		obs(1, 2, RegimeGood, 1.0),
	}
	aggs, err := Aggregate(exact, RegimeDependenceThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if !aggs[0].RegimeSpecific || !aggs[0].RegimeRatioDefined || aggs[0].RegimeRatio != 2.0 {
		t.Fatalf("exact threshold aggregate = %+v", aggs[0])
	}
	if aggs[0].PerRegimeTau[RegimeDegraded] != 2.0 || aggs[0].PerRegimeTau[RegimeGood] != 1.0 {
		t.Fatalf("per-regime tau = %v", aggs[0].PerRegimeTau)
	}
	// 1.99 < 2.0 -> pooled.
	below := []Observation{
		obs(1, 2, RegimeDegraded, 1.99),
		obs(1, 2, RegimeGood, 1.0),
	}
	aggs, err = Aggregate(below, RegimeDependenceThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if aggs[0].RegimeSpecific || aggs[0].TauSeconds != Median([]float64{1.99, 1.0}) {
		t.Fatalf("below threshold aggregate = %+v", aggs[0])
	}
}

// TestAggregatePooledMedian covers odd and even pooled medians.
func TestAggregatePooledMedian(t *testing.T) {
	odd := []Observation{obs(1, 2, RegimeDegraded, 3), obs(1, 2, RegimeDegraded, 1), obs(1, 2, RegimeDegraded, 2)}
	aggs, err := Aggregate(odd, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	if aggs[0].PooledTauSeconds != 2 || aggs[0].PooledN != 3 {
		t.Fatalf("odd pooled = %+v", aggs[0])
	}
	even := []Observation{obs(1, 2, RegimeDegraded, 4), obs(1, 2, RegimeDegraded, 1)}
	aggs, err = Aggregate(even, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	if aggs[0].PooledTauSeconds != 2.5 {
		t.Fatalf("even pooled = %+v", aggs[0])
	}
}

// TestAggregateRegimeSpecific checks per-regime medians are selected.
func TestAggregateRegimeSpecific(t *testing.T) {
	observations := []Observation{
		obs(4, 8, RegimeDegraded, 10), obs(4, 8, RegimeDegraded, 12),
		obs(4, 8, RegimeRecovering, 1), obs(4, 8, RegimeRecovering, 3),
	}
	aggs, err := Aggregate(observations, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	a := aggs[0]
	if !a.RegimeSpecific {
		t.Fatalf("not regime-specific: %+v", a)
	}
	if a.PerRegimeTau[RegimeDegraded] != 11 || a.PerRegimeTau[RegimeRecovering] != 2 {
		t.Fatalf("per-regime medians = %v", a.PerRegimeTau)
	}
	if a.RegimeCounts[RegimeDegraded] != 2 || a.RegimeCounts[RegimeRecovering] != 2 {
		t.Fatalf("regime counts = %v", a.RegimeCounts)
	}
}

// TestAggregateNegativeTauPreserved verifies negative observations are never
// clamped, and a non-positive regime median makes the ratio undefined with an
// explicit reason.
func TestAggregateNegativeTauPreserved(t *testing.T) {
	observations := []Observation{
		obs(1, 2, RegimeDegraded, -5), obs(1, 2, RegimeDegraded, -3),
		obs(1, 2, RegimeGood, 1),
	}
	aggs, err := Aggregate(observations, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	a := aggs[0]
	if a.RegimeMedians[RegimeDegraded] != -4 {
		t.Fatalf("negative median was altered: %v", a.RegimeMedians)
	}
	if a.RegimeRatioDefined {
		t.Fatal("ratio reported as defined with a non-positive median")
	}
	if a.Reason == "" {
		t.Fatal("undefined ratio must carry an explicit reason")
	}
	if a.TauSeconds != Median([]float64{-5, -3, 1}) {
		t.Fatalf("pooled tau = %v", a.TauSeconds)
	}
}

// TestTauSecondsFormula covers the frozen transition-cost formula and the
// zero-denominator case.
func TestTauSecondsFormula(t *testing.T) {
	window := 120 * time.Second
	// Control 12 MiB, switched 9 MiB -> deficit 3 MiB; rate 100 KiB/s -> tau 30 s.
	got, rate, err := TauSeconds(9<<20, 12<<20, window)
	if err != nil {
		t.Fatal(err)
	}
	if rate != float64(12<<20)/120 || got != 30 {
		t.Fatalf("tau = %v rate = %v, want 30 s / 100 KiB/s", got, rate)
	}
	// Negative deficit preserved.
	got, _, err = TauSeconds(13<<20, 12<<20, window)
	if err != nil {
		t.Fatal(err)
	}
	if got >= 0 {
		t.Fatalf("negative deficit produced non-negative tau %v", got)
	}
	if _, _, err := TauSeconds(10, 0, window); err == nil {
		t.Fatal("zero control bytes accepted")
	}
}

// TestWindowBytesHalfOpen checks read-time windowing against the anchor.
func TestWindowBytesHalfOpen(t *testing.T) {
	const origin = int64(1_000_000_000)
	events := []Event{
		{WallUnixNano: origin + int64(119*time.Second), Delta: 1}, // before
		{WallUnixNano: origin + int64(120*time.Second), Delta: 2}, // start (inclusive)
		{WallUnixNano: origin + int64(179*time.Second), Delta: 4},
		{WallUnixNano: origin + int64(240*time.Second), Delta: 8}, // end (exclusive)
	}
	if got := WindowBytes(events, origin, 120*time.Second, 240*time.Second); got != 6 {
		t.Fatalf("window bytes = %d, want 6", got)
	}
}

// TestReadNativeEpochsAndInputsReadOnly reads a trace fixture and verifies the
// input files are never modified.
func TestReadNativeEpochsAndInputsReadOnly(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "t.down")
	// Build a deterministic trace: epoch e has 100*(e+1) opportunities.
	var sb []byte
	for e := 0; e < 4; e++ {
		for i := 0; i < 100*(e+1); i++ {
			sb = append(sb, []byte(itoa(int64(e*120000+i%120000))+"\n")...)
		}
	}
	// A final opportunity at 500000 ms makes epoch 3 complete (the stamp
	// itself belongs to the dropped partial epoch 4).
	sb = append(sb, []byte("500000\n")...)
	if err := os.WriteFile(trace, sb, 0o644); err != nil {
		t.Fatal(err)
	}
	before := hashFile(t, trace)
	rates, err := ReadNativeEpochs(trace, EpochDuration)
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 4 {
		t.Fatalf("epochs = %d, want 4", len(rates))
	}
	for e, r := range rates {
		if r.Opportunities != 100*(e+1) {
			t.Fatalf("epoch %d opportunities = %d", e, r.Opportunities)
		}
		want := float64(r.Opportunities) * float64(PacketBytes) / EpochDuration.Seconds()
		if r.RateBytesPerSecond != want {
			t.Fatalf("epoch %d rate = %v, want %v", e, r.RateBytesPerSecond, want)
		}
	}
	if after := hashFile(t, trace); after != before {
		t.Fatal("input trace was modified by the classifier")
	}
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(events, []byte(`{"wall_unix_nano":1,"delta":10}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evBefore := hashFile(t, events)
	if _, err := LoadEvents(events); err != nil {
		t.Fatal(err)
	}
	if after := hashFile(t, events); after != evBefore {
		t.Fatal("input events file was modified")
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := v < 0
	if neg {
		v = -v
	}
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

func hashFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestFormalCostNonnegative verifies the versioned formal-cost transform:
// negative estimates become 0, nonnegative estimates are unchanged.
func TestFormalCostNonnegative(t *testing.T) {
	if got := FormalCost(-0.13); got != 0 {
		t.Fatalf("FormalCost(-0.13) = %v, want 0", got)
	}
	if got := FormalCost(0); got != 0 {
		t.Fatalf("FormalCost(0) = %v, want 0", got)
	}
	if got := FormalCost(0.57); got != 0.57 {
		t.Fatalf("FormalCost(0.57) = %v, want 0.57", got)
	}
}

// TestAggregateFormalCost applies the transform after the estimator: raw
// observations and signed medians are preserved, and only the formal values
// are floored.
func TestAggregateFormalCost(t *testing.T) {
	observations := []Observation{
		obs(8, 4, RegimeDegraded, -0.30), obs(8, 4, RegimeDegraded, -0.10),
		obs(8, 4, RegimeDegraded, 0.05),
		obs(16, 8, RegimeRecovering, 0.40), obs(16, 8, RegimeRecovering, 0.60),
	}
	aggs, err := Aggregate(observations, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	var eightToFour, sixteenToEight *DirectionAggregate
	for i := range aggs {
		switch {
		case aggs[i].FromC == 8 && aggs[i].ToC == 4:
			eightToFour = &aggs[i]
		case aggs[i].FromC == 16 && aggs[i].ToC == 8:
			sixteenToEight = &aggs[i]
		}
	}
	if eightToFour == nil || sixteenToEight == nil {
		t.Fatal("missing directions")
	}
	// Signed pooled median is -0.10 (preserved); formal cost is 0.
	if eightToFour.PooledTauSeconds != -0.10 {
		t.Fatalf("signed pooled = %v, want -0.10", eightToFour.PooledTauSeconds)
	}
	if eightToFour.FormalCostSeconds != 0 {
		t.Fatalf("formal = %v, want 0", eightToFour.FormalCostSeconds)
	}
	// Positive estimator unchanged.
	if sixteenToEight.FormalCostSeconds != 0.50 {
		t.Fatalf("formal = %v, want 0.50", sixteenToEight.FormalCostSeconds)
	}
}

// TestFormalizedTableFloorsEstimates checks the oracle-facing formal table.
func TestFormalizedTableFloorsEstimates(t *testing.T) {
	taus := TauTable{
		Pooled:         map[[2]int]float64{{8, 4}: -0.13, {16, 8}: 0.25},
		RegimeSpecific: map[[2]int]bool{{8, 4}: true},
		RegimeMedians:  map[[2]int]map[Regime]float64{{8, 4}: {RegimeDegraded: -0.13, RegimeRecovering: 0.4}},
	}
	f := taus.Formalized()
	if v, _ := f.Value(8, 4, RegimeDegraded); v != 0 {
		t.Fatalf("formal degraded = %v, want 0", v)
	}
	if v, _ := f.Value(8, 4, RegimeRecovering); v != 0.4 {
		t.Fatalf("formal recovering = %v, want 0.4", v)
	}
	if v, _ := f.Value(16, 8, RegimeMiddle); v != 0.25 {
		t.Fatalf("formal 16->8 = %v, want 0.25", v)
	}
	// The original table is untouched (raw estimates preserved).
	if v, _ := taus.Value(8, 4, RegimeDegraded); v != -0.13 {
		t.Fatalf("original estimate changed: %v", v)
	}
}
