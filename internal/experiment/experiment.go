// Package experiment runs controlled, deterministic download experiments that
// compare four control modes under identical scenarios:
//
//   - fixed
//   - concurrency-adaptive (ThroughputPolicy)
//   - chunk-size-adaptive (ChunkSizePolicy)
//   - joint-adaptive (JointPolicy)
//
// It is measurement infrastructure only: it does not rank modes, compute a
// winner, or use rewards/learning, and it contains no RL, bandits, packet loss
// simulation, or network shaping beyond the existing tc/netem helper.
package experiment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

// ErrInvalidScenario indicates an unusable ScenarioConfig.
var ErrInvalidScenario = errors.New("experiment: invalid scenario configuration")

// Mode identifies the experiment variant.
type Mode string

const (
	// ModeFixed keeps both concurrency and chunk size constant (no policy).
	ModeFixed Mode = "fixed"
	// ModeConcurrencyAdaptive lets a ThroughputPolicy change concurrency while
	// the chunk size stays fixed.
	ModeConcurrencyAdaptive Mode = "concurrency-adaptive"
	// ModeChunkSizeAdaptive lets a ChunkSizePolicy change chunk size while
	// concurrency stays fixed.
	ModeChunkSizeAdaptive Mode = "chunk-size-adaptive"
	// ModeJointAdaptive lets a JointPolicy change either or both dimensions.
	ModeJointAdaptive Mode = "joint-adaptive"
	// ModeLinUCB uses the LinUCB contextual bandit (behind the RL Adapter) to
	// choose concurrency and chunk-size actions from reward feedback. It is an
	// OPT-IN fifth mode and is NOT part of the default four-mode set.
	ModeLinUCB Mode = "linucb"
	// ModeLinUCBPeriodic is the LinUCB mode with the deterministic periodic
	// exploration correction enabled (ExplorationInterval=LinUCBPeriodicInterval).
	// It is an OPT-IN mode used for the standard-vs-periodic comparison.
	ModeLinUCBPeriodic Mode = "linucb-periodic"
)

// LinUCBPeriodicInterval is the fixed periodic exploration interval used by
// ModeLinUCBPeriodic (one forced exploratory action per N normal decisions).
const LinUCBPeriodicInterval = 5

// ModeAdaptive is a deprecated alias for ModeConcurrencyAdaptive.
//
// Deprecated: use ModeConcurrencyAdaptive.
const ModeAdaptive = ModeConcurrencyAdaptive

// AllModes returns the four baseline modes in their canonical order. LinUCB is
// deliberately excluded so the default comparison and the saved baseline
// dataset remain unchanged; use AllModesWithLinUCB to opt in.
func AllModes() []Mode {
	return []Mode{ModeFixed, ModeConcurrencyAdaptive, ModeChunkSizeAdaptive, ModeJointAdaptive}
}

// AllModesWithLinUCB returns the four baseline modes plus the opt-in LinUCB
// mode, in canonical order.
func AllModesWithLinUCB() []Mode {
	return append(AllModes(), ModeLinUCB)
}

// AllModesWithBothLinUCB returns the four baseline modes plus both opt-in
// LinUCB variants (standard, then periodic). It is used by the final
// standard-vs-periodic comparison; the default mode set is unchanged.
func AllModesWithBothLinUCB() []Mode {
	return append(AllModesWithLinUCB(), ModeLinUCBPeriodic)
}

// IsLinUCBMode reports whether mode is one of the opt-in LinUCB modes.
func IsLinUCBMode(mode Mode) bool { return mode == ModeLinUCB || mode == ModeLinUCBPeriodic }

// NetworkStatus describes whether a requested tc/netem condition was in effect.
type NetworkStatus string

const (
	// NetworkStatusNone means no network condition was requested.
	NetworkStatusNone NetworkStatus = "none"
	// NetworkStatusApplied means the requested condition was applied.
	NetworkStatusApplied NetworkStatus = "applied"
	// NetworkStatusUnavailable means a condition was requested but could not be
	// applied (missing privileges/tc). Such runs are not valid measurements of
	// the requested condition and are excluded from condition aggregates.
	NetworkStatusUnavailable NetworkStatus = "unavailable"
)

// ScenarioConfig describes one controlled experiment. Every mode of a scenario
// uses exactly these values; only the Policy differs.
type ScenarioConfig struct {
	FileSize  int64
	ChunkSize int64

	InitialConcurrency int
	MinConcurrency     int
	MaxConcurrency     int

	// InitialRate is the server transfer rate in bytes/sec before the
	// transition; ChangedRate is used from request RateChangeAfterRequests+1
	// onward. The transition is by request count, not wall-clock time.
	InitialRate             int64
	ChangedRate             int64
	RateChangeAfterRequests int64

	// Adaptive chunk-size bounds. When MaxChunkSize > 0 the downloader uses a
	// chunk-size controller (needed by the chunk-size and joint modes).
	MinChunkSize     int64
	MaxChunkSize     int64
	InitialChunkSize int64
	ChunkSizeStep    int64

	ImprovementThreshold float64
	DegradationThreshold float64
	PolicyWindowSize     int

	MaxRetries int
	Timeout    time.Duration
	// Repetitions is the number of independent runs per mode.
	Repetitions int

	// NetworkCondition optionally applies Linux tc/netem shaping for the whole
	// run. nil means no network shaping at all (the default). This is separate
	// from the server-side throttle (InitialRate/ChangedRate) and is never
	// applied unless explicitly set.
	NetworkCondition *network.Config
	// NetworkInterface selects the interface to shape. Empty means resolve it
	// from the experiment server's address (loopback -> "lo").
	NetworkInterface string

	// LinUCBAlpha is the exploration parameter for the opt-in LinUCB mode. It
	// is only consulted when ModeLinUCB is selected; the action count and
	// feature dimension are derived from the existing RL contract. Ignored by
	// the four baseline modes.
	LinUCBAlpha float64
	// LinUCBExplorationInterval, when > 0, enables the LinUCB periodic
	// exploration correction for the opt-in LinUCB mode (see
	// docs/PROJECT_CONTEXT.md "Periodic Exploration Diagnostic"). Zero (the
	// default) is standard LinUCB. Ignored by the four baseline modes.
	LinUCBExplorationInterval int
	// RewardConfig optionally configures the reward used by the opt-in LinUCB
	// mode. When nil, DefaultRewardConfig() is used for LinUCB runs (the four
	// baseline modes ignore reward). This is the existing reward contract; no
	// reward is defined here.
	RewardConfig *rl.RewardConfig

	// ServerDelay adds a deterministic fixed response delay to every request.
	// It is an application-level server delay (server.SetDelay), NOT network
	// shaping, so it needs no privileges. Used by calibration to model latency
	// deterministically.
	ServerDelay time.Duration
	// ServerFailFirstRequests makes the first N requests fail deterministically
	// with ServerFailStatus (default 500). It exposes the experiment server's
	// existing FailNext mechanism so failure/retry reward terms can be
	// calibrated without tc/netem. Zero disables it.
	ServerFailFirstRequests int64
	// ServerFailStatus is the HTTP status for ServerFailFirstRequests. Zero
	// means 500.
	ServerFailStatus int

	// Wall-clock server rate schedule (experimental). When ScheduleInitialRate
	// > 0 the server uses a wall-clock rate transition instead of the
	// request-count switch: InitialRate until ScheduleTransitionAfter has
	// elapsed since the download starts, then ScheduleSubsequentRate. The
	// transition is independent of request count, bytes, chunk size, and
	// concurrency. All three fields must be set together.
	ScheduleInitialRate     int64
	ScheduleTransitionAfter time.Duration
	ScheduleSubsequentRate  int64

	// EnableRequestLog, when true, records per-request server timings in
	// RunResult.RequestLog (diagnostics only; no effect on serving behavior).
	EnableRequestLog bool
}

// DefaultScenarioConfig returns a reasonable, fast experiment configuration.
func DefaultScenarioConfig() ScenarioConfig {
	rw := rl.DefaultRewardConfig()
	return ScenarioConfig{
		FileSize:                256 << 10,
		ChunkSize:               32 << 10,
		InitialConcurrency:      1,
		MinConcurrency:          1,
		MaxConcurrency:          4,
		InitialRate:             512 << 10,
		ChangedRate:             4 << 20,
		RateChangeAfterRequests: 4,
		MinChunkSize:            32 << 10,
		MaxChunkSize:            128 << 10,
		InitialChunkSize:        32 << 10,
		ChunkSizeStep:           32 << 10,
		ImprovementThreshold:    0.2,
		DegradationThreshold:    0.2,
		PolicyWindowSize:        4,
		MaxRetries:              3,
		Timeout:                 30 * time.Second,
		Repetitions:             5,
		LinUCBAlpha:             0.2,
		RewardConfig:            &rw,
	}
}

// Validate checks the scenario configuration.
func (c ScenarioConfig) Validate() error {
	switch {
	case c.FileSize <= 0:
		return fmt.Errorf("%w: file size must be > 0, got %d", ErrInvalidScenario, c.FileSize)
	case c.ChunkSize <= 0:
		return fmt.Errorf("%w: chunk size must be > 0, got %d", ErrInvalidScenario, c.ChunkSize)
	case c.InitialConcurrency < 1:
		return fmt.Errorf("%w: initial concurrency must be >= 1, got %d", ErrInvalidScenario, c.InitialConcurrency)
	case c.MinConcurrency < 1:
		return fmt.Errorf("%w: min concurrency must be >= 1, got %d", ErrInvalidScenario, c.MinConcurrency)
	case c.MaxConcurrency < c.MinConcurrency:
		return fmt.Errorf("%w: max concurrency (%d) must be >= min (%d)", ErrInvalidScenario, c.MaxConcurrency, c.MinConcurrency)
	case c.InitialRate <= 0:
		return fmt.Errorf("%w: initial rate must be > 0, got %d", ErrInvalidScenario, c.InitialRate)
	case c.ChangedRate <= 0:
		return fmt.Errorf("%w: changed rate must be > 0, got %d", ErrInvalidScenario, c.ChangedRate)
	case c.RateChangeAfterRequests < 0:
		return fmt.Errorf("%w: rate change point must be >= 0, got %d", ErrInvalidScenario, c.RateChangeAfterRequests)
	case c.MinChunkSize < 1:
		return fmt.Errorf("%w: min chunk size must be >= 1, got %d", ErrInvalidScenario, c.MinChunkSize)
	case c.MaxChunkSize < c.MinChunkSize:
		return fmt.Errorf("%w: max chunk size (%d) must be >= min (%d)", ErrInvalidScenario, c.MaxChunkSize, c.MinChunkSize)
	case c.InitialChunkSize < c.MinChunkSize || c.InitialChunkSize > c.MaxChunkSize:
		return fmt.Errorf("%w: initial chunk size %d must be within [%d, %d]", ErrInvalidScenario, c.InitialChunkSize, c.MinChunkSize, c.MaxChunkSize)
	case c.ChunkSizeStep < 1:
		return fmt.Errorf("%w: chunk size step must be >= 1, got %d", ErrInvalidScenario, c.ChunkSizeStep)
	case c.ImprovementThreshold < 0:
		return fmt.Errorf("%w: improvement threshold must be >= 0, got %v", ErrInvalidScenario, c.ImprovementThreshold)
	case c.DegradationThreshold < 0:
		return fmt.Errorf("%w: degradation threshold must be >= 0, got %v", ErrInvalidScenario, c.DegradationThreshold)
	case c.MaxRetries < 0:
		return fmt.Errorf("%w: max retries must be >= 0, got %d", ErrInvalidScenario, c.MaxRetries)
	case c.Repetitions < 1:
		return fmt.Errorf("%w: repetitions must be >= 1, got %d", ErrInvalidScenario, c.Repetitions)
	case math.IsNaN(c.LinUCBAlpha) || math.IsInf(c.LinUCBAlpha, 0) || c.LinUCBAlpha < 0:
		return fmt.Errorf("%w: linucb alpha must be finite and >= 0, got %v", ErrInvalidScenario, c.LinUCBAlpha)
	case c.LinUCBExplorationInterval < 0:
		return fmt.Errorf("%w: linucb exploration interval must be >= 0, got %d", ErrInvalidScenario, c.LinUCBExplorationInterval)
	case c.ServerDelay < 0:
		return fmt.Errorf("%w: server delay must be >= 0, got %v", ErrInvalidScenario, c.ServerDelay)
	case c.ServerFailFirstRequests < 0:
		return fmt.Errorf("%w: server fail-first count must be >= 0, got %d", ErrInvalidScenario, c.ServerFailFirstRequests)
	case c.ServerFailStatus != 0 && (c.ServerFailStatus < 400 || c.ServerFailStatus > 599):
		return fmt.Errorf("%w: server fail status must be in [400,599] or 0, got %d", ErrInvalidScenario, c.ServerFailStatus)
	case c.ScheduleInitialRate < 0:
		return fmt.Errorf("%w: schedule initial rate must be >= 0, got %d", ErrInvalidScenario, c.ScheduleInitialRate)
	case c.ScheduleSubsequentRate < 0:
		return fmt.Errorf("%w: schedule subsequent rate must be >= 0, got %d", ErrInvalidScenario, c.ScheduleSubsequentRate)
	case c.ScheduleTransitionAfter < 0:
		return fmt.Errorf("%w: schedule transition must be >= 0, got %v", ErrInvalidScenario, c.ScheduleTransitionAfter)
	case c.ScheduleInitialRate > 0 && (c.ScheduleSubsequentRate <= 0 || c.ScheduleTransitionAfter <= 0):
		return fmt.Errorf("%w: wall-clock schedule requires subsequent rate > 0 and transition > 0", ErrInvalidScenario)
	}
	if c.NetworkCondition != nil {
		if err := c.NetworkCondition.Validate(); err != nil {
			return fmt.Errorf("%w: network condition: %w", ErrInvalidScenario, err)
		}
	}
	if c.RewardConfig != nil {
		if err := c.RewardConfig.Validate(); err != nil {
			return fmt.Errorf("%w: reward config: %w", ErrInvalidScenario, err)
		}
	}
	return nil
}

// ConcurrencyEvent is one policy decision affecting concurrency, keyed by
// request index rather than wall-clock time.
type ConcurrencyEvent struct {
	RequestIndex int64
	Concurrency  int
	Action       string
}

// ChunkSizeEvent is one policy decision affecting the chunk-size target, keyed
// by request index rather than wall-clock time.
type ChunkSizeEvent struct {
	RequestIndex int64
	ChunkSize    int64
	Action       policy.ActionKind
}

// DecisionEvent is a per-decision diagnostic record used by the LinUCB
// calibration. It captures the observation at decision time, the selected
// action, the reward the runner attributed to that action (0 for deterministic
// modes and for the first decision, which has no outcome yet), and the
// controller targets before/after the action. It is populated only for the
// opt-in LinUCB mode; the four baseline modes leave it empty.
type DecisionEvent struct {
	// RequestIndex is Context.TotalAttempts at decision time.
	RequestIndex int64
	// Action is the selected action kind.
	Action policy.ActionKind
	// ActionName is the action's String() form.
	ActionName string
	// ActionIndex is the RL action-space index (rl.ActionIndex), or -1 when the
	// action is not part of the learning action space.
	ActionIndex int
	// Reward is the scalar reward the runner passed to Update for this action.
	Reward float64
	// Concurrency/ChunkSize are the controller targets at decision time (before
	// this action).
	Concurrency int
	ChunkSize   int64
	// ConcurrencyAfter/ChunkSizeAfter are the controller targets after this
	// action was applied (observed at the next decision, or the run's final
	// values for the last decision).
	ConcurrencyAfter int
	ChunkSizeAfter   int64
	// Throughput, AggregateThroughput, RequestDuration, Retries,
	// FailedRequests are the observation fields needed to reconstruct the
	// reward components.
	Throughput          float64
	AggregateThroughput float64
	RequestDuration     time.Duration
	Retries             int64
	FailedRequests      int64
}

// RunResult holds the measurements from a single run.
type RunResult struct {
	Mode Mode
	// Scenario is the matrix scenario name; empty for a single-scenario run.
	Scenario string
	// Repetition is the 0-based repetition index within a scenario.
	Repetition int

	Duration time.Duration

	FileSize         int64
	UsefulBytes      int64
	TransferredBytes int64

	TotalAttempts      int64
	SuccessfulAttempts int64
	FailedAttempts     int64
	RetryAttempts      int64

	AverageThroughput float64
	MinThroughput     float64
	MaxThroughput     float64

	InitialConcurrency int
	FinalConcurrency   int
	MaxConcurrency     int

	InitialChunkSize int64
	FinalChunkSize   int64
	MinChunkSize     int64
	MaxChunkSize     int64

	ConcurrencyTrajectory []ConcurrencyEvent
	ChunkSizeTrajectory   []ChunkSizeEvent

	// RequestLog is the per-request server timing log (diagnostics only;
	// populated when ScenarioConfig.EnableRequestLog is set).
	RequestLog []server.RequestEvent

	// Decisions is the per-decision diagnostic record. Populated only for the
	// opt-in LinUCB mode.
	Decisions []DecisionEvent

	// ConcurrencyActions and ChunkSizeActions count applied (non-none) actions.
	ConcurrencyActions int64
	ChunkSizeActions   int64

	OutputMatches     bool
	NoDuplicateRanges bool
	NoMissingRanges   bool

	// NetworkInterface is the interface shaped for this run (empty when no
	// network condition was requested).
	NetworkInterface string
	// NetworkApplied reports whether a tc/netem condition was applied.
	NetworkApplied bool
	// RequestedNetworkCondition is the condition the scenario asked for (nil
	// when none was requested).
	RequestedNetworkCondition *network.Config
	// AppliedNetworkCondition is the network condition that was actually
	// applied (nil when none was applied).
	AppliedNetworkCondition *network.Config
	// NetworkConditionStatus records whether the requested condition was in
	// effect.
	NetworkConditionStatus NetworkStatus

	// Err is set when the download or network setup failed; the result is still
	// returned.
	Err string
}

// ExperimentResult holds every individual run for each mode. LinUCB is populated
// only when the opt-in mode is selected.
type ExperimentResult struct {
	Config              ScenarioConfig
	Fixed               []RunResult
	ConcurrencyAdaptive []RunResult
	ChunkSizeAdaptive   []RunResult
	JointAdaptive       []RunResult
	LinUCB              []RunResult
	LinUCBPeriodic      []RunResult
}

// Runs returns the runs for a mode.
func (r ExperimentResult) Runs(mode Mode) []RunResult {
	switch mode {
	case ModeFixed:
		return r.Fixed
	case ModeConcurrencyAdaptive:
		return r.ConcurrencyAdaptive
	case ModeChunkSizeAdaptive:
		return r.ChunkSizeAdaptive
	case ModeJointAdaptive:
		return r.JointAdaptive
	case ModeLinUCB:
		return r.LinUCB
	case ModeLinUCBPeriodic:
		return r.LinUCBPeriodic
	default:
		return nil
	}
}

func (r *ExperimentResult) add(mode Mode, rr RunResult) {
	switch mode {
	case ModeFixed:
		r.Fixed = append(r.Fixed, rr)
	case ModeConcurrencyAdaptive:
		r.ConcurrencyAdaptive = append(r.ConcurrencyAdaptive, rr)
	case ModeChunkSizeAdaptive:
		r.ChunkSizeAdaptive = append(r.ChunkSizeAdaptive, rr)
	case ModeJointAdaptive:
		r.JointAdaptive = append(r.JointAdaptive, rr)
	case ModeLinUCB:
		r.LinUCB = append(r.LinUCB, rr)
	case ModeLinUCBPeriodic:
		r.LinUCBPeriodic = append(r.LinUCBPeriodic, rr)
	}
}

// Summary aggregates the modes. LinUCB modes are populated only when selected.
type Summary struct {
	Fixed               ModeAggregate
	ConcurrencyAdaptive ModeAggregate
	ChunkSizeAdaptive   ModeAggregate
	JointAdaptive       ModeAggregate
	LinUCB              ModeAggregate
	LinUCBPeriodic      ModeAggregate
}

// Summary returns aggregate statistics for all modes.
func (r ExperimentResult) Summary() Summary {
	return Summary{
		Fixed:               aggregateRuns(r.Fixed),
		ConcurrencyAdaptive: aggregateRuns(r.ConcurrencyAdaptive),
		ChunkSizeAdaptive:   aggregateRuns(r.ChunkSizeAdaptive),
		JointAdaptive:       aggregateRuns(r.JointAdaptive),
		LinUCB:              aggregateRuns(r.LinUCB),
		LinUCBPeriodic:      aggregateRuns(r.LinUCBPeriodic),
	}
}

// Aggregate returns the summary for a mode.
func (s Summary) Aggregate(mode Mode) ModeAggregate {
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

// Run executes the scenario for the four baseline modes, Repetitions times
// each, using a fresh server and output file per run. The default behavior is
// unchanged; LinUCB is opt-in via RunSelected.
func Run(cfg ScenarioConfig) (ExperimentResult, error) {
	return run(cfg, AllModes(), network.NewController())
}

// RunSelected executes the scenario for the given modes (Repetitions times
// each). Pass AllModesWithLinUCB() to include the opt-in LinUCB mode. Modes are
// run in the given order with fresh server/output/state per run.
func RunSelected(cfg ScenarioConfig, modes []Mode) (ExperimentResult, error) {
	if err := validateModes(modes); err != nil {
		return ExperimentResult{}, err
	}
	return run(cfg, modes, network.NewController())
}

// run is Run with an injectable network controller and explicit mode list (used
// by tests and RunSelected).
func run(cfg ScenarioConfig, modes []Mode, netCtl *network.Controller) (ExperimentResult, error) {
	if err := cfg.Validate(); err != nil {
		return ExperimentResult{}, err
	}
	if err := validateModes(modes); err != nil {
		return ExperimentResult{}, err
	}
	content := contentPattern(cfg.FileSize)

	res := ExperimentResult{Config: cfg}
	for i := 0; i < cfg.Repetitions; i++ {
		for _, mode := range modes {
			rr, err := runOnce(cfg, mode, content, netCtl)
			if err != nil {
				return res, fmt.Errorf("%s run %d: %w", mode, i, err)
			}
			rr.Repetition = i
			res.add(mode, rr)
		}
	}
	return res, nil
}

// rlStateConfig builds the RL normalization Config from a scenario so the
// Adapter and the downloader's RewardCalculator normalize Contexts into States
// identically. It mirrors policyRunner.rewarderConfig bounds; only the raw
// State fields (throughput, duration, counters) differ, and those are not
// affected by these bounds.
func rlStateConfig(cfg ScenarioConfig) rl.Config {
	maxChunk := cfg.MaxChunkSize
	if maxChunk <= 0 {
		maxChunk = cfg.ChunkSize
	}
	return rl.Config{
		MinConcurrency:     cfg.MinConcurrency,
		MaxConcurrency:     cfg.MaxConcurrency,
		MinChunkSize:       1,
		MaxChunkSize:       maxChunk,
		MaxThroughput:      1e9,
		MaxRequestDuration: 1 << 40,
		CounterScale:       1000,
	}
}

// validateModes rejects empty mode lists and unknown modes.
func validateModes(modes []Mode) error {
	if len(modes) == 0 {
		return fmt.Errorf("%w: no modes selected", ErrInvalidScenario)
	}
	for _, m := range modes {
		switch m {
		case ModeFixed, ModeConcurrencyAdaptive, ModeChunkSizeAdaptive, ModeJointAdaptive, ModeLinUCB, ModeLinUCBPeriodic:
		default:
			return fmt.Errorf("%w: unknown mode %q", ErrInvalidScenario, m)
		}
	}
	return nil
}

func runOnce(cfg ScenarioConfig, mode Mode, content []byte, netCtl *network.Controller) (res RunResult, err error) {
	res.Mode = mode
	srv := server.New(content)
	srv.SetRate(cfg.InitialRate)
	if cfg.RateChangeAfterRequests > 0 {
		srv.SetRateAfter(cfg.RateChangeAfterRequests, cfg.ChangedRate)
	}
	if cfg.EnableRequestLog {
		srv.SetRequestLogEnabled(true)
	}
	if cfg.ServerDelay > 0 {
		srv.SetDelay(cfg.ServerDelay)
	}
	if cfg.ServerFailFirstRequests > 0 {
		status := cfg.ServerFailStatus
		if status == 0 {
			status = http.StatusInternalServerError
		}
		srv.FailNextStatus(int(cfg.ServerFailFirstRequests), status)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	dir, err := os.MkdirTemp("", "adaptivedl-exp-")
	if err != nil {
		return res, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	// Network shaping is only ever applied when explicitly configured, and is
	// always cleared on the way out, including on failure. If it cannot be
	// applied, the run is marked unavailable and returned (not treated as a
	// valid measurement of the requested condition).
	netStatus := NetworkStatusNone
	netIface := ""
	var requestedCond, appliedCond *network.Config
	if cfg.NetworkCondition != nil {
		requestedCond = cfg.NetworkCondition
		iface := cfg.NetworkInterface
		if iface == "" {
			u, perr := url.Parse(ts.URL)
			if perr != nil {
				return res, fmt.Errorf("parse server URL: %w", perr)
			}
			iface, err = netCtl.InterfaceForHost(u.Hostname())
			if err != nil {
				res.NetworkConditionStatus = NetworkStatusUnavailable
				res.RequestedNetworkCondition = requestedCond
				res.Err = err.Error()
				return res, nil
			}
		}
		netIface = iface
		if !network.InterfaceExists(iface) {
			res.NetworkInterface = iface
			res.NetworkConditionStatus = NetworkStatusUnavailable
			res.RequestedNetworkCondition = requestedCond
			res.Err = fmt.Sprintf("%v: %q", network.ErrInvalidInterface, iface)
			return res, nil
		}
		if aerr := netCtl.Apply(iface, *cfg.NetworkCondition); aerr != nil {
			res.NetworkInterface = iface
			res.NetworkConditionStatus = NetworkStatusUnavailable
			res.RequestedNetworkCondition = requestedCond
			res.Err = fmt.Sprintf("apply network condition: %v", aerr)
			return res, nil
		}
		appliedCond = cfg.NetworkCondition
		netStatus = NetworkStatusApplied
		defer func() {
			if cerr := netCtl.Clear(iface); cerr != nil && !errors.Is(cerr, network.ErrNoQdisc) && err == nil {
				err = fmt.Errorf("clear network condition on %q: %w", iface, cerr)
			}
		}()
	}

	dlCfg := downloader.Config{
		URL:              ts.URL,
		OutputPath:       outPath,
		TotalSize:        cfg.FileSize,
		Concurrency:      cfg.InitialConcurrency,
		ChunkSize:        cfg.ChunkSize,
		Timeout:          cfg.Timeout,
		MaxRetries:       cfg.MaxRetries,
		MinConcurrency:   cfg.MinConcurrency,
		MaxConcurrency:   cfg.MaxConcurrency,
		MinChunkSize:     cfg.MinChunkSize,
		MaxChunkSize:     cfg.MaxChunkSize,
		InitialChunkSize: cfg.InitialChunkSize,
		ChunkSizeStep:    cfg.ChunkSizeStep,
		PolicyWindowSize: cfg.PolicyWindowSize,
	}

	// The only difference between modes is the Policy supplied to the
	// downloader. All scenarios share the same controllers and limits.
	var recorder *recordingPolicy
	switch mode {
	case ModeFixed:
		// No policy: both dimensions stay at their initial values.
	case ModeConcurrencyAdaptive:
		inner, perr := policy.NewThroughputPolicy(policy.ThroughputPolicyConfig{
			MinConcurrency:       cfg.MinConcurrency,
			MaxConcurrency:       cfg.MaxConcurrency,
			ImprovementThreshold: cfg.ImprovementThreshold,
			DegradationThreshold: cfg.DegradationThreshold,
		})
		if perr != nil {
			return RunResult{}, fmt.Errorf("throughput policy: %w", perr)
		}
		recorder = newRecordingPolicy(inner)
		dlCfg.Policy = recorder
	case ModeChunkSizeAdaptive:
		inner, perr := policy.NewChunkSizePolicy(policy.ChunkSizePolicyConfig{
			MinChunkSize:         cfg.MinChunkSize,
			MaxChunkSize:         cfg.MaxChunkSize,
			ImprovementThreshold: cfg.ImprovementThreshold,
			DegradationThreshold: cfg.DegradationThreshold,
		})
		if perr != nil {
			return RunResult{}, fmt.Errorf("chunk-size policy: %w", perr)
		}
		recorder = newRecordingPolicy(inner)
		dlCfg.Policy = recorder
	case ModeJointAdaptive:
		inner, perr := policy.NewJointPolicy(policy.JointPolicyConfig{
			MinConcurrency:       cfg.MinConcurrency,
			MaxConcurrency:       cfg.MaxConcurrency,
			MinChunkSize:         cfg.MinChunkSize,
			MaxChunkSize:         cfg.MaxChunkSize,
			ImprovementThreshold: cfg.ImprovementThreshold,
			DegradationThreshold: cfg.DegradationThreshold,
		})
		if perr != nil {
			return RunResult{}, fmt.Errorf("joint policy: %w", perr)
		}
		recorder = newRecordingPolicy(inner)
		dlCfg.Policy = recorder
	case ModeLinUCB, ModeLinUCBPeriodic:
		// Opt-in learning modes. The learner sits behind the existing RL
		// Adapter and the existing policy.Policy interface; the downloader and
		// policyRunner are unchanged. The reward is computed by the existing
		// RewardCalculator from the existing RewardConfig.
		explorationInterval := cfg.LinUCBExplorationInterval
		if mode == ModeLinUCBPeriodic {
			explorationInterval = LinUCBPeriodicInterval
		}
		inner, perr := rl.NewLinUCB(rl.LinUCBConfig{
			Alpha:      cfg.LinUCBAlpha,
			NumActions: rl.NumActions,
			Dimension:  len(rl.FeatureNames()),
			// One deterministic initial-exploration pass so every action
			// receives at least one update before normal LinUCB selection (see
			// "LinUCB Exploration Correction" in docs/PROJECT_CONTEXT.md).
			InitialExplorationRounds: 1,
			// Standard mode uses the configured interval (default 0); the
			// periodic mode uses the fixed LinUCBPeriodicInterval.
			ExplorationInterval: explorationInterval,
		})
		if perr != nil {
			return RunResult{}, fmt.Errorf("linucb: %w", perr)
		}
		rewarder := cfg.RewardConfig
		if rewarder == nil {
			def := rl.DefaultRewardConfig()
			rewarder = &def
		}
		adapter, perr := rl.NewAdapter(inner, rlStateConfig(cfg))
		if perr != nil {
			return RunResult{}, fmt.Errorf("linucb adapter: %w", perr)
		}
		recorder = newRecordingPolicy(adapter)
		dlCfg.Policy = recorder
		dlCfg.RewardConfig = rewarder
	default:
		return RunResult{}, fmt.Errorf("%w: unknown mode %q", ErrInvalidScenario, mode)
	}

	dl, err := downloader.New(dlCfg)
	if err != nil {
		return RunResult{}, fmt.Errorf("downloader: %w", err)
	}

	initialConcurrency := dl.Concurrency()
	initialChunkSize := dl.ChunkSize()
	start := time.Now()
	if cfg.ScheduleInitialRate > 0 {
		// The wall-clock schedule is anchored to the download start so the
		// transition is independent of request count and granularity.
		if err := srv.SetRateScheduleAt(start, server.RateSchedule{
			InitialRate:     cfg.ScheduleInitialRate,
			TransitionAfter: cfg.ScheduleTransitionAfter,
			SubsequentRate:  cfg.ScheduleSubsequentRate,
		}); err != nil {
			return RunResult{}, fmt.Errorf("rate schedule: %w", err)
		}
	}
	runErr := dl.Download(context.Background())
	duration := time.Since(start)

	snap := dl.Metrics()
	res = RunResult{
		Mode:               mode,
		Duration:           duration,
		FileSize:           cfg.FileSize,
		UsefulBytes:        cfg.FileSize,
		TransferredBytes:   snap.Aggregate.TotalBytes,
		TotalAttempts:      snap.Aggregate.TotalAttempts,
		SuccessfulAttempts: snap.Aggregate.SuccessfulRequests,
		FailedAttempts:     snap.Aggregate.FailedRequests,
		AverageThroughput:  snap.Aggregate.AverageThroughput,
		MinThroughput:      snap.Aggregate.MinThroughput,
		MaxThroughput:      snap.Aggregate.MaxThroughput,
		InitialConcurrency: initialConcurrency,
		FinalConcurrency:   dl.Concurrency(),
		InitialChunkSize:   initialChunkSize,
		FinalChunkSize:     dl.ChunkSize(),
	}
	for _, rec := range snap.Records {
		if rec.Attempt > 1 {
			res.RetryAttempts++
		}
	}
	if cfg.EnableRequestLog {
		res.RequestLog = srv.RequestLog()
	}
	if recorder != nil {
		res.ConcurrencyTrajectory = recorder.concurrencyEvents()
		res.ChunkSizeTrajectory = recorder.chunkSizeEvents()
		if IsLinUCBMode(mode) {
			// Fill the "after action" controller targets: a decision's action is
			// observed in the next decision's context; the last one uses the
			// run's final values.
			decisions := recorder.decisionEvents()
			for i := range decisions {
				if i+1 < len(decisions) {
					decisions[i].ConcurrencyAfter = decisions[i+1].Concurrency
					decisions[i].ChunkSizeAfter = decisions[i+1].ChunkSize
				} else {
					decisions[i].ConcurrencyAfter = dl.Concurrency()
					decisions[i].ChunkSizeAfter = dl.ChunkSize()
				}
			}
			res.Decisions = decisions
		}
	}
	res.MaxConcurrency = res.FinalConcurrency
	for _, e := range res.ConcurrencyTrajectory {
		if e.Concurrency > res.MaxConcurrency {
			res.MaxConcurrency = e.Concurrency
		}
		if e.Action == policy.IncreaseConcurrency().String() || e.Action == policy.DecreaseConcurrency().String() {
			res.ConcurrencyActions++
		}
	}
	res.MinChunkSize = res.InitialChunkSize
	res.MaxChunkSize = res.InitialChunkSize
	for _, e := range res.ChunkSizeTrajectory {
		if e.ChunkSize < res.MinChunkSize {
			res.MinChunkSize = e.ChunkSize
		}
		if e.ChunkSize > res.MaxChunkSize {
			res.MaxChunkSize = e.ChunkSize
		}
		switch e.Action {
		case policy.ActionIncreaseChunkSize, policy.ActionDecreaseChunkSize:
			res.ChunkSizeActions++
		}
	}
	if res.FinalChunkSize < res.MinChunkSize {
		res.MinChunkSize = res.FinalChunkSize
	}
	if res.FinalChunkSize > res.MaxChunkSize {
		res.MaxChunkSize = res.FinalChunkSize
	}
	res.NetworkInterface = netIface
	res.NetworkApplied = netStatus == NetworkStatusApplied
	res.RequestedNetworkCondition = requestedCond
	res.AppliedNetworkCondition = appliedCond
	res.NetworkConditionStatus = netStatus

	if runErr != nil {
		res.Err = runErr.Error()
		return res, nil
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		res.Err = err.Error()
		return res, nil
	}
	res.OutputMatches = bytes.Equal(got, content)
	res.NoDuplicateRanges, res.NoMissingRanges = verifyRanges(snap.Records, cfg.FileSize)
	return res, nil
}

// verifyRanges checks the successful attempt ranges are disjoint and cover
// [0, size) exactly.
func verifyRanges(records []metrics.Record, size int64) (noDuplicates, noMissing bool) {
	type segment struct{ start, end int64 }
	segments := make([]segment, 0, len(records))
	for _, r := range records {
		if r.Success {
			segments = append(segments, segment{start: r.RangeStart, end: r.RangeEnd})
		}
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].start < segments[j].start })

	noDuplicates = true
	noMissing = true
	var prevEnd int64 = -1
	for _, s := range segments {
		if s.start <= prevEnd {
			noDuplicates = false
		}
		if s.start != prevEnd+1 {
			noMissing = false
		}
		if s.end > prevEnd {
			prevEnd = s.end
		}
	}
	if prevEnd != size-1 {
		noMissing = false
	}
	return noDuplicates, noMissing
}

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

// recordingPolicy wraps a Policy and records each decision's concurrency and
// chunk-size targets. The inner policy is called serially by the downloader, so
// recording does not affect timing or determinism.
type recordingPolicy struct {
	inner policy.Policy

	mu          sync.Mutex
	concurrency []ConcurrencyEvent
	chunkSize   []ChunkSizeEvent
	decisions   []DecisionEvent
}

func newRecordingPolicy(inner policy.Policy) *recordingPolicy {
	return &recordingPolicy{inner: inner}
}

func (p *recordingPolicy) SelectAction(ctx policy.Context) policy.Action {
	action := p.inner.SelectAction(ctx)
	p.mu.Lock()
	p.concurrency = append(p.concurrency, ConcurrencyEvent{
		RequestIndex: ctx.TotalAttempts,
		Concurrency:  ctx.Concurrency,
		Action:       action.String(),
	})
	p.chunkSize = append(p.chunkSize, ChunkSizeEvent{
		RequestIndex: ctx.TotalAttempts,
		ChunkSize:    ctx.ChunkSize,
		Action:       action.Kind,
	})
	actionIndex := -1
	if idx, err := rl.ActionIndexFromAction(action); err == nil {
		actionIndex = int(idx)
	}
	p.decisions = append(p.decisions, DecisionEvent{
		RequestIndex:        ctx.TotalAttempts,
		Action:              action.Kind,
		ActionName:          action.String(),
		ActionIndex:         actionIndex,
		Concurrency:         ctx.Concurrency,
		ChunkSize:           ctx.ChunkSize,
		Throughput:          ctx.Throughput,
		AggregateThroughput: ctx.AggregateThroughput,
		RequestDuration:     ctx.RequestDuration,
		Retries:             ctx.Retries,
		FailedRequests:      ctx.FailedRequests,
	})
	p.mu.Unlock()
	return action
}

func (p *recordingPolicy) Update(ctx policy.Context, action policy.Action, reward float64) {
	// Update is called (by the serialized runner) with the PREVIOUS action, and
	// before the next SelectAction, so the reward belongs to the most recently
	// recorded decision.
	p.mu.Lock()
	if n := len(p.decisions); n > 0 {
		p.decisions[n-1].Reward = reward
	}
	p.mu.Unlock()
	p.inner.Update(ctx, action, reward)
}

func (p *recordingPolicy) decisionEvents() []DecisionEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]DecisionEvent(nil), p.decisions...)
}

func (p *recordingPolicy) concurrencyEvents() []ConcurrencyEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ConcurrencyEvent(nil), p.concurrency...)
}

func (p *recordingPolicy) chunkSizeEvents() []ChunkSizeEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ChunkSizeEvent(nil), p.chunkSize...)
}
