// Package epoch implements trace-time epoch construction, byte attribution,
// epoch validation, shared (cross-C) epoch exclusion, and the pilot accounting
// for the epoch-duration calibration. It performs no tau/oracle/headroom work.
package epoch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"adaptivedl/internal/experiment/replay"
)

const (
	// ByteFloor is the minimum received bytes per C per epoch for the epoch to
	// be a sufficient measurement (measurement-sufficiency floor, not a
	// request-completion floor).
	ByteFloor int64 = 5 << 20
	// MinObservedFraction is the minimum realized/nominal epoch duration.
	MinObservedFraction = 0.80
	// PilotSurvivalThreshold is the required fraction of candidate epochs that
	// satisfy the byte observation requirement.
	PilotSurvivalThreshold = 0.80
	// PilotMinValidEpochs is the minimum valid epochs per pilot replay.
	PilotMinValidEpochs = 3
)

// Slice is one trace-time epoch window [Start, End). Boundaries are expressed
// in the calibrated trace-time coordinate and are identical for every C.
type Slice struct {
	Index   int
	Nominal time.Duration
	Start   time.Duration
	End     time.Duration
}

// Slices partitions [0, traceDuration) into nominal-length epochs; the last
// epoch is partial when the trace does not divide evenly.
func Slices(traceDuration, nominal time.Duration) []Slice {
	if traceDuration <= 0 || nominal <= 0 {
		return nil
	}
	var out []Slice
	for i, start := 0, time.Duration(0); start < traceDuration; i, start = i+1, start+nominal {
		end := start + nominal
		if end > traceDuration {
			end = traceDuration
		}
		out = append(out, Slice{Index: i, Nominal: nominal, Start: start, End: end})
	}
	return out
}

// TraceEvent is one byte observation already mapped to trace time.
type TraceEvent struct {
	Time  time.Duration
	Bytes int64
}

// Attribute assigns each event's bytes to the half-open epoch containing its
// read time. Events must be sorted by Time; events outside [0, last End) are
// dropped (they cannot belong to any epoch).
func Attribute(slices []Slice, events []TraceEvent) []int64 {
	bytes := make([]int64, len(slices))
	if len(slices) == 0 {
		return bytes
	}
	traceEnd := slices[len(slices)-1].End
	j := 0
	for _, e := range events {
		if e.Bytes <= 0 || e.Time < 0 || e.Time >= traceEnd {
			continue
		}
		for j < len(slices) && e.Time >= slices[j].End {
			j++
		}
		if j < len(slices) && e.Time >= slices[j].Start {
			bytes[j] += e.Bytes
		}
	}
	return bytes
}

// Exclusion is a pre-registered epoch exclusion reason.
type Exclusion string

const (
	ExclusionNone               Exclusion = ""
	ExclusionByteFloor          Exclusion = "below_byte_floor"
	ExclusionObservedFraction   Exclusion = "below_observed_fraction"
	ExclusionOtherC             Exclusion = "excluded_by_other_c"
	ExclusionAnchorAlignment    Exclusion = "anchor_alignment"
	ExclusionTraceContamination Exclusion = "trace_contamination"
)

// Epoch is the machine-readable epoch record.
type Epoch struct {
	TraceID          string        `json:"trace_id"`
	C                int           `json:"c"`
	Index            int           `json:"epoch_index"`
	TraceStart       time.Duration `json:"trace_start_time"`
	TraceEnd         time.Duration `json:"trace_end_time"`
	Nominal          time.Duration `json:"nominal_duration"`
	Observed         time.Duration `json:"observed_duration"`
	ObservedFraction float64       `json:"observed_fraction"`
	BytesReceived    int64         `json:"bytes_received"`
	R                float64       `json:"r_bytes_per_second"`
	Valid            bool          `json:"valid"`
	Exclusion        Exclusion     `json:"exclusion_reason,omitempty"`
}

// Evaluate builds the per-run epoch records. runInvalid non-empty marks every
// epoch of the run invalid (anchor/alignment or trace-contamination failure).
func Evaluate(traceID string, c int, slices []Slice, bytes []int64, runInvalid Exclusion) []Epoch {
	out := make([]Epoch, len(slices))
	for i, s := range slices {
		observed := s.End - s.Start
		fraction := 1.0
		if s.Nominal > 0 {
			fraction = float64(observed) / float64(s.Nominal)
		}
		e := Epoch{
			TraceID: traceID, C: c, Index: s.Index,
			TraceStart: s.Start, TraceEnd: s.End,
			Nominal: s.Nominal, Observed: observed, ObservedFraction: fraction,
			BytesReceived: bytes[i],
		}
		switch {
		case runInvalid != ExclusionNone:
			e.Exclusion = runInvalid
		case e.BytesReceived < ByteFloor:
			e.Exclusion = ExclusionByteFloor
		case fraction < MinObservedFraction:
			e.Exclusion = ExclusionObservedFraction
		default:
			e.Valid = true
		}
		if e.Observed > 0 {
			e.R = float64(e.BytesReceived) / e.Observed.Seconds()
		}
		out[i] = e
	}
	return out
}

// SharedValid reports which epoch indices are valid for every C. The resulting
// index set is identical by construction for all C.
func SharedValid(perC map[int][]Epoch) map[int]bool {
	shared := make(map[int]bool)
	if len(perC) == 0 {
		return shared
	}
	n := -1
	for _, es := range perC {
		if n < 0 || len(es) < n {
			n = len(es)
		}
	}
	for i := 0; i < n; i++ {
		ok := true
		for _, es := range perC {
			if !es[i].Valid {
				ok = false
				break
			}
		}
		if ok {
			shared[i] = true
		}
	}
	return shared
}

// ApplyShared enforces shared validity in place: an epoch valid for one C but
// not for all is marked excluded_by_other_c for every C. It returns the shared
// valid index set.
func ApplyShared(perC map[int][]Epoch) map[int]bool {
	shared := SharedValid(perC)
	for i := range shared {
		_ = i
	}
	for _, es := range perC {
		for i := range es {
			if _, ok := shared[i]; ok {
				continue
			}
			if es[i].Valid {
				es[i].Valid = false
				es[i].Exclusion = ExclusionOtherC
			}
		}
	}
	return shared
}

// CountExclusions tallies exclusion reasons across runs.
func CountExclusions(perC map[int][]Epoch) map[Exclusion]int {
	counts := make(map[Exclusion]int)
	for _, es := range perC {
		for _, e := range es {
			if !e.Valid {
				counts[e.Exclusion]++
			}
		}
	}
	return counts
}

// Candidate summarizes one nominal-duration candidate in the pilot.
type Candidate struct {
	Nominal             time.Duration `json:"nominal_duration"`
	CandidateEpochs     int           `json:"candidate_epochs"`
	ValidEpochs         int           `json:"valid_epochs"`
	SurvivalFraction    float64       `json:"survival_fraction"`
	ByteFloorExclusions int           `json:"byte_floor_exclusions"`
	TotalExclusions     int           `json:"total_exclusions"`
}

// NewCandidate summarizes shared-valid epoch counts for one duration.
func NewCandidate(nominal time.Duration, candidateEpochs, validEpochs int, counts map[Exclusion]int) Candidate {
	c := Candidate{
		Nominal:         nominal,
		CandidateEpochs: candidateEpochs,
		ValidEpochs:     validEpochs,
	}
	if candidateEpochs > 0 {
		c.SurvivalFraction = float64(validEpochs) / float64(candidateEpochs)
	}
	c.ByteFloorExclusions = counts[ExclusionByteFloor]
	for _, n := range counts {
		c.TotalExclusions += n
	}
	return c
}

// SelectDuration returns the shortest nominal duration that meets both the
// survival and minimum-valid-epoch criteria across the pilot conditions.
func SelectDuration(cands []Candidate) (time.Duration, bool) {
	sorted := append([]Candidate(nil), cands...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Nominal < sorted[j].Nominal })
	for _, c := range sorted {
		if c.SurvivalFraction >= PilotSurvivalThreshold && c.ValidEpochs >= PilotMinValidEpochs {
			return c.Nominal, true
		}
	}
	return 0, false
}

// Protocol version identifiers. Version 1 is the historical 120 s request /
// 30 s dial pilot protocol (§22.65); version 2 is the watchdog-only protocol
// (§22.67–§22.68). Historical revisions stay representable; they are never
// rewritten in place.
const (
	ProtocolVersionV1 = 1
	ProtocolVersionV2 = 2
)

// DefaultWatchdog is the pre-registered parent/child watchdog of the replay
// harness (both are the same 45-minute bound).
const DefaultWatchdog = 45 * time.Minute

// TimeoutPolicy is the pre-registered deadline policy of one protocol
// revision. The Disabled booleans make "no deadline" explicit, so it can
// never be confused with an unset or default value.
type TimeoutPolicy struct {
	RequestDisabled bool          `json:"request_disabled"`
	Request         time.Duration `json:"request,omitempty"`
	DialDisabled    bool          `json:"dial_disabled"`
	Dial            time.Duration `json:"dial,omitempty"`
	ParentWatchdog  time.Duration `json:"parent_watchdog"`
	ChildWatchdog   time.Duration `json:"child_watchdog"`
}

// WatchdogOnly reports whether the policy has no request and no dial deadline
// and keeps a positive watchdog.
func (p TimeoutPolicy) WatchdogOnly() bool {
	return p.RequestDisabled && p.Request == 0 &&
		p.DialDisabled && p.Dial == 0 &&
		p.ParentWatchdog > 0 && p.ChildWatchdog > 0
}

// Protocol is the pre-registered pilot/epoch protocol state.
type Protocol struct {
	Version             int             `json:"version"`
	Candidates          []time.Duration `json:"candidates"`
	ByteFloor           int64           `json:"byte_floor"`
	MinObservedFraction float64         `json:"min_observed_fraction"`
	Concurrency         []int           `json:"concurrency"`
	ChunkSize           int64           `json:"chunk_size"`
	Timeout             TimeoutPolicy   `json:"timeout"`
}

// DefaultProtocol is the pre-registered watchdog-only epoch pilot protocol
// (version 2): no HTTP request deadline and no connection dial deadline; the
// 45 m parent and child watchdogs are the only time bounds. All frozen
// experimental dimensions (C set, chunk size, candidate epochs, byte floor,
// observed fraction) are unchanged from version 1.
func DefaultProtocol() Protocol {
	return Protocol{
		Version:             ProtocolVersionV2,
		Candidates:          []time.Duration{30 * time.Second, 60 * time.Second, 90 * time.Second, 120 * time.Second},
		ByteFloor:           ByteFloor,
		MinObservedFraction: MinObservedFraction,
		Concurrency:         []int{1, 2, 4, 8, 16},
		ChunkSize:           1 << 20,
		Timeout: TimeoutPolicy{
			RequestDisabled: true,
			DialDisabled:    true,
			ParentWatchdog:  DefaultWatchdog,
			ChildWatchdog:   DefaultWatchdog,
		},
	}
}

// LegacyProtocolV1 returns the historical version-1 protocol (120 s request
// deadline, 30 s dial deadline, 45 m watchdogs) used by the §22.65 pilot. It
// exists so historical protocol data remains representable; it is not the
// active default.
func LegacyProtocolV1() Protocol {
	p := DefaultProtocol()
	p.Version = ProtocolVersionV1
	p.Timeout = TimeoutPolicy{
		Request:        120 * time.Second,
		Dial:           30 * time.Second,
		ParentWatchdog: DefaultWatchdog,
		ChildWatchdog:  DefaultWatchdog,
	}
	return p
}

// ValidateRevision permits only candidate-list and byte-floor changes within
// the same protocol version; the C set, chunk size, epoch definition,
// observed-duration threshold, timeout policy, and all locked semantics are
// rejected. Any permitted revision requires a fresh pilot.
func ValidateRevision(old, next Protocol) error {
	if next.Version != old.Version {
		return fmt.Errorf("epoch: ordinary revisions may not change the protocol version")
	}
	if !equalInts(old.Concurrency, next.Concurrency) {
		return fmt.Errorf("epoch: revision may not change the concurrency set")
	}
	if old.ChunkSize != next.ChunkSize {
		return fmt.Errorf("epoch: revision may not change the chunk size")
	}
	if old.MinObservedFraction != next.MinObservedFraction {
		return fmt.Errorf("epoch: revision may not change the observed-duration threshold")
	}
	if old.Timeout != next.Timeout {
		return fmt.Errorf("epoch: revision may not change the timeout policy; use ValidateTimeoutPolicyRevision")
	}
	if len(next.Candidates) == 0 {
		return fmt.Errorf("epoch: revision must keep at least one candidate duration")
	}
	if next.ByteFloor <= 0 {
		return fmt.Errorf("epoch: revision must keep a positive byte floor")
	}
	return nil
}

// ValidateTimeoutPolicyRevision checks that next is a versioned protocol
// revision that changes only the deadline policy to the watchdog-only form
// and keeps every frozen experimental dimension identical to old. A version
// bump is mandatory, so the historical protocol remains representable and is
// never rewritten in place.
func ValidateTimeoutPolicyRevision(old, next Protocol) error {
	if next.Version <= old.Version {
		return fmt.Errorf("epoch: a timeout-policy revision must increment the protocol version (%d -> %d)", old.Version, next.Version)
	}
	if !equalInts(old.Concurrency, next.Concurrency) {
		return fmt.Errorf("epoch: timeout-policy revision may not change the concurrency set")
	}
	if old.ChunkSize != next.ChunkSize {
		return fmt.Errorf("epoch: timeout-policy revision may not change the chunk size")
	}
	if old.MinObservedFraction != next.MinObservedFraction {
		return fmt.Errorf("epoch: timeout-policy revision may not change the observed-duration threshold")
	}
	if !equalDurations(old.Candidates, next.Candidates) {
		return fmt.Errorf("epoch: timeout-policy revision may not change the candidate durations")
	}
	if old.ByteFloor != next.ByteFloor {
		return fmt.Errorf("epoch: timeout-policy revision may not change the byte floor")
	}
	if !next.Timeout.WatchdogOnly() {
		return fmt.Errorf("epoch: timeout-policy revision must be watchdog-only (no request or dial deadline, positive watchdogs)")
	}
	if next.Timeout.ParentWatchdog != old.Timeout.ParentWatchdog || next.Timeout.ChildWatchdog != old.Timeout.ChildWatchdog {
		return fmt.Errorf("epoch: timeout-policy revision may not change the watchdog bounds")
	}
	return nil
}

func equalDurations(a, b []time.Duration) bool {
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

func equalInts(a, b []int) bool {
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

// NativeTrace describes one installed Mahimahi trace used by the pilot.
type NativeTrace struct {
	ID                 string        `json:"id"`
	Path               string        `json:"path"`
	Duration           time.Duration `json:"trace_duration"`
	Lines              int           `json:"lines"`
	MeanBytesPerSecond float64       `json:"mean_bytes_per_second"`
	Classification     string        `json:"classification,omitempty"`
}

// ScanTraceCorpus lists *.down traces in dir and computes their mean
// delivery-opportunity rate (lines * 1500 bytes / duration). Originals are
// never modified.
func ScanTraceCorpus(dir string) ([]NativeTrace, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.down"))
	if err != nil {
		return nil, err
	}
	var out []NativeTrace
	for _, p := range paths {
		info, err := replay.ReadTrace(p)
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(p), ".down")
		out = append(out, NativeTrace{
			ID:                 base,
			Path:               p,
			Duration:           info.Duration,
			Lines:              info.Lines,
			MeanBytesPerSecond: float64(info.Lines) * 1500 / info.Duration.Seconds(),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("epoch: no readable .down traces in %s", dir)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SelectPilotTraces classifies the highest- and lowest-mean-rate traces among
// those long enough to evaluate every candidate with at least
// PilotMinValidEpochs epochs (minDuration), as good/degraded. The rule is
// frozen and reproducible; it uses no performance/headroom data.
func SelectPilotTraces(all []NativeTrace, minDuration time.Duration) (good, degraded NativeTrace, ok bool) {
	var elig []NativeTrace
	for _, t := range all {
		if t.Duration >= minDuration {
			elig = append(elig, t)
		}
	}
	if len(elig) < 2 {
		return NativeTrace{}, NativeTrace{}, false
	}
	sort.Slice(elig, func(i, j int) bool { return elig[i].MeanBytesPerSecond < elig[j].MeanBytesPerSecond })
	good = elig[len(elig)-1]
	good.Classification = "good"
	degraded = elig[0]
	degraded.Classification = "degraded"
	return good, degraded, true
}

// Ensure the corpus directory exists (documentation helper).
func CorpusExists(dir string) bool {
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}
