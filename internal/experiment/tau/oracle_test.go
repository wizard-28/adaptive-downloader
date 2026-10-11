package tau

import (
	"math"
	"testing"
)

func epochsFrom(rates [][]float64) []OracleEpoch {
	out := make([]OracleEpoch, len(rates))
	for e, row := range rates {
		m := map[int]float64{}
		for i, c := range Ladder {
			m[c] = row[i]
		}
		out[e] = OracleEpoch{Index: e, Regime: RegimeMiddle, Rates: m}
	}
	return out
}

func zeroTauTable() TauTable {
	t := TauTable{Pooled: map[[2]int]float64{}, RegimeSpecific: map[[2]int]bool{}, RegimeMedians: map[[2]int]map[Regime]float64{}}
	for _, a := range Ladder {
		for _, b := range Ladder {
			if AllowedTransition(a, b) {
				t.Pooled[[2]int{a, b}] = 0
			}
		}
	}
	return t
}

func TestOracleFixedAndInstantaneous(t *testing.T) {
	// Ladder order: 1,2,4,8,16.
	epochs := epochsFrom([][]float64{
		{100, 50, 50, 50, 50},
		{50, 100, 50, 50, 50},
	})
	fixed, err := FixedCompletion(epochs, 100)
	if err != nil {
		t.Fatal(err)
	}
	if fixed[1] != 3 || fixed[2] != 3 || fixed[4] != 4 {
		t.Fatalf("fixed = %v", fixed)
	}
	inst, err := Instantaneous(epochs, 100)
	if err != nil {
		t.Fatal(err)
	}
	if inst != 2 {
		t.Fatalf("instantaneous = %v, want 2", inst)
	}
}

func TestOracleConstrainedDwellLadder(t *testing.T) {
	// Without the dwell rule [1,2,4] would cost 3; the dwell rule forbids
	// changing at two consecutive boundaries, so the optimum is 4.
	epochs := epochsFrom([][]float64{
		{100, 50, 50, 50, 50},
		{50, 100, 50, 50, 50},
		{50, 50, 100, 50, 50},
	})
	sol, err := SolveConstrained(epochs, 100, zeroTauTable())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(sol.Objective-4) > 1e-9 {
		t.Fatalf("objective = %v, want 4", sol.Objective)
	}
	if err := ValidateSchedule(sol.Sequence); err != nil {
		t.Fatalf("sequence %v invalid: %v", sol.Sequence, err)
	}
	if len(sol.Sequence) != 3 || sol.Sequence[0] != 1 || sol.Sequence[1] != 2 {
		t.Fatalf("sequence = %v", sol.Sequence)
	}
	if sol.Sequence[2] == 4 {
		t.Fatalf("dwell rule violated: %v", sol.Sequence)
	}
}

func TestOracleNegativeTauCanBeatInstantaneous(t *testing.T) {
	epochs := epochsFrom([][]float64{
		{100, 50, 50, 50, 50},
		{50, 100, 50, 50, 50},
	})
	taus := zeroTauTable()
	taus.Pooled[[2]int{1, 2}] = -0.5
	sol, err := SolveConstrained(epochs, 100, taus)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := Instantaneous(epochs, 100)
	if !(sol.Objective < inst) {
		t.Fatalf("negative tau did not beat the instantaneous bound: con=%v inst=%v", sol.Objective, inst)
	}
	if len(sol.Sequence) != 2 || sol.Sequence[0] != 1 || sol.Sequence[1] != 2 {
		t.Fatalf("sequence = %v", sol.Sequence)
	}
	// Flooring negative taus restores the lower-bound property.
	nn := taus.NonNegative()
	solNN, err := SolveConstrained(epochs, 100, nn)
	if err != nil {
		t.Fatal(err)
	}
	if solNN.Objective < inst {
		t.Fatalf("nonnegative tau violated the bound: con=%v inst=%v", solNN.Objective, inst)
	}
}

func TestTauTableRegimeFallback(t *testing.T) {
	taus := TauTable{
		Pooled:         map[[2]int]float64{{1, 2}: 0.5},
		RegimeSpecific: map[[2]int]bool{{1, 2}: true},
		RegimeMedians:  map[[2]int]map[Regime]float64{{1, 2}: {RegimeDegraded: 0.1, RegimeRecovering: 2.0}},
	}
	if v, ok := taus.Value(1, 2, RegimeDegraded); !ok || v != 0.1 {
		t.Fatalf("degraded value = %v/%v", v, ok)
	}
	if v, ok := taus.Value(1, 2, RegimeRecovering); !ok || v != 2.0 {
		t.Fatalf("recovering value = %v/%v", v, ok)
	}
	// middle has no regime-specific median: pooled fallback.
	if v, ok := taus.Value(1, 2, RegimeMiddle); !ok || v != 0.5 {
		t.Fatalf("middle fallback = %v/%v", v, ok)
	}
	if _, ok := taus.Value(2, 1, RegimeMiddle); ok {
		t.Fatal("unknown direction accepted")
	}
}

// bruteForce enumerates every dwell/ladder-valid schedule exactly.
func bruteForce(epochs []OracleEpoch, workloadBytes int64, taus TauTable) float64 {
	n := len(epochs)
	best := math.Inf(1)
	seq := make([]int, n)
	var rec func(e int)
	rec = func(e int) {
		if e == n {
			obj := 0.0
			for i := 0; i < n; i++ {
				obj += float64(workloadBytes) / epochs[i].Rates[seq[i]]
			}
			for i := 1; i < n; i++ {
				if seq[i] != seq[i-1] {
					tau, _ := taus.Value(seq[i-1], seq[i], epochs[i].Regime)
					obj += tau
				}
			}
			if obj < best {
				best = obj
			}
			return
		}
		for _, c := range Ladder {
			if e > 0 && c != seq[e-1] {
				if !AllowedTransition(seq[e-1], c) {
					continue
				}
				if e >= 2 && seq[e-1] != seq[e-2] {
					continue // dwell
				}
			}
			seq[e] = c
			rec(e + 1)
		}
	}
	rec(0)
	return best
}

func TestOracleBruteForceCrossCheck(t *testing.T) {
	seed := uint64(20260927)
	next := func() float64 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return 10 + float64(seed%1000)
	}
	taus := zeroTauTable()
	vals := map[[2]int]float64{
		{1, 2}: -0.3, {2, 1}: 0.2, {2, 4}: -0.1, {4, 2}: 0.05,
		{4, 8}: 0.4, {8, 4}: -0.2, {8, 16}: 0.1, {16, 8}: -0.05,
	}
	for k, v := range vals {
		taus.Pooled[k] = v
	}
	regimes := []Regime{RegimeMiddle, RegimeDegraded, RegimeGood, RegimeRecovering}
	for n := 2; n <= 5; n++ {
		for trial := 0; trial < 8; trial++ {
			rates := make([][]float64, n)
			for e := 0; e < n; e++ {
				row := make([]float64, len(Ladder))
				for i := range row {
					row[i] = next()
				}
				rates[e] = row
			}
			epochs := epochsFrom(rates)
			for e := range epochs {
				epochs[e].Regime = regimes[(trial+e)%len(regimes)]
			}
			sol, err := SolveConstrained(epochs, 1<<20, taus)
			if err != nil {
				t.Fatalf("n=%d trial=%d: %v", n, trial, err)
			}
			if err := ValidateSchedule(sol.Sequence); err != nil {
				t.Fatalf("n=%d trial=%d: sequence %v invalid: %v", n, trial, sol.Sequence, err)
			}
			want := bruteForce(epochs, 1<<20, taus)
			if math.Abs(sol.Objective-want) > 1e-9 {
				t.Fatalf("n=%d trial=%d: DP=%v brute=%v", n, trial, sol.Objective, want)
			}
		}
	}
}

func TestOracleStaticIsFeasible(t *testing.T) {
	epochs := epochsFrom([][]float64{
		{100, 80, 60, 40, 20},
		{20, 40, 60, 80, 100},
		{50, 50, 50, 50, 50},
	})
	taus := zeroTauTable()
	taus.Pooled[[2]int{1, 2}] = 0.3
	taus.Pooled[[2]int{2, 4}] = 0.2
	sol, err := SolveConstrained(epochs, 1<<20, taus)
	if err != nil {
		t.Fatal(err)
	}
	fixed, _ := FixedCompletion(epochs, 1<<20)
	best := math.Inf(1)
	for _, v := range fixed {
		if v < best {
			best = v
		}
	}
	if sol.Objective > best+1e-9 {
		t.Fatalf("constrained %v exceeds best fixed %v", sol.Objective, best)
	}
}
