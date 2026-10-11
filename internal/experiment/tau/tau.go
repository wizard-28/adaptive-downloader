// Package tau implements the pre-registered Stage 0 oracle definitions and the
// transition-cost (tau) measurement, classification, selection, and
// aggregation rules. It deliberately performs no headroom computation: the
// oracle formulas are registered here but not evaluated.
//
// All constants and algorithms in this package are frozen before any tau
// measurement or headroom inspection (see
// testdata/oracle_preregistration_v2/oracle_preregistration.json).
package tau

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Ladder is the frozen concurrency ladder for the constrained oracle.
var Ladder = []int{1, 2, 4, 8, 16}

// EpochDuration is the selected formal epoch duration.
const EpochDuration = 120 * time.Second

// ByteFloor is the frozen per-epoch workload B_e. Every C is evaluated against
// this same byte workload for a retained epoch.
const ByteFloor int64 = 5 << 20

// PacketBytes is the native delivery-opportunity payload used by the regime
// classifier (one trace line = one 1500-byte delivery opportunity).
const PacketBytes int64 = 1500

// RecoveringRatio is the frozen native-rate rise that labels an epoch as
// recovering (>= 25% higher than the immediately preceding epoch).
const RecoveringRatio = 1.25

// RegimeDependenceThreshold is the frozen ratio (max/min of per-regime median
// tau) at or above which a direction uses regime-specific tau instead of the
// pooled tau.
const RegimeDependenceThreshold = 2.0

// Regime is the native-trace delivery regime of one full epoch.
type Regime string

const (
	RegimeDegraded   Regime = "degraded"
	RegimeGood       Regime = "good"
	RegimeRecovering Regime = "recovering"
	RegimeMiddle     Regime = "middle"
)

// EpochRate is one full native epoch of the downlink trace. The final partial
// epoch is never represented here: it cannot host a complete injection window.
type EpochRate struct {
	Index              int     `json:"epoch_index"`
	StartMs            int64   `json:"start_ms"`
	EndMs              int64   `json:"end_ms"`
	Opportunities      int     `json:"opportunities"`
	Bytes              int64   `json:"bytes"`
	RateBytesPerSecond float64 `json:"rate_bytes_per_second"`
}

// Injection is one preselected switch-injection timestamp: the start of a full
// epoch with a given native regime.
type Injection struct {
	TraceID    string `json:"trace_id"`
	Regime     Regime `json:"regime"`
	EpochIndex int    `json:"epoch_index"`
	StartMs    int64  `json:"start_ms"`
	EndMs      int64  `json:"end_ms"`
}

// ReadNativeEpochs reads a native downlink trace and partitions it into full
// EpochDuration epochs. The final partial epoch (if any) is dropped. Only
// native delivery opportunities are used; downloader throughput never enters
// the classification.
func ReadNativeEpochs(path string, epoch time.Duration) ([]EpochRate, error) {
	if epoch <= 0 {
		return nil, fmt.Errorf("tau: epoch duration must be positive")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("tau: open trace: %w", err)
	}
	defer f.Close()

	epochMs := epoch.Milliseconds()
	var counts []int64
	var lastMs int64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ms, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("tau: trace %s: %w", path, err)
		}
		if ms < 0 {
			return nil, fmt.Errorf("tau: trace %s: negative timestamp %d", path, ms)
		}
		idx := ms / epochMs
		for int64(len(counts)) <= idx {
			counts = append(counts, 0)
		}
		counts[idx]++
		if ms > lastMs {
			lastMs = ms
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("tau: read trace %s: %w", path, err)
	}
	if len(counts) == 0 {
		return nil, fmt.Errorf("tau: trace %s has no delivery opportunities", path)
	}
	nFull := int(lastMs / epochMs)
	if nFull == 0 {
		return nil, fmt.Errorf("tau: trace %s has no full %v epoch", path, epoch)
	}
	out := make([]EpochRate, 0, nFull)
	for e := 0; e < nFull; e++ {
		var opportunities int64
		if e < len(counts) {
			opportunities = counts[e]
		}
		bytes := opportunities * PacketBytes
		out = append(out, EpochRate{
			Index:              e,
			StartMs:            int64(e) * epochMs,
			EndMs:              int64(e+1) * epochMs,
			Opportunities:      int(opportunities),
			Bytes:              bytes,
			RateBytesPerSecond: float64(bytes) / epoch.Seconds(),
		})
	}
	return out, nil
}

// Classify labels full native epochs. Frozen algorithm:
//
//  1. k = max(1, n/3).
//  2. degraded = the k epochs with the lowest rate (ties: lower index first).
//  3. good = the k highest-rate epochs not already degraded (ties: lower
//     index first); the two sets are disjoint even under complete ties.
//  4. recovering: e > 0 with rate[e] >= 1.25*rate[e-1] and e not degraded.
//  5. Precedence: degraded, then recovering, then good, else middle.
func Classify(rates []EpochRate) []Regime {
	n := len(rates)
	labels := make([]Regime, n)
	if n == 0 {
		return labels
	}
	k := n / 3
	if k < 1 {
		k = 1
	}
	asc := make([]int, n)
	for i := range asc {
		asc[i] = i
	}
	sort.SliceStable(asc, func(a, b int) bool {
		ra, rb := rates[asc[a]].RateBytesPerSecond, rates[asc[b]].RateBytesPerSecond
		if ra != rb {
			return ra < rb
		}
		return asc[a] < asc[b]
	})
	degraded := make(map[int]bool, k)
	for _, i := range asc[:k] {
		degraded[i] = true
	}
	desc := make([]int, n)
	copy(desc, asc)
	sort.SliceStable(desc, func(a, b int) bool {
		ra, rb := rates[desc[a]].RateBytesPerSecond, rates[desc[b]].RateBytesPerSecond
		if ra != rb {
			return ra > rb
		}
		return desc[a] < desc[b]
	})
	good := make(map[int]bool, k)
	for _, i := range desc {
		if len(good) >= k {
			break
		}
		if degraded[i] {
			continue
		}
		good[i] = true
	}
	for e := range rates {
		switch {
		case degraded[e]:
			labels[e] = RegimeDegraded
		case e > 0 && rates[e].RateBytesPerSecond >= RecoveringRatio*rates[e-1].RateBytesPerSecond:
			labels[e] = RegimeRecovering
		case good[e]:
			labels[e] = RegimeGood
		default:
			labels[e] = RegimeMiddle
		}
	}
	return labels
}

// SelectInjections chooses the frozen injection timestamps: for each regime in
// the fixed order degraded, good, recovering, the earliest full epoch with that
// label and, when at least one more exists, the next distinct epoch of the same
// regime. Absent regimes contribute nothing (they are never invented).
func SelectInjections(traceID string, rates []EpochRate, labels []Regime) []Injection {
	var out []Injection
	for _, regime := range []Regime{RegimeDegraded, RegimeGood, RegimeRecovering} {
		var epochs []int
		for i, l := range labels {
			if l == regime {
				epochs = append(epochs, i)
			}
		}
		for n, idx := range epochs {
			if n >= 2 {
				break
			}
			out = append(out, Injection{
				TraceID:    traceID,
				Regime:     regime,
				EpochIndex: idx,
				StartMs:    rates[idx].StartMs,
				EndMs:      rates[idx].EndMs,
			})
		}
	}
	return out
}

// AllowedTransition reports whether a transition is a stay or exactly one
// ladder step.
func AllowedTransition(from, to int) bool {
	fi, okF := ladderIndex(from)
	ti, okT := ladderIndex(to)
	if !okF || !okT {
		return false
	}
	diff := fi - ti
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1
}

func ladderIndex(c int) (int, bool) {
	for i, v := range Ladder {
		if v == c {
			return i, true
		}
	}
	return 0, false
}

// ValidateSchedule checks a per-epoch C schedule against the frozen rules:
// every C is on the ladder, every change is exactly one ladder step, and after
// a change at boundary e the new C must be held for at least the following
// complete epoch (schedule[e+1] == schedule[e]).
func ValidateSchedule(schedule []int) error {
	if len(schedule) == 0 {
		return fmt.Errorf("tau: empty schedule")
	}
	for i, c := range schedule {
		if _, ok := ladderIndex(c); !ok {
			return fmt.Errorf("tau: C=%d at epoch %d is not on the ladder", c, i)
		}
	}
	for e := 1; e < len(schedule); e++ {
		if schedule[e] == schedule[e-1] {
			continue
		}
		if !AllowedTransition(schedule[e-1], schedule[e]) {
			return fmt.Errorf("tau: transition %d->%d at epoch %d is not one ladder step", schedule[e-1], schedule[e], e)
		}
		if e+1 < len(schedule) && schedule[e+1] != schedule[e] {
			return fmt.Errorf("tau: dwell violation at epoch %d: changed to %d and changed again before one complete epoch", e+1, schedule[e])
		}
	}
	return nil
}

// EpochTransferSeconds is the frozen per-epoch transfer time t[c,e] = B_e/R.
// Every C uses the same B_e, so the mapping is C-independent.
func EpochTransferSeconds(rateBytesPerSecond float64) (float64, error) {
	if rateBytesPerSecond <= 0 {
		return 0, fmt.Errorf("tau: non-positive epoch rate %g", rateBytesPerSecond)
	}
	return float64(ByteFloor) / rateBytesPerSecond, nil
}

// Observation is one switch-injection measurement for a direction at a
// preselected timestamp.
type Observation struct {
	TraceID       string  `json:"trace_id"`
	FromC         int     `json:"from_c"`
	ToC           int     `json:"to_c"`
	EpochIndex    int     `json:"epoch_index"`
	Regime        Regime  `json:"regime"`
	SwitchedRun   string  `json:"switched_run"`
	ControlRun    string  `json:"control_run"`
	SwitchedBytes int64   `json:"switched_bytes"`
	ControlBytes  int64   `json:"control_bytes"`
	ControlRate   float64 `json:"control_rate_bytes_per_second"`
	TauSeconds    float64 `json:"tau_seconds"`
	Valid         bool    `json:"valid"`
	Note          string  `json:"note,omitempty"`
}

// DirectionAggregate is the frozen aggregation of one transition direction.
type DirectionAggregate struct {
	FromC              int                `json:"from_c"`
	ToC                int                `json:"to_c"`
	TauSeconds         float64            `json:"tau_seconds"`
	PooledTauSeconds   float64            `json:"pooled_tau_seconds"`
	PooledN            int                `json:"pooled_n"`
	RegimeMedians      map[Regime]float64 `json:"regime_medians"`
	RegimeCounts       map[Regime]int     `json:"regime_counts"`
	RegimeSpecific     bool               `json:"regime_specific"`
	RegimeRatio        float64            `json:"regime_ratio,omitempty"`
	RegimeRatioDefined bool               `json:"regime_ratio_defined"`
	PerRegimeTau       map[Regime]float64 `json:"per_regime_tau,omitempty"`
	Reason             string             `json:"reason,omitempty"`

	// FormalCostSeconds is the versioned nonnegative formal transition cost:
	// max(0, tau_seconds). It is computed AFTER the estimator; raw signed
	// observations are retained separately as diagnostics.
	FormalCostSeconds  float64            `json:"formal_tau_seconds"`
	FormalPerRegimeTau map[Regime]float64 `json:"formal_per_regime_tau,omitempty"`
}

// FormalCost applies the versioned nonnegative-cost transform to an estimated
// tau: tau_formal = max(0, estimate). It is applied only after the
// preregistered estimator (median, optionally regime-specific) and never to
// individual raw observations.
func FormalCost(estimate float64) float64 {
	if estimate < 0 {
		return 0
	}
	return estimate
}

// Aggregate applies the frozen tau aggregation rule per direction:
//
//   - pooled tau = median of all valid observations for the direction;
//   - per-regime medians over valid observations with >= 1 observation;
//   - regime-specific tau is used iff at least two regime medians exist, all
//     medians are > 0, and max/min >= threshold;
//   - otherwise the pooled tau is used; a zero/negative denominator is
//     reported explicitly (never silently divided).
func Aggregate(observations []Observation, threshold float64) ([]DirectionAggregate, error) {
	if threshold <= 0 {
		return nil, fmt.Errorf("tau: regime threshold must be positive")
	}
	type key struct{ from, to int }
	groups := make(map[key][]Observation)
	var order []key
	for _, o := range observations {
		k := key{o.FromC, o.ToC}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], o)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].from != order[j].from {
			return order[i].from < order[j].from
		}
		return order[i].to < order[j].to
	})
	var out []DirectionAggregate
	for _, k := range order {
		obs := groups[k]
		agg := DirectionAggregate{
			FromC:         k.from,
			ToC:           k.to,
			RegimeMedians: map[Regime]float64{},
			RegimeCounts:  map[Regime]int{},
		}
		byRegime := map[Regime][]float64{}
		var pooledVals []float64
		for _, o := range obs {
			if !o.Valid {
				continue
			}
			pooledVals = append(pooledVals, o.TauSeconds)
			byRegime[o.Regime] = append(byRegime[o.Regime], o.TauSeconds)
		}
		if len(pooledVals) == 0 {
			agg.Reason = "no valid observations"
			out = append(out, agg)
			continue
		}
		agg.PooledN = len(pooledVals)
		agg.PooledTauSeconds = Median(pooledVals)
		for _, regime := range []Regime{RegimeDegraded, RegimeGood, RegimeRecovering} {
			vals := byRegime[regime]
			if len(vals) == 0 {
				continue
			}
			agg.RegimeCounts[regime] = len(vals)
			agg.RegimeMedians[regime] = Median(vals)
		}
		var medians []float64
		for _, regime := range []Regime{RegimeDegraded, RegimeGood, RegimeRecovering} {
			if m, ok := agg.RegimeMedians[regime]; ok {
				medians = append(medians, m)
			}
		}
		switch {
		case len(medians) < 2:
			agg.Reason = "fewer than two regimes with valid observations"
		default:
			minM, maxM := medians[0], medians[0]
			for _, m := range medians[1:] {
				if m < minM {
					minM = m
				}
				if m > maxM {
					maxM = m
				}
			}
			if minM <= 0 {
				agg.Reason = fmt.Sprintf("regime ratio undefined: non-positive regime median (min=%g)", minM)
				break
			}
			agg.RegimeRatio = maxM / minM
			agg.RegimeRatioDefined = true
			if agg.RegimeRatio >= threshold {
				agg.RegimeSpecific = true
				agg.PerRegimeTau = map[Regime]float64{}
				for regime, m := range agg.RegimeMedians {
					agg.PerRegimeTau[regime] = m
				}
			}
		}
		agg.TauSeconds = agg.PooledTauSeconds
		// Versioned nonnegative formal cost, applied after the estimator.
		agg.FormalCostSeconds = FormalCost(agg.TauSeconds)
		if agg.RegimeSpecific {
			agg.FormalPerRegimeTau = map[Regime]float64{}
			for regime, m := range agg.PerRegimeTau {
				agg.FormalPerRegimeTau[regime] = FormalCost(m)
			}
		}
		out = append(out, agg)
	}
	return out, nil
}

// Median returns the median of vals (average of the two middle values for an
// even count). The input is not modified.
func Median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
