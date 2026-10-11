package experiment

import (
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"adaptivedl/internal/experiment/network"
)

// MatrixScenario is one named scenario in a matrix.
type MatrixScenario struct {
	Name   string
	Config ScenarioConfig
}

// MatrixConfig describes a matrix run.
type MatrixConfig struct {
	Scenarios []MatrixScenario
	// Repetitions overrides each scenario's Repetitions. Zero means 5.
	Repetitions int
	// NetworkController is used for tc/netem. Nil means the system tc command.
	NetworkController *network.Controller
	// Modes selects which modes to run. Nil means the four baseline modes
	// (AllModes). Pass AllModesWithLinUCB() to include the opt-in LinUCB mode.
	Modes []Mode
}

// ModeAggregate summarizes the valid runs of one scenario x mode cell. It is
// used by both the single-scenario summary and the matrix summary. All means,
// medians, and variability measures are computed over valid runs only; failed
// and network-unavailable runs are counted separately and excluded.
type ModeAggregate struct {
	Runs                   int
	ValidRuns              int
	FailedRuns             int
	NetworkUnavailableRuns int

	MeanDuration   time.Duration
	MedianDuration time.Duration
	MinDuration    time.Duration
	MaxDuration    time.Duration
	StdDevDuration time.Duration
	IQRDuration    time.Duration

	MeanThroughput   float64
	MedianThroughput float64
	StdDevThroughput float64
	IQRThroughput    float64

	MeanAttempts           float64
	MeanRetries            float64
	MeanFinalConcurrency   float64
	MeanMaxConcurrency     float64
	MeanFinalChunkSize     float64
	MeanMinChunkSize       float64
	MeanMaxChunkSize       float64
	MeanConcurrencyActions float64
	MeanChunkSizeActions   float64
}

// ScenarioSummary aggregates all modes of a scenario. LinUCB is populated only
// when the opt-in mode is selected.
type ScenarioSummary struct {
	Fixed               ModeAggregate
	ConcurrencyAdaptive ModeAggregate
	ChunkSizeAdaptive   ModeAggregate
	JointAdaptive       ModeAggregate
	LinUCB              ModeAggregate
	LinUCBPeriodic      ModeAggregate
}

// ScenarioResult keeps every individual run for a scenario plus its summary.
// LinUCB modes are populated only when selected.
type ScenarioResult struct {
	Name                string
	Config              ScenarioConfig
	Fixed               []RunResult
	ConcurrencyAdaptive []RunResult
	ChunkSizeAdaptive   []RunResult
	JointAdaptive       []RunResult
	LinUCB              []RunResult
	LinUCBPeriodic      []RunResult
	Summary             ScenarioSummary
}

// Runs returns the runs for a mode.
func (s ScenarioResult) Runs(mode Mode) []RunResult {
	switch mode {
	case ModeFixed:
		return s.Fixed
	case ModeConcurrencyAdaptive:
		return s.ConcurrencyAdaptive
	case ModeChunkSizeAdaptive:
		return s.ChunkSizeAdaptive
	case ModeJointAdaptive:
		return s.JointAdaptive
	case ModeLinUCB:
		return s.LinUCB
	case ModeLinUCBPeriodic:
		return s.LinUCBPeriodic
	default:
		return nil
	}
}

func (s *ScenarioResult) add(mode Mode, rr RunResult) {
	switch mode {
	case ModeFixed:
		s.Fixed = append(s.Fixed, rr)
	case ModeConcurrencyAdaptive:
		s.ConcurrencyAdaptive = append(s.ConcurrencyAdaptive, rr)
	case ModeChunkSizeAdaptive:
		s.ChunkSizeAdaptive = append(s.ChunkSizeAdaptive, rr)
	case ModeJointAdaptive:
		s.JointAdaptive = append(s.JointAdaptive, rr)
	case ModeLinUCB:
		s.LinUCB = append(s.LinUCB, rr)
	case ModeLinUCBPeriodic:
		s.LinUCBPeriodic = append(s.LinUCBPeriodic, rr)
	}
}

// MatrixResult is the full matrix output. Raw runs are preserved; the summary
// is supplementary.
type MatrixResult struct {
	Scenarios []ScenarioResult
}

// DefaultMatrixConfig returns the baseline characterization matrix. Values are
// modest local-test values, not a model of any particular real ISP.
func DefaultMatrixConfig() MatrixConfig {
	base := func() ScenarioConfig {
		cfg := DefaultScenarioConfig()
		cfg.FileSize = 256 << 10
		cfg.ChunkSize = 32 << 10
		cfg.InitialConcurrency = 1
		cfg.MinConcurrency = 1
		cfg.MaxConcurrency = 4
		cfg.InitialRate = 4 << 20
		cfg.ChangedRate = 4 << 20
		cfg.RateChangeAfterRequests = 0
		cfg.MinChunkSize = 32 << 10
		cfg.MaxChunkSize = 128 << 10
		cfg.InitialChunkSize = 32 << 10
		cfg.ChunkSizeStep = 32 << 10
		cfg.ImprovementThreshold = 0.2
		cfg.DegradationThreshold = 0.2
		cfg.PolicyWindowSize = 4
		cfg.MaxRetries = 3
		cfg.Timeout = 30 * time.Second
		cfg.Repetitions = 5
		return cfg
	}

	baseline := base()

	serverThrottle := base()
	serverThrottle.InitialRate = 512 << 10
	serverThrottle.ChangedRate = 4 << 20
	serverThrottle.RateChangeAfterRequests = 4

	latency := base()
	latency.NetworkCondition = &network.Config{Latency: 20 * time.Millisecond}

	loss := base()
	loss.NetworkCondition = &network.Config{LossPercent: 1}

	combined := base()
	combined.InitialRate = 1 << 20 // constrained bandwidth via server throttle
	combined.NetworkCondition = &network.Config{Latency: 20 * time.Millisecond, LossPercent: 1}

	return MatrixConfig{
		Repetitions: 5,
		Scenarios: []MatrixScenario{
			{Name: "baseline", Config: baseline},
			{Name: "server-throttle", Config: serverThrottle},
			{Name: "latency", Config: latency},
			{Name: "loss", Config: loss},
			{Name: "combined", Config: combined},
		},
	}
}

// RunMatrix runs every scenario in all four modes, Repetitions times each, using
// fresh server/downloader/output/metrics/policy state per run. Network shaping
// is applied and cleared per run, so a failure cannot leak into later runs.
func RunMatrix(mc MatrixConfig) (MatrixResult, error) {
	if mc.Repetitions <= 0 {
		mc.Repetitions = 5
	}
	netCtl := mc.NetworkController
	if netCtl == nil {
		netCtl = network.NewController()
	}
	modes := mc.Modes
	if len(modes) == 0 {
		modes = AllModes()
	}
	if err := validateModes(modes); err != nil {
		return MatrixResult{}, err
	}

	var result MatrixResult
	for _, sc := range mc.Scenarios {
		cfg := sc.Config
		cfg.Repetitions = mc.Repetitions
		if err := cfg.Validate(); err != nil {
			return result, fmt.Errorf("scenario %q: %w", sc.Name, err)
		}
		content := contentPattern(cfg.FileSize)

		sr := ScenarioResult{Name: sc.Name, Config: cfg}
		for i := 0; i < mc.Repetitions; i++ {
			for _, mode := range modes {
				rr, err := runOnce(cfg, mode, content, netCtl)
				if err != nil {
					if rr.Mode == "" {
						rr.Mode = mode
					}
					rr.Err = err.Error()
				}
				rr.Scenario = sc.Name
				rr.Repetition = i
				sr.add(mode, rr)
			}
		}
		sr.Summary = summarizeScenario(sr)
		result.Scenarios = append(result.Scenarios, sr)
	}
	return result, nil
}

func summarizeScenario(sr ScenarioResult) ScenarioSummary {
	return ScenarioSummary{
		Fixed:               aggregateRuns(sr.Fixed),
		ConcurrencyAdaptive: aggregateRuns(sr.ConcurrencyAdaptive),
		ChunkSizeAdaptive:   aggregateRuns(sr.ChunkSizeAdaptive),
		JointAdaptive:       aggregateRuns(sr.JointAdaptive),
		LinUCB:              aggregateRuns(sr.LinUCB),
		LinUCBPeriodic:      aggregateRuns(sr.LinUCBPeriodic),
	}
}

func aggregateRuns(runs []RunResult) ModeAggregate {
	agg := ModeAggregate{Runs: len(runs)}
	valid := make([]RunResult, 0, len(runs))
	for _, r := range runs {
		switch {
		case r.NetworkConditionStatus == NetworkStatusUnavailable:
			agg.NetworkUnavailableRuns++
			agg.FailedRuns++
		case runSuccessful(r):
			agg.ValidRuns++
			valid = append(valid, r)
		default:
			agg.FailedRuns++
		}
	}
	if len(valid) == 0 {
		return agg
	}

	durations := make([]time.Duration, 0, len(valid))
	throughputs := make([]float64, 0, len(valid))
	var (
		totalDuration time.Duration
		attempts      float64
		retries       float64
		finalConc     float64
		maxConc       float64
		finalChunk    float64
		minChunk      float64
		maxChunk      float64
		concActions   float64
		chunkActions  float64
	)
	for _, r := range valid {
		durations = append(durations, r.Duration)
		throughputs = append(throughputs, r.AverageThroughput)
		totalDuration += r.Duration
		attempts += float64(r.TotalAttempts)
		retries += float64(r.RetryAttempts)
		finalConc += float64(r.FinalConcurrency)
		maxConc += float64(r.MaxConcurrency)
		finalChunk += float64(r.FinalChunkSize)
		minChunk += float64(r.MinChunkSize)
		maxChunk += float64(r.MaxChunkSize)
		concActions += float64(r.ConcurrencyActions)
		chunkActions += float64(r.ChunkSizeActions)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	sort.Float64s(throughputs)

	n := float64(len(valid))
	agg.MeanDuration = totalDuration / time.Duration(len(valid))
	agg.MedianDuration = medianDuration(durations)
	agg.MinDuration = durations[0]
	agg.MaxDuration = durations[len(durations)-1]
	agg.StdDevDuration = stddevDuration(durations)
	agg.IQRDuration = iqrDuration(durations)

	agg.MeanThroughput = meanFloat(throughputs)
	agg.MedianThroughput = medianFloat(throughputs)
	agg.StdDevThroughput = stddevFloat(throughputs)
	agg.IQRThroughput = iqrFloat(throughputs)

	agg.MeanAttempts = attempts / n
	agg.MeanRetries = retries / n
	agg.MeanFinalConcurrency = finalConc / n
	agg.MeanMaxConcurrency = maxConc / n
	agg.MeanFinalChunkSize = finalChunk / n
	agg.MeanMinChunkSize = minChunk / n
	agg.MeanMaxChunkSize = maxChunk / n
	agg.MeanConcurrencyActions = concActions / n
	agg.MeanChunkSizeActions = chunkActions / n
	return agg
}

// runSuccessful reports whether a run completed and reconstructed the file
// with no duplicate or missing ranges.
func runSuccessful(r RunResult) bool {
	return r.Err == "" && r.OutputMatches && r.NoDuplicateRanges && r.NoMissingRanges
}

func medianDuration(sorted []time.Duration) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func meanFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// medianFloat expects a sorted slice.
func medianFloat(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func stddevFloat(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	mean := meanFloat(vals)
	var sq float64
	for _, v := range vals {
		d := v - mean
		sq += d * d
	}
	return math.Sqrt(sq / float64(len(vals)))
}

// percentile expects a sorted slice and uses linear interpolation.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	rank := p * float64(n-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func iqrFloat(sorted []float64) float64 {
	return percentile(sorted, 0.75) - percentile(sorted, 0.25)
}

func stddevDuration(vals []time.Duration) time.Duration {
	if len(vals) < 2 {
		return 0
	}
	f := make([]float64, len(vals))
	for i, v := range vals {
		f[i] = float64(v)
	}
	return time.Duration(stddevFloat(f))
}

func iqrDuration(sorted []time.Duration) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	f := make([]float64, len(sorted))
	for i, v := range sorted {
		f[i] = float64(v)
	}
	return time.Duration(iqrFloat(f))
}

// modesPresent returns the modes that produced runs in the matrix, in the
// canonical baseline-then-LinUCB order. The four baseline modes are included
// only if the scenario produced runs for them, so a LinUCB-only matrix is
// printed correctly without changing baseline output.
func (r MatrixResult) modesPresent() []Mode {
	present := make(map[Mode]bool)
	for _, s := range r.Scenarios {
		for _, mode := range AllModesWithBothLinUCB() {
			if len(s.Runs(mode)) > 0 {
				present[mode] = true
			}
		}
	}
	var modes []Mode
	for _, mode := range AllModesWithBothLinUCB() {
		if present[mode] {
			modes = append(modes, mode)
		}
	}
	if len(modes) == 0 {
		return AllModes()
	}
	return modes
}

// WriteText writes the raw runs and per-scenario aggregates as a plain table.
func (r MatrixResult) WriteText(w io.Writer) {
	modes := r.modesPresent()
	fmt.Fprintf(w, "%-16s %-19s %-4s %-12s %-9s %-12s %-8s %-8s %-8s %-7s\n",
		"Scenario", "Mode", "Run", "Duration", "Attempts", "Throughput", "FinalC", "FinalChunk", "MaxChunk", "Correct")
	for _, s := range r.Scenarios {
		for _, mode := range modes {
			writeRunRows(w, s.Name, s.Runs(mode))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Aggregates (scenario x mode, valid runs only):")
	fmt.Fprintf(w, "%-16s %-19s %-6s %-6s %-12s %-12s %-11s %-9s %-9s\n",
		"Scenario", "Mode", "Valid", "Failed", "MeanDur", "MedianDur", "MeanThrpt", "MeanAtt", "MeanRetr")
	for _, s := range r.Scenarios {
		for _, mode := range modes {
			writeAggRow(w, s.Name, string(mode), s.Summary.aggregate(mode))
		}
	}
}

func (s ScenarioSummary) aggregate(mode Mode) ModeAggregate {
	switch mode {
	case ModeFixed:
		return s.Fixed
	case ModeConcurrencyAdaptive:
		return s.ConcurrencyAdaptive
	case ModeChunkSizeAdaptive:
		return s.ChunkSizeAdaptive
	case ModeJointAdaptive:
		return s.JointAdaptive
	case ModeLinUCB:
		return s.LinUCB
	case ModeLinUCBPeriodic:
		return s.LinUCBPeriodic
	default:
		return ModeAggregate{}
	}
}

func writeRunRows(w io.Writer, scenario string, runs []RunResult) {
	for i, r := range runs {
		fmt.Fprintf(w, "%-16s %-19s %-4d %-12s %-9d %-12.0f %-8d %-8d %-8d %-7v\n",
			scenario, r.Mode, i, r.Duration, r.TotalAttempts, r.AverageThroughput,
			r.FinalConcurrency, r.FinalChunkSize, r.MaxChunkSize, runSuccessful(r))
	}
}

func writeAggRow(w io.Writer, scenario, mode string, a ModeAggregate) {
	fmt.Fprintf(w, "%-16s %-19s %-6d %-6d %-12s %-12s %-11.0f %-9.2f %-9.2f\n",
		scenario, mode, a.ValidRuns, a.FailedRuns, a.MeanDuration, a.MedianDuration,
		a.MeanThroughput, a.MeanAttempts, a.MeanRetries)
}
