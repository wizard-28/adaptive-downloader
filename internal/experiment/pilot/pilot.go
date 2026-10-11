// Package pilot orchestrates the pre-registered epoch-duration calibration
// pilot: 2 traces x C={1,2,4,8,16}, one full-trace replay each, all four
// candidate durations evaluated post-hoc from the same replay. It implements
// no tau/oracle/headroom logic and never alters the registered protocol.
package pilot

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"adaptivedl/internal/experiment/epoch"
	"adaptivedl/internal/experiment/replay"
)

// Defaults of the registered pilot.
var (
	DefaultCSet       = []int{1, 2, 4, 8, 16}
	DefaultCandidates = []time.Duration{30 * time.Second, 60 * time.Second, 90 * time.Second, 120 * time.Second}
)

// Trace-pair resolution.
const (
	traceUpSuffix   = ".up"
	traceDownSuffix = ".down"
)

// ErrTracePair is returned when a trace family cannot be resolved to a
// matching .up/.down pair.
var ErrTracePair = errors.New("pilot: trace pair")

// TracePair is one Mahimahi trace family with its uplink (.up) and downlink
// (.down) members resolved. The pilot replays .up as the uplink trace and
// .down as the downlink trace; the same file is never passed for both
// directions.
type TracePair struct {
	ID       string `json:"id"`
	UpPath   string `json:"up_path"`
	DownPath string `json:"down_path"`
}

// TracePairFromPaths validates an explicit uplink/downlink pair: both members
// must be non-empty, live in the same directory, and belong to the same trace
// family (same basename stem). Missing or mismatched pairs are rejected.
func TracePairFromPaths(up, down string) (TracePair, error) {
	if up == "" || down == "" {
		return TracePair{}, fmt.Errorf("%w: both .up and .down members are required", ErrTracePair)
	}
	if filepath.Dir(up) != filepath.Dir(down) {
		return TracePair{}, fmt.Errorf("%w: %s and %s are in different directories", ErrTracePair, up, down)
	}
	id := traceFamily(up)
	if id != traceFamily(down) {
		return TracePair{}, fmt.Errorf("%w: %s and %s are different trace families", ErrTracePair, up, down)
	}
	if id == "" {
		return TracePair{}, fmt.Errorf("%w: %s has no family name", ErrTracePair, up)
	}
	return TracePair{ID: id, UpPath: up, DownPath: down}, nil
}

// ResolveTracePair resolves a trace family (a bare stem or a path with a
// .up/.down suffix) to its .up uplink and .down downlink members. Both
// members must exist as regular files. Trace identity is the family stem with
// the extension removed.
func ResolveTracePair(family string) (TracePair, error) {
	f := strings.TrimSpace(family)
	if f == "" {
		return TracePair{}, fmt.Errorf("%w: empty trace family", ErrTracePair)
	}
	stem := strings.TrimSuffix(strings.TrimSuffix(f, traceDownSuffix), traceUpSuffix)
	if stem == "" || strings.HasSuffix(stem, "/") || strings.HasSuffix(stem, string(filepath.Separator)) {
		return TracePair{}, fmt.Errorf("%w: %q has no family name", ErrTracePair, family)
	}
	pair, err := TracePairFromPaths(stem+traceUpSuffix, stem+traceDownSuffix)
	if err != nil {
		return TracePair{}, err
	}
	for _, p := range []string{pair.UpPath, pair.DownPath} {
		st, err := os.Stat(p)
		if err != nil {
			return TracePair{}, fmt.Errorf("%w: %s: %w", ErrTracePair, p, err)
		}
		if !st.Mode().IsRegular() {
			return TracePair{}, fmt.Errorf("%w: %s is not a regular file", ErrTracePair, p)
		}
	}
	return pair, nil
}

func traceFamily(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(strings.TrimSuffix(base, traceUpSuffix), traceDownSuffix)
}

// RunSpec is one planned full-trace replay.
type RunSpec struct {
	TraceID  string
	UpPath   string
	DownPath string
	C        int
}

// Plan returns the exact 10-run matrix (2 trace pairs x 5 C). Each run
// resolves its own uplink and downlink members from the pair.
func Plan(good, degraded TracePair, cs []int) []RunSpec {
	var out []RunSpec
	for _, p := range []TracePair{good, degraded} {
		for _, c := range cs {
			out = append(out, RunSpec{TraceID: p.ID, UpPath: p.UpPath, DownPath: p.DownPath, C: c})
		}
	}
	return out
}

// RunDir is the unique, non-overwriting directory for one trace/C replay.
func RunDir(base, traceID string, c int) string {
	return filepath.Join(base, fmt.Sprintf("%s_C%d", traceID, c))
}

// ResultPath is the machine-readable result of a completed replay.
func ResultPath(dir string) string { return filepath.Join(dir, "result.json") }

// FailurePath is the machine-readable record of a failed replay attempt.
func FailurePath(dir string, attempt int) string {
	return filepath.Join(dir, fmt.Sprintf("result.failed.%d.json", attempt))
}

// AnchorMeta is the compact, serializable anchor needed to map raw event wall
// timestamps into calibrated trace time. It is derived from replay.Anchor and
// never redefines t=0.
type AnchorMeta struct {
	LaunchParentWallUnixNano int64 `json:"launch_parent_wall_unix_nano"`
	ExitParentWallUnixNano   int64 `json:"exit_parent_wall_unix_nano"`
	ChildStartWallUnixNano   int64 `json:"child_start_wall_unix_nano"`
	StartupOffsetNanos       int64 `json:"startup_offset_nanos"`
	TraceDurationNanos       int64 `json:"trace_duration_nanos"`
	AlignmentToleranceNanos  int64 `json:"alignment_tolerance_nanos"`
	DownlinkLogLines         int64 `json:"downlink_log_lines,omitempty"`
	DownlinkLogLastMs        int64 `json:"downlink_log_last_ms,omitempty"`
}

// AnchorFromResult converts the harness anchor into its persisted form.
func AnchorFromResult(a *replay.Anchor) *AnchorMeta {
	if a == nil || a.LaunchParentMono.IsZero() {
		return nil
	}
	return &AnchorMeta{
		LaunchParentWallUnixNano: a.LaunchParentMono.UnixNano(),
		ExitParentWallUnixNano:   a.ExitParentMono.UnixNano(),
		ChildStartWallUnixNano:   a.ChildStartWallUnixNano,
		StartupOffsetNanos:       int64(a.StartupOffset),
		TraceDurationNanos:       int64(a.TraceDuration),
		AlignmentToleranceNanos:  int64(a.AlignmentTolerance),
		DownlinkLogLines:         a.DownlinkLogLines,
		DownlinkLogLastMs:        a.DownlinkLogLastMs,
	}
}

// Valid reports whether the anchor can be consumed.
func (m *AnchorMeta) Valid() bool {
	return m != nil && m.LaunchParentWallUnixNano != 0 && m.TraceDurationNanos > 0
}

// TraceTimeNanos maps a raw event wall timestamp into trace time using the
// frozen anchor: (event_wall - launch_wall) - StartupOffset. Wall is used only
// as a cross-process epoch; the trace coordinate itself is the calibrated one.
func (m *AnchorMeta) TraceTimeNanos(eventWallUnixNano int64) int64 {
	return eventWallUnixNano - m.LaunchParentWallUnixNano - m.StartupOffsetNanos
}

// CurrentRep layerErr is returned for a replay whose anchor is missing/invalid.
var ErrMissingAnchor = fmt.Errorf("pilot: replay anchor missing or invalid")

// eventLine is the minimal JSONL event record streamed from disk.
type eventLine struct {
	WallUnixNano int64 `json:"wall_unix_nano"`
	Delta        int64 `json:"delta"`
}

// StreamEvents stream-decodes the child event timeline (JSONL), maps each event
// into trace time through the anchor, and accumulates bytes per epoch for every
// candidate duration in a single pass with bounded memory (one int64 slice per
// candidate). It never retains events. Events outside [0, traceDuration) are
// counted and skipped, never silently dropped.
func StreamEvents(eventsPath string, anchor *AnchorMeta, traceDuration time.Duration, candidates []time.Duration) (map[int64][]int64, int64, int64, error) {
	if !anchor.Valid() {
		return nil, 0, 0, ErrMissingAnchor
	}
	f, err := os.Open(eventsPath)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()

	acc := make(map[int64][]int64, len(candidates))
	for _, c := range candidates {
		if c <= 0 {
			return nil, 0, 0, fmt.Errorf("pilot: non-positive candidate duration %v", c)
		}
		n := int(ceilDiv(traceDuration, c))
		acc[int64(c)] = make([]int64, n)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4<<20)
	var total, outOfRange int64
	var ev eventLine
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, total, outOfRange, fmt.Errorf("pilot: decode event: %w", err)
		}
		if ev.Delta <= 0 {
			continue
		}
		t := time.Duration(anchor.TraceTimeNanos(ev.WallUnixNano))
		if t < 0 || t >= traceDuration {
			outOfRange++
			continue
		}
		total += ev.Delta
		for _, c := range candidates {
			idx := int(t / c)
			slice := acc[int64(c)]
			if idx >= 0 && idx < len(slice) {
				slice[idx] += ev.Delta
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, total, outOfRange, err
	}
	return acc, total, outOfRange, nil
}

func ceilDiv(a, b time.Duration) int64 {
	if b <= 0 {
		return 0
	}
	return int64((a + b - 1) / b)
}

// DurationResult holds the epoch records for one candidate duration.
type DurationResult struct {
	NominalNanos int64         `json:"nominal_nanos"`
	Epochs       []epoch.Epoch `json:"epochs"`
}

// RunEvidence is the persisted harness evidence for one replay attempt. It
// records why the harness did or did not classify the run as reaching trace
// EOF, so a misclassification cannot be hidden behind a bare status string.
// It is saved for both successful and failed runs.
type RunEvidence struct {
	Outcome                    string `json:"outcome"`
	TraceExpectedDurationNanos int64  `json:"trace_expected_duration_nanos"`
	TraceObservedEndpointNanos int64  `json:"trace_observed_endpoint_nanos"`
	TraceEndpointResidualNanos int64  `json:"trace_endpoint_residual_nanos"`
	EndpointValid              bool   `json:"endpoint_valid"`
	MMLinkExit                 int    `json:"mm_link_exit"`
	ChildStatus                string `json:"child_status"`
	ChildError                 string `json:"child_error,omitempty"`
}

// EvidenceFromResult converts a replay result into its persisted evidence.
func EvidenceFromResult(res replay.Result) RunEvidence {
	return RunEvidence{
		Outcome:                    string(res.Outcome),
		TraceExpectedDurationNanos: int64(res.TraceExpectedDuration),
		TraceObservedEndpointNanos: int64(res.TraceObservedEndpoint),
		TraceEndpointResidualNanos: int64(res.TraceEndpointResidual),
		EndpointValid:              res.EndpointValid,
		MMLinkExit:                 res.MMLinkExit,
		ChildStatus:                string(res.ChildStatus),
		ChildError:                 res.ChildError,
	}
}

// RunResult is the persisted result of one trace/C replay, covering all
// candidate durations evaluated post-hoc from the same events.
type RunResult struct {
	TraceID     string                    `json:"trace_id"`
	UpPath      string                    `json:"up_trace_path"`
	DownPath    string                    `json:"down_trace_path"`
	C           int                       `json:"c"`
	Status      string                    `json:"status"`
	StartedAt   time.Time                 `json:"started_at"`
	CompletedAt time.Time                 `json:"completed_at"`
	TraceNanos  int64                     `json:"trace_duration_nanos"`
	Anchor      *AnchorMeta               `json:"anchor"`
	Evidence    RunEvidence               `json:"evidence"`
	TotalBytes  int64                     `json:"total_bytes_received"`
	OutOfRange  int64                     `json:"events_out_of_range"`
	Durations   map[string]DurationResult `json:"durations"`
	Err         string                    `json:"err,omitempty"`
}

// BuildRunResult evaluates all candidate durations from one streamed event
// pass. runInvalid (anchor/contamination) marks every epoch invalid.
func BuildRunResult(spec RunSpec, traceDuration time.Duration, anchor *AnchorMeta, eventsPath string, candidates []time.Duration, runInvalid epoch.Exclusion) (*RunResult, error) {
	if runInvalid == epoch.ExclusionNone && !anchor.Valid() {
		runInvalid = epoch.ExclusionAnchorAlignment
	}
	res := &RunResult{
		TraceID:    spec.TraceID,
		UpPath:     spec.UpPath,
		DownPath:   spec.DownPath,
		C:          spec.C,
		Status:     "ok",
		TraceNanos: int64(traceDuration),
		Anchor:     anchor,
		Durations:  make(map[string]DurationResult, len(candidates)),
	}
	bytesByDur, total, outOfRange, err := streamOrZero(eventsPath, anchor, traceDuration, candidates)
	if err != nil {
		return nil, err
	}
	res.TotalBytes = total
	res.OutOfRange = outOfRange
	for _, c := range candidates {
		slices := epoch.Slices(traceDuration, c)
		bytes := bytesByDur[int64(c)]
		if bytes == nil {
			bytes = make([]int64, len(slices))
		}
		res.Durations[c.String()] = DurationResult{
			NominalNanos: int64(c),
			Epochs:       epoch.Evaluate(spec.TraceID, spec.C, slices, bytes, runInvalid),
		}
	}
	return res, nil
}

func streamOrZero(eventsPath string, anchor *AnchorMeta, traceDuration time.Duration, candidates []time.Duration) (map[int64][]int64, int64, int64, error) {
	if !anchor.Valid() {
		// No fabricated alignment: return zero byte attribution; Evaluate will
		// mark every epoch invalid via the anchor exclusion.
		acc := make(map[int64][]int64, len(candidates))
		for _, c := range candidates {
			acc[int64(c)] = make([]int64, int(ceilDiv(traceDuration, c)))
		}
		return acc, 0, 0, nil
	}
	return StreamEvents(eventsPath, anchor, traceDuration, candidates)
}

// LoadResult reads a persisted run result; a missing file returns (nil, nil).
func LoadResult(dir string) (*RunResult, error) {
	data, err := os.ReadFile(ResultPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r RunResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// WriteJSON writes indented JSON without overwriting unrelated files.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// TraceSummary aggregates the shared-valid epoch statistics for one trace.
type TraceSummary struct {
	TraceID       string                     `json:"trace_id"`
	Candidates    map[string]epoch.Candidate `json:"candidates"`
	PerCByteFloor map[string]map[int]int     `json:"per_c_byte_floor_exclusions"`
}

// CombineTrace applies shared validity across the five C results for every
// candidate duration and summarizes exclusions.
func CombineTrace(traceID string, perC map[int]*RunResult, candidates []time.Duration) (TraceSummary, error) {
	ts := TraceSummary{
		TraceID:       traceID,
		Candidates:    make(map[string]epoch.Candidate, len(candidates)),
		PerCByteFloor: make(map[string]map[int]int, len(candidates)),
	}
	for _, c := range candidates {
		key := c.String()
		perCEpochs := make(map[int][]epoch.Epoch, len(perC))
		perCByteFloor := make(map[int]int, len(perC))
		for concurrency, r := range perC {
			if r == nil {
				return ts, fmt.Errorf("pilot: trace %s C=%d missing", traceID, concurrency)
			}
			d, ok := r.Durations[key]
			if !ok {
				return ts, fmt.Errorf("pilot: trace %s C=%d duration %s missing", traceID, concurrency, key)
			}
			perCEpochs[concurrency] = d.Epochs
			n := 0
			for _, e := range d.Epochs {
				if e.Exclusion == epoch.ExclusionByteFloor {
					n++
				}
			}
			perCByteFloor[concurrency] = n
		}
		counts := epoch.CountExclusions(perCEpochs)
		shared := epoch.ApplyShared(perCEpochs)
		candidateEpochs := 0
		for _, es := range perCEpochs {
			candidateEpochs = len(es)
			break
		}
		ts.Candidates[key] = epoch.NewCandidate(c, candidateEpochs, len(shared), counts)
		ts.PerCByteFloor[key] = perCByteFloor
	}
	return ts, nil
}

// PilotReport is the final pilot aggregation.
type PilotReport struct {
	Traces      []TraceSummary `json:"traces"`
	Selected    string         `json:"selected_duration,omitempty"`
	Passed      bool           `json:"passed"`
	Incomplete  []string       `json:"incomplete_runs,omitempty"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// SummarizePilot aggregates across traces. The survival fraction is computed
// from the shared trace-time epoch set over both pilot conditions; the minimum
// per-trace valid count gates the >=3 valid-epochs requirement.
func SummarizePilot(summaries []TraceSummary, candidates []time.Duration, incomplete []string) PilotReport {
	rep := PilotReport{Traces: summaries, Incomplete: incomplete, GeneratedAt: time.Now()}
	if len(incomplete) > 0 {
		return rep
	}
	agg := make([]epoch.Candidate, 0, len(candidates))
	for _, c := range candidates {
		key := c.String()
		totalCandidate, totalValid, minValid := 0, 0, -1
		for _, ts := range summaries {
			cand, ok := ts.Candidates[key]
			if !ok {
				return rep
			}
			totalCandidate += cand.CandidateEpochs
			totalValid += cand.ValidEpochs
			if minValid < 0 || cand.ValidEpochs < minValid {
				minValid = cand.ValidEpochs
			}
		}
		ac := epoch.Candidate{Nominal: c, CandidateEpochs: totalCandidate, ValidEpochs: minValid}
		if totalCandidate > 0 {
			ac.SurvivalFraction = float64(totalValid) / float64(totalCandidate)
		}
		agg = append(agg, ac)
	}
	if d, ok := epoch.SelectDuration(agg); ok {
		rep.Selected = d.String()
		rep.Passed = true
	}
	return rep
}

// SortedSpecs returns the plan in deterministic execution order.
func SortedSpecs(specs []RunSpec) []RunSpec {
	out := append([]RunSpec(nil), specs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].TraceID != out[j].TraceID {
			return out[i].TraceID < out[j].TraceID
		}
		return out[i].C < out[j].C
	})
	return out
}
