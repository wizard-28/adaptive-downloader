package tau

import (
	"fmt"
	"math"
	"sort"
)

// TauTable is the frozen transition-cost lookup used by the constrained
// oracle. Regime-specific values are used only where the pre-registered
// aggregation selected them; epochs whose regime has no specific median (for
// example "middle" epochs for the 1->2 direction) fall back to the direction's
// pooled tau.
type TauTable struct {
	Pooled         map[[2]int]float64
	RegimeSpecific map[[2]int]bool
	RegimeMedians  map[[2]int]map[Regime]float64
}

// Value returns the frozen tau for a transition entering an epoch with the
// given native regime label.
func (t TauTable) Value(from, to int, regime Regime) (float64, bool) {
	key := [2]int{from, to}
	pooled, ok := t.Pooled[key]
	if !ok {
		return 0, false
	}
	if t.RegimeSpecific[key] {
		if m, ok := t.RegimeMedians[key]; ok {
			if v, ok := m[regime]; ok {
				return v, true
			}
		}
		// No regime-specific median for this label: use the pooled value.
	}
	return pooled, true
}

// RegimePolicy reports whether a direction is regime-specific.
func (t TauTable) RegimePolicy(from, to int) bool {
	return t.RegimeSpecific[[2]int{from, to}]
}

// Formalized returns a copy with every estimated pooled/regime value replaced
// by the versioned nonnegative formal cost max(0, estimate). This is the
// formal tau table; NonNegative is retained as the historical sensitivity
// helper (identical arithmetic, different provenance).
func (t TauTable) Formalized() TauTable {
	return t.NonNegative()
}

// NonNegative returns a copy with every negative pooled/regime value replaced
// by zero. It was introduced as the secondary sensitivity analysis and is
// retained for historical comparison.
func (t TauTable) NonNegative() TauTable {
	out := TauTable{
		Pooled:         map[[2]int]float64{},
		RegimeSpecific: map[[2]int]bool{},
		RegimeMedians:  map[[2]int]map[Regime]float64{},
	}
	for k, v := range t.Pooled {
		if v < 0 {
			v = 0
		}
		out.Pooled[k] = v
	}
	for k, v := range t.RegimeSpecific {
		out.RegimeSpecific[k] = v
	}
	for k, m := range t.RegimeMedians {
		nm := map[Regime]float64{}
		for r, v := range m {
			if v < 0 {
				v = 0
			}
			nm[r] = v
		}
		out.RegimeMedians[k] = nm
	}
	return out
}

// ZeroTau returns a copy with all transition costs set to zero (diagnostic).
func (t TauTable) ZeroTau() TauTable {
	out := TauTable{Pooled: map[[2]int]float64{}, RegimeSpecific: map[[2]int]bool{}, RegimeMedians: map[[2]int]map[Regime]float64{}}
	for k := range t.Pooled {
		out.Pooled[k] = 0
	}
	for k, v := range t.RegimeSpecific {
		out.RegimeSpecific[k] = v
	}
	for k, m := range t.RegimeMedians {
		nm := map[Regime]float64{}
		for r := range m {
			nm[r] = 0
		}
		out.RegimeMedians[k] = nm
	}
	return out
}

// OracleEpoch is one retained shared-valid epoch with its per-C measured rate
// and its pre-registered native regime label.
type OracleEpoch struct {
	Index  int
	Regime Regime
	Rates  map[int]float64 // C -> bytes/sec
}

// validateEpochs requires every ladder C to have a positive rate.
func validateEpochs(epochs []OracleEpoch) error {
	if len(epochs) == 0 {
		return fmt.Errorf("oracle: no epochs")
	}
	for _, e := range epochs {
		for _, c := range Ladder {
			r, ok := e.Rates[c]
			if !ok || r <= 0 {
				return fmt.Errorf("oracle: epoch %d has no positive rate for C=%d", e.Index, c)
			}
		}
	}
	return nil
}

// FixedCompletion returns T_fixed(C) = sum_e B/R[C,e] for every ladder C.
func FixedCompletion(epochs []OracleEpoch, workloadBytes int64) (map[int]float64, error) {
	if err := validateEpochs(epochs); err != nil {
		return nil, err
	}
	out := map[int]float64{}
	for _, c := range Ladder {
		var sum float64
		for _, e := range epochs {
			sum += float64(workloadBytes) / e.Rates[c]
		}
		out[c] = sum
	}
	return out, nil
}

// Instantaneous returns the diagnostic instantaneous oracle
// T_instantaneous = sum_e B/max_C R[C,e] (no tau, no constraints).
func Instantaneous(epochs []OracleEpoch, workloadBytes int64) (float64, error) {
	if err := validateEpochs(epochs); err != nil {
		return 0, err
	}
	var sum float64
	for _, e := range epochs {
		best := 0.0
		for _, c := range Ladder {
			if e.Rates[c] > best {
				best = e.Rates[c]
			}
		}
		sum += float64(workloadBytes) / best
	}
	return sum, nil
}

// TransitionDetail audits one change in a constrained sequence.
type TransitionDetail struct {
	EpochIndex int     `json:"epoch_index"`
	FromC      int     `json:"from_c"`
	ToC        int     `json:"to_c"`
	Regime     Regime  `json:"regime"`
	Tau        float64 `json:"tau_seconds"`
}

// OracleSolution is the exact constrained-oracle result for one trace.
type OracleSolution struct {
	Objective       float64            `json:"objective_seconds"`
	Sequence        []int              `json:"sequence"`
	Transitions     int                `json:"transitions"`
	TransitionAudit []TransitionDetail `json:"transition_details"`
}

// SolveConstrained solves the exact frozen dynamic program:
//
//	minimize sum_e B/R[C_e,e] + sum_changes tau(C_{e-1}->C_e, regime[e])
//
// subject to the ladder (stay or one adjacent step), the dwell rule (after a
// change at boundary e, C_{e+1} = C_e), and stay always allowed. Transition
// costs use the pre-registered native regime label of the entered epoch e.
func SolveConstrained(epochs []OracleEpoch, workloadBytes int64, taus TauTable) (OracleSolution, error) {
	if err := validateEpochs(epochs); err != nil {
		return OracleSolution{}, err
	}
	n := len(epochs)
	type state struct{ c, prev int } // prev: 0 = no change at this boundary, 1 = change
	const noChange, changed = 0, 1
	idx := map[int]int{}
	for i, c := range Ladder {
		idx[c] = i
	}
	inf := math.Inf(1)
	cost := make([]map[int][2]float64, n)
	parent := make([]map[int][2]state, n)
	for e := range cost {
		cost[e] = map[int][2]float64{}
		parent[e] = map[int][2]state{}
		for _, c := range Ladder {
			cost[e][c] = [2]float64{inf, inf}
			parent[e][c] = [2]state{{-1, -1}, {-1, -1}}
		}
	}
	for _, c := range Ladder {
		v := cost[0][c]
		v[noChange] = float64(workloadBytes) / epochs[0].Rates[c]
		cost[0][c] = v
	}
	for e := 1; e < n; e++ {
		regime := epochs[e].Regime
		for _, c := range Ladder {
			// Stay.
			best := math.Inf(1)
			bestPrev := noChange
			for _, prev := range []int{noChange, changed} {
				if cost[e-1][c][prev] < best {
					best = cost[e-1][c][prev]
					bestPrev = prev
				}
			}
			if !math.IsInf(best, 1) {
				v := cost[e][c]
				v[noChange] = best + float64(workloadBytes)/epochs[e].Rates[c]
				cost[e][c] = v
				pv := parent[e][c]
				pv[noChange] = state{c, bestPrev}
				parent[e][c] = pv
			}
			// Change: only from a previous boundary with no change (dwell).
			for _, cp := range Ladder {
				if cp == c || absInt(idx[cp]-idx[c]) != 1 {
					continue
				}
				base := cost[e-1][cp][noChange]
				if math.IsInf(base, 1) {
					continue
				}
				tau, ok := taus.Value(cp, c, regime)
				if !ok {
					return OracleSolution{}, fmt.Errorf("oracle: no tau for %d->%d", cp, c)
				}
				val := base + tau + float64(workloadBytes)/epochs[e].Rates[c]
				if val < cost[e][c][changed] {
					v := cost[e][c]
					v[changed] = val
					cost[e][c] = v
					pv := parent[e][c]
					pv[changed] = state{cp, noChange}
					parent[e][c] = pv
				}
			}
		}
	}
	best := math.Inf(1)
	bestC, bestPrev := -1, noChange
	for _, c := range Ladder {
		for _, prev := range []int{noChange, changed} {
			if cost[n-1][c][prev] < best {
				best = cost[n-1][c][prev]
				bestC, bestPrev = c, prev
			}
		}
	}
	if bestC < 0 {
		return OracleSolution{}, fmt.Errorf("oracle: no feasible schedule")
	}
	seq := make([]int, n)
	c, prev := bestC, bestPrev
	for e := n - 1; e >= 0; e-- {
		seq[e] = c
		if e == 0 {
			break
		}
		p := parent[e][c][prev]
		c, prev = p.c, p.prev
	}
	sol := OracleSolution{Objective: best, Sequence: seq}
	for e := 1; e < n; e++ {
		if seq[e] != seq[e-1] {
			tau, _ := taus.Value(seq[e-1], seq[e], epochs[e].Regime)
			sol.TransitionAudit = append(sol.TransitionAudit, TransitionDetail{
				EpochIndex: e, FromC: seq[e-1], ToC: seq[e], Regime: epochs[e].Regime, Tau: tau,
			})
		}
	}
	sol.Transitions = len(sol.TransitionAudit)
	return sol, nil
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// SortedLadder returns a sorted copy of the ladder (defensive helper for
// deterministic artifact output).
func SortedLadder() []int {
	out := append([]int(nil), Ladder...)
	sort.Ints(out)
	return out
}
