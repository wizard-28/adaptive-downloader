package experiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
)

type networkCall struct {
	name string
	args []string
}

type fakeNetwork struct {
	mu    sync.Mutex
	calls []networkCall
}

func (f *fakeNetwork) run(name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, networkCall{name: name, args: append([]string(nil), args...)})
	return nil, nil
}

func (f *fakeNetwork) controller() *network.Controller {
	return &network.Controller{Exec: f.run}
}

func (f *fakeNetwork) recorded() []networkCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]networkCall(nil), f.calls...)
}

func (f *fakeNetwork) applied() bool {
	for _, c := range f.recorded() {
		if c.name == "tc" && len(c.args) >= 2 && c.args[0] == "qdisc" && c.args[1] == "replace" {
			return true
		}
	}
	return false
}

func (f *fakeNetwork) cleared() bool {
	for _, c := range f.recorded() {
		if c.name == "tc" && len(c.args) >= 2 && c.args[0] == "qdisc" && c.args[1] == "del" {
			return true
		}
	}
	return false
}

func testScenario() ScenarioConfig {
	cfg := DefaultScenarioConfig()
	cfg.FileSize = 400
	cfg.ChunkSize = 50
	cfg.InitialConcurrency = 1
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 4
	cfg.InitialRate = 2000
	cfg.ChangedRate = 1000000
	cfg.RateChangeAfterRequests = 3
	cfg.MinChunkSize = 50
	cfg.MaxChunkSize = 200
	cfg.InitialChunkSize = 50
	cfg.ChunkSizeStep = 50
	cfg.ImprovementThreshold = 0.1
	cfg.DegradationThreshold = 0.1
	cfg.PolicyWindowSize = 4
	cfg.MaxRetries = 3
	cfg.Timeout = 5 * time.Second
	cfg.Repetitions = 1
	return cfg
}

func TestRunFixedCompletes(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if len(res.Fixed) != 1 {
		t.Fatalf("len(Fixed) = %d, want 1", len(res.Fixed))
	}
	r := res.Fixed[0]
	if r.Err != "" {
		t.Fatalf("fixed run error: %s", r.Err)
	}
	if !r.OutputMatches || !r.NoDuplicateRanges || !r.NoMissingRanges {
		t.Fatalf("correctness = output %v duplicates %v missing %v", r.OutputMatches, r.NoDuplicateRanges, r.NoMissingRanges)
	}
	if r.SuccessfulAttempts != 8 || r.FailedAttempts != 0 {
		t.Fatalf("attempts success/fail = %d/%d, want 8/0", r.SuccessfulAttempts, r.FailedAttempts)
	}
	if r.InitialConcurrency != 1 || r.FinalConcurrency != 1 || r.MaxConcurrency != 1 {
		t.Fatalf("fixed concurrency = initial %d final %d max %d, want all 1", r.InitialConcurrency, r.FinalConcurrency, r.MaxConcurrency)
	}
	if r.InitialChunkSize != 50 || r.FinalChunkSize != 50 || r.MinChunkSize != 50 || r.MaxChunkSize != 50 {
		t.Fatalf("fixed chunk = initial %d final %d min %d max %d, want all 50", r.InitialChunkSize, r.FinalChunkSize, r.MinChunkSize, r.MaxChunkSize)
	}
	if len(r.ConcurrencyTrajectory) != 0 || len(r.ChunkSizeTrajectory) != 0 {
		t.Fatalf("fixed mode recorded trajectories without a policy")
	}
}

func TestRunConcurrencyAdaptiveCompletes(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if len(res.ConcurrencyAdaptive) != 1 {
		t.Fatalf("len(ConcurrencyAdaptive) = %d, want 1", len(res.ConcurrencyAdaptive))
	}
	r := res.ConcurrencyAdaptive[0]
	if r.Err != "" {
		t.Fatalf("concurrency-adaptive run error: %s", r.Err)
	}
	if !r.OutputMatches || !r.NoDuplicateRanges || !r.NoMissingRanges {
		t.Fatalf("correctness = output %v duplicates %v missing %v", r.OutputMatches, r.NoDuplicateRanges, r.NoMissingRanges)
	}
	if r.SuccessfulAttempts != 8 || r.FailedAttempts != 0 {
		t.Fatalf("attempts success/fail = %d/%d, want 8/0", r.SuccessfulAttempts, r.FailedAttempts)
	}
	// Concurrency-only adaptation must never change the chunk size.
	if r.InitialChunkSize != 50 || r.FinalChunkSize != 50 || r.MinChunkSize != 50 || r.MaxChunkSize != 50 {
		t.Fatalf("concurrency-adaptive changed chunk size: %+v", r)
	}
}

func TestAdaptiveRecordsConcurrencyChanges(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	r := res.ConcurrencyAdaptive[0]
	if len(r.ConcurrencyTrajectory) != 8 {
		t.Fatalf("trajectory length = %d, want 8 (one per attempt)", len(r.ConcurrencyTrajectory))
	}
	changed := false
	for _, e := range r.ConcurrencyTrajectory {
		if e.Action != "none" {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("no concurrency change recorded: %+v", r.ConcurrencyTrajectory)
	}
	if r.MaxConcurrency <= r.InitialConcurrency {
		t.Fatalf("MaxConcurrency = %d, want > initial %d", r.MaxConcurrency, r.InitialConcurrency)
	}
	if r.MaxConcurrency > 4 {
		t.Fatalf("MaxConcurrency = %d, want <= max 4", r.MaxConcurrency)
	}
}

func TestFourModesControlExpectations(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}

	// Fixed: nothing changes.
	fixed := res.Fixed[0]
	if fixed.FinalConcurrency != fixed.InitialConcurrency || fixed.FinalChunkSize != fixed.InitialChunkSize {
		t.Fatalf("fixed changed: %+v", fixed)
	}

	// Concurrency-only: chunk size fixed; concurrency may change.
	conc := res.ConcurrencyAdaptive[0]
	if conc.FinalChunkSize != conc.InitialChunkSize {
		t.Fatalf("concurrency-only changed chunk size: %+v", conc)
	}
	if len(conc.ChunkSizeTrajectory) == 0 {
		t.Fatal("concurrency-only has no chunk-size trajectory")
	}
	for _, e := range conc.ChunkSizeTrajectory {
		if e.ChunkSize != conc.InitialChunkSize {
			t.Fatalf("concurrency-only chunk trajectory changed: %+v", e)
		}
	}

	// Chunk-size-only: concurrency fixed; chunk size may change.
	chunk := res.ChunkSizeAdaptive[0]
	if chunk.FinalConcurrency != chunk.InitialConcurrency {
		t.Fatalf("chunk-size-only changed concurrency: %+v", chunk)
	}
	if len(chunk.ConcurrencyTrajectory) == 0 {
		t.Fatal("chunk-size-only has no concurrency trajectory")
	}
	for _, e := range chunk.ConcurrencyTrajectory {
		if e.Concurrency != chunk.InitialConcurrency {
			t.Fatalf("chunk-size-only concurrency trajectory changed: %+v", e)
		}
	}

	// Joint: either or both may change; both trajectories exist.
	joint := res.JointAdaptive[0]
	if len(joint.ConcurrencyTrajectory) == 0 || len(joint.ChunkSizeTrajectory) == 0 {
		t.Fatal("joint mode missing trajectories")
	}
}

func TestChunkSizeAdaptiveChangesChunkSize(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	r := res.ChunkSizeAdaptive[0]
	if r.Err != "" || !r.OutputMatches {
		t.Fatalf("chunk-size-adaptive run not correct: %+v", r)
	}
	// The server rate transition drives a throughput increase, so the chunk
	// size should grow; concurrency must stay fixed.
	if r.FinalChunkSize <= r.InitialChunkSize {
		t.Fatalf("chunk-size-adaptive chunk did not grow: %d -> %d", r.InitialChunkSize, r.FinalChunkSize)
	}
	if r.FinalConcurrency != r.InitialConcurrency {
		t.Fatalf("chunk-size-adaptive changed concurrency: %d -> %d", r.InitialConcurrency, r.FinalConcurrency)
	}
	if len(r.ChunkSizeTrajectory) == 0 {
		t.Fatal("chunk-size trajectory is empty")
	}
	// Trajectory records the target chunk size at each decision.
	for _, e := range r.ChunkSizeTrajectory {
		if e.ChunkSize < r.MinChunkSize || e.ChunkSize > r.MaxChunkSize {
			t.Fatalf("chunk trajectory value %d outside observed [%d,%d]", e.ChunkSize, r.MinChunkSize, r.MaxChunkSize)
		}
	}
}

func TestChunkSizeTrajectoryOnlyForAdaptiveModes(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if got := len(res.Fixed[0].ChunkSizeTrajectory); got != 0 {
		t.Fatalf("fixed mode chunk trajectory = %d, want 0", got)
	}
	if got := len(res.ConcurrencyAdaptive[0].ChunkSizeTrajectory); got == 0 {
		t.Fatal("concurrency-adaptive mode missing chunk trajectory")
	}
	if got := len(res.ChunkSizeAdaptive[0].ChunkSizeTrajectory); got == 0 {
		t.Fatal("chunk-size-adaptive mode missing chunk trajectory")
	}
	if got := len(res.JointAdaptive[0].ChunkSizeTrajectory); got == 0 {
		t.Fatal("joint mode missing chunk trajectory")
	}
}

func TestBothModesReconstructIdenticalContent(t *testing.T) {
	cfg := testScenario()
	cfg.Repetitions = 2

	res, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	for _, mode := range AllModes() {
		for _, r := range res.Runs(mode) {
			if r.Err != "" {
				t.Fatalf("%s run error: %s", r.Mode, r.Err)
			}
			if !r.OutputMatches {
				t.Fatalf("%s run output does not match source", r.Mode)
			}
		}
	}
}

func TestResultsContainRequiredMetrics(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	r := res.ConcurrencyAdaptive[0]

	if r.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", r.Duration)
	}
	if r.FileSize != 400 || r.UsefulBytes != 400 {
		t.Errorf("file/useful bytes = %d/%d, want 400/400", r.FileSize, r.UsefulBytes)
	}
	if r.TotalAttempts != 8 || r.SuccessfulAttempts != 8 || r.FailedAttempts != 0 {
		t.Errorf("attempts total/success/fail = %d/%d/%d, want 8/8/0", r.TotalAttempts, r.SuccessfulAttempts, r.FailedAttempts)
	}
	if r.TransferredBytes != 400 {
		t.Errorf("TransferredBytes = %d, want 400", r.TransferredBytes)
	}
	if r.AverageThroughput <= 0 || r.MinThroughput <= 0 || r.MaxThroughput < r.MinThroughput {
		t.Errorf("throughput avg/min/max = %v/%v/%v", r.AverageThroughput, r.MinThroughput, r.MaxThroughput)
	}
	if r.InitialConcurrency != 1 || r.FinalConcurrency < 1 {
		t.Errorf("concurrency initial/final = %d/%d", r.InitialConcurrency, r.FinalConcurrency)
	}
	if r.MaxConcurrency < r.FinalConcurrency {
		t.Errorf("MaxConcurrency %d < FinalConcurrency %d", r.MaxConcurrency, r.FinalConcurrency)
	}
	if r.InitialChunkSize != 50 || r.FinalChunkSize != 50 {
		t.Errorf("chunk initial/final = %d/%d, want 50/50", r.InitialChunkSize, r.FinalChunkSize)
	}
	if r.MinChunkSize != 50 || r.MaxChunkSize != 50 {
		t.Errorf("chunk min/max = %d/%d, want 50/50", r.MinChunkSize, r.MaxChunkSize)
	}
}

func TestRepeatedRunsIndependent(t *testing.T) {
	cfg := testScenario()
	cfg.Repetitions = 3

	res, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	for _, mode := range AllModes() {
		if got := len(res.Runs(mode)); got != 3 {
			t.Fatalf("mode %s run count = %d, want 3", mode, got)
		}
	}
	// Fixed and concurrency-adaptive keep the chunk size at 50, so they always
	// make 8 attempts. Adaptive-chunk modes may use fewer.
	for _, mode := range []Mode{ModeFixed, ModeConcurrencyAdaptive} {
		for i, r := range res.Runs(mode) {
			if r.TotalAttempts != 8 {
				t.Fatalf("%s run %d TotalAttempts = %d, want 8 (fresh state)", mode, i, r.TotalAttempts)
			}
		}
	}

	s := res.Summary()
	if s.Fixed.Runs != 3 || s.ConcurrencyAdaptive.Runs != 3 || s.ChunkSizeAdaptive.Runs != 3 || s.JointAdaptive.Runs != 3 {
		t.Fatalf("summary runs = %d/%d/%d/%d, want all 3", s.Fixed.Runs, s.ConcurrencyAdaptive.Runs, s.ChunkSizeAdaptive.Runs, s.JointAdaptive.Runs)
	}
	if s.Fixed.MeanAttempts != 8 || s.ConcurrencyAdaptive.MeanAttempts != 8 {
		t.Fatalf("summary mean attempts = %v/%v, want 8/8", s.Fixed.MeanAttempts, s.ConcurrencyAdaptive.MeanAttempts)
	}
}

func TestScenarioChangesServerBehavior(t *testing.T) {
	content := contentPattern(300)
	srv := server.New(content)
	srv.SetRate(2000)
	srv.SetRateAfter(3, 1000000)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	durations := make([]time.Duration, 6)
	for i := range durations {
		start := int64(i * 50)
		durations[i] = timedRangeGet(t, ts.Client(), ts.URL, start, start+49)
	}

	// Requests 1..3 use the slow rate; 4..6 use the fast rate.
	if durations[0] < 10*time.Millisecond {
		t.Fatalf("first request = %v, want >= 10ms at slow rate", durations[0])
	}
	if durations[5] >= durations[0] {
		t.Fatalf("last request = %v, want faster than first %v after rate switch", durations[5], durations[0])
	}
}

func TestInvalidScenario(t *testing.T) {
	cases := map[string]func(*ScenarioConfig){
		"zero file size":    func(c *ScenarioConfig) { c.FileSize = 0 },
		"zero chunk":        func(c *ScenarioConfig) { c.ChunkSize = 0 },
		"zero concurrency":  func(c *ScenarioConfig) { c.InitialConcurrency = 0 },
		"min below one":     func(c *ScenarioConfig) { c.MinConcurrency = 0 },
		"max below min":     func(c *ScenarioConfig) { c.MaxConcurrency = c.MinConcurrency - 1 },
		"zero initial rate": func(c *ScenarioConfig) { c.InitialRate = 0 },
		"zero changed rate": func(c *ScenarioConfig) { c.ChangedRate = 0 },
		"negative switch":   func(c *ScenarioConfig) { c.RateChangeAfterRequests = -1 },
		"negative improve":  func(c *ScenarioConfig) { c.ImprovementThreshold = -0.1 },
		"negative degrade":  func(c *ScenarioConfig) { c.DegradationThreshold = -0.1 },
		"negative retries":  func(c *ScenarioConfig) { c.MaxRetries = -1 },
		"zero repetitions":  func(c *ScenarioConfig) { c.Repetitions = 0 },
		"zero min chunk":    func(c *ScenarioConfig) { c.MinChunkSize = 0 },
		"max chunk below min": func(c *ScenarioConfig) {
			c.MaxChunkSize = c.MinChunkSize - 1
		},
		"initial chunk out of range": func(c *ScenarioConfig) { c.InitialChunkSize = c.MaxChunkSize + 1 },
		"zero chunk step":            func(c *ScenarioConfig) { c.ChunkSizeStep = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testScenario()
			mutate(&cfg)
			if _, err := Run(cfg); !errors.Is(err, ErrInvalidScenario) {
				t.Fatalf("Run = %v, want ErrInvalidScenario", err)
			}
		})
	}
}

func TestNetworkConditionNilMeansNoShaping(t *testing.T) {
	cfg := testScenario() // NetworkCondition nil
	fake := &fakeNetwork{}

	res, err := run(cfg, AllModes(), fake.controller())
	if err != nil {
		t.Fatalf("run = %v", err)
	}
	if calls := fake.recorded(); len(calls) != 0 {
		t.Fatalf("tc invoked without a network condition: %+v", calls)
	}
	for _, mode := range AllModes() {
		for _, r := range res.Runs(mode) {
			if r.NetworkApplied || r.NetworkInterface != "" {
				t.Fatalf("%s run reported network shaping: %+v", mode, r)
			}
		}
	}
}

func TestNetworkConditionAppliedAndCleared(t *testing.T) {
	cfg := testScenario()
	cfg.NetworkInterface = "lo"
	cfg.NetworkCondition = &network.Config{BandwidthBps: 1_000_000}
	fake := &fakeNetwork{}

	res, err := run(cfg, AllModes(), fake.controller())
	if err != nil {
		t.Fatalf("run = %v", err)
	}
	if !fake.applied() {
		t.Fatal("netem qdisc was not applied")
	}
	if !fake.cleared() {
		t.Fatal("netem qdisc was not cleared")
	}
	for _, mode := range AllModes() {
		for _, r := range res.Runs(mode) {
			if !r.NetworkApplied || r.NetworkInterface != "lo" {
				t.Fatalf("%s run network = applied %v iface %q", mode, r.NetworkApplied, r.NetworkInterface)
			}
		}
	}
}

func TestNetworkConditionResolvedFromURL(t *testing.T) {
	cfg := testScenario()
	cfg.NetworkCondition = &network.Config{BandwidthBps: 1_000_000} // no explicit interface
	fake := &fakeNetwork{}

	res, err := run(cfg, AllModes(), fake.controller())
	if err != nil {
		t.Fatalf("run = %v", err)
	}
	if got := res.Fixed[0].NetworkInterface; got != "lo" {
		t.Fatalf("resolved interface = %q, want lo", got)
	}
}

func TestNetworkConditionClearedOnDownloadFailure(t *testing.T) {
	cfg := testScenario()
	cfg.NetworkInterface = "lo"
	cfg.NetworkCondition = &network.Config{BandwidthBps: 1_000_000}
	// Force the download to fail: a 1ns timeout makes every attempt fail and
	// the retry limit is reached.
	cfg.Timeout = time.Nanosecond
	cfg.MaxRetries = 0
	fake := &fakeNetwork{}

	res, err := run(cfg, AllModes(), fake.controller())
	if err != nil {
		t.Fatalf("run = %v", err)
	}
	if !fake.applied() {
		t.Fatal("netem qdisc was not applied")
	}
	if !fake.cleared() {
		t.Fatal("netem qdisc was not cleared after a failed download")
	}
	if res.Fixed[0].Err == "" {
		t.Fatal("expected the fixed run to record a download error")
	}
}

func TestNetworkInvalidInterface(t *testing.T) {
	cfg := testScenario()
	cfg.NetworkInterface = "definitely-not-a-real-interface-xyz"
	cfg.NetworkCondition = &network.Config{BandwidthBps: 1_000_000}
	fake := &fakeNetwork{}

	res, err := run(cfg, AllModes(), fake.controller())
	if err != nil {
		t.Fatalf("run = %v, want nil (unavailable is recorded, not fatal)", err)
	}
	if len(fake.recorded()) != 0 {
		t.Fatalf("tc invoked for an invalid interface: %+v", fake.recorded())
	}
	for _, mode := range AllModes() {
		r := res.Runs(mode)[0]
		if r.NetworkConditionStatus != NetworkStatusUnavailable {
			t.Fatalf("mode %s status = %q, want unavailable", mode, r.NetworkConditionStatus)
		}
		if !strings.Contains(r.Err, "invalid interface") {
			t.Fatalf("mode %s error = %q, want invalid interface", mode, r.Err)
		}
	}
}

func TestNetworkConditionValidation(t *testing.T) {
	cfg := testScenario()
	cfg.NetworkCondition = &network.Config{} // empty is invalid

	if _, err := run(cfg, AllModes(), (&fakeNetwork{}).controller()); !errors.Is(err, ErrInvalidScenario) || !errors.Is(err, network.ErrEmptyConfig) {
		t.Fatalf("run = %v, want ErrInvalidScenario wrapping ErrEmptyConfig", err)
	}
}

func TestServerThrottleAndNetworkConditionSeparate(t *testing.T) {
	cfg := testScenario()
	cfg.InitialRate = 4000
	cfg.ChangedRate = 8000
	cfg.NetworkInterface = "lo"
	cfg.NetworkCondition = &network.Config{Latency: 5 * time.Millisecond, BandwidthBps: 1_000_000}
	fake := &fakeNetwork{}

	res, err := run(cfg, AllModes(), fake.controller())
	if err != nil {
		t.Fatalf("run = %v", err)
	}

	// The server-side throttle still runs: the download completes with the
	// expected number of attempts.
	r := res.Fixed[0]
	if r.Err != "" || r.TotalAttempts != 8 || !r.OutputMatches {
		t.Fatalf("server-throttled run = %+v", r)
	}

	// The network condition is applied through tc, independent of the server
	// rate: the netem args reflect the network config, not InitialRate.
	if !r.NetworkApplied {
		t.Fatal("network condition was not applied")
	}
	var netemArgs string
	for _, c := range fake.recorded() {
		if c.name == "tc" && len(c.args) >= 2 && c.args[1] == "replace" {
			netemArgs = strings.Join(c.args, " ")
		}
	}
	if !strings.Contains(netemArgs, "delay 5ms") || !strings.Contains(netemArgs, "rate 1mbit") {
		t.Fatalf("netem args = %q, want network condition only", netemArgs)
	}
}

func testMatrixConfig(reps int) MatrixConfig {
	base := func() ScenarioConfig {
		cfg := testScenario()
		cfg.Repetitions = reps
		return cfg
	}
	baseline := base()
	latency := base()
	latency.NetworkCondition = &network.Config{Latency: 5 * time.Millisecond}

	return MatrixConfig{
		Repetitions: reps,
		Scenarios: []MatrixScenario{
			{Name: "baseline", Config: baseline},
			{Name: "latency", Config: latency},
		},
	}
}

func runTestMatrix(t *testing.T, reps int) (MatrixResult, *fakeNetwork) {
	t.Helper()
	mc := testMatrixConfig(reps)
	fake := &fakeNetwork{}
	mc.NetworkController = fake.controller()
	res, err := RunMatrix(mc)
	if err != nil {
		t.Fatalf("RunMatrix = %v", err)
	}
	return res, fake
}

func TestDefaultMatrixConfig(t *testing.T) {
	mc := DefaultMatrixConfig()
	if len(mc.Scenarios) != 5 {
		t.Fatalf("scenarios = %d, want 5", len(mc.Scenarios))
	}
	want := map[string]bool{
		"baseline": false, "server-throttle": false, "latency": false, "loss": false, "combined": false,
	}
	byName := map[string]ScenarioConfig{}
	for _, s := range mc.Scenarios {
		if _, ok := want[s.Name]; !ok {
			t.Fatalf("unexpected scenario %q", s.Name)
		}
		want[s.Name] = true
		if err := s.Config.Validate(); err != nil {
			t.Fatalf("scenario %q invalid: %v", s.Name, err)
		}
		byName[s.Name] = s.Config
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing scenario %q", name)
		}
	}

	if c := byName["baseline"]; c.RateChangeAfterRequests != 0 || c.NetworkCondition != nil {
		t.Fatalf("baseline should have no rate change and no netem: %+v", c)
	}
	if c := byName["server-throttle"]; c.RateChangeAfterRequests == 0 || c.InitialRate == c.ChangedRate || c.NetworkCondition != nil {
		t.Fatalf("server-throttle config = %+v", c)
	}
	if c := byName["latency"]; c.NetworkCondition == nil || c.NetworkCondition.Latency == 0 || c.NetworkCondition.LossPercent != 0 {
		t.Fatalf("latency config = %+v", c.NetworkCondition)
	}
	if c := byName["loss"]; c.NetworkCondition == nil || c.NetworkCondition.LossPercent == 0 || c.NetworkCondition.Latency != 0 {
		t.Fatalf("loss config = %+v", c.NetworkCondition)
	}
	if c := byName["combined"]; c.NetworkCondition == nil || c.NetworkCondition.Latency == 0 || c.NetworkCondition.LossPercent == 0 || c.InitialRate >= 4<<20 {
		t.Fatalf("combined config = %+v / %+v", c, c.NetworkCondition)
	}
}

func TestMatrixExpandsToExpectedCombinations(t *testing.T) {
	res, _ := runTestMatrix(t, 3)
	if len(res.Scenarios) != 2 {
		t.Fatalf("scenarios = %d, want 2", len(res.Scenarios))
	}
	for _, s := range res.Scenarios {
		for _, mode := range AllModes() {
			if got := len(s.Runs(mode)); got != 3 {
				t.Fatalf("scenario %q mode %s runs = %d, want 3", s.Name, mode, got)
			}
		}
	}
}

func TestMatrixModesReceiveEquivalentConfig(t *testing.T) {
	res, _ := runTestMatrix(t, 2)
	for _, s := range res.Scenarios {
		for i := 0; i < len(s.Fixed); i++ {
			base := s.Fixed[i]
			for _, mode := range AllModes() {
				r := s.Runs(mode)[i]
				if r.FileSize != base.FileSize || r.TransferredBytes != base.TransferredBytes {
					t.Fatalf("scenario %q run %d mode %s config-derived metrics differ", s.Name, i, mode)
				}
				if r.InitialConcurrency != base.InitialConcurrency || r.InitialChunkSize != base.InitialChunkSize {
					t.Fatalf("scenario %q run %d mode %s initial config differs", s.Name, i, mode)
				}
				if (r.AppliedNetworkCondition == nil) != (base.AppliedNetworkCondition == nil) {
					t.Fatalf("scenario %q run %d mode %s network condition presence differs", s.Name, i, mode)
				}
				if r.Mode != mode {
					t.Fatalf("scenario %q run %d mode = %q, want %q", s.Name, i, r.Mode, mode)
				}
			}
		}
	}
}

func TestMatrixFreshStatePerRun(t *testing.T) {
	res, _ := runTestMatrix(t, 3)
	for _, s := range res.Scenarios {
		for _, mode := range AllModes() {
			for _, r := range s.Runs(mode) {
				if !r.OutputMatches || r.TransferredBytes != 400 {
					t.Fatalf("scenario %q mode %s run not fresh: %+v", s.Name, mode, r)
				}
				// Fixed and concurrency-adaptive keep chunk 50 -> 8 attempts.
				if (mode == ModeFixed || mode == ModeConcurrencyAdaptive) && r.TotalAttempts != 8 {
					t.Fatalf("scenario %q mode %s TotalAttempts = %d, want 8", s.Name, mode, r.TotalAttempts)
				}
			}
		}
	}
}

func TestMatrixAggregateCounts(t *testing.T) {
	res, _ := runTestMatrix(t, 3)
	for _, s := range res.Scenarios {
		for _, mode := range AllModes() {
			a := s.Summary.aggregate(mode)
			if a.Runs != 3 {
				t.Fatalf("scenario %q mode %s summary runs = %d, want 3", s.Name, mode, a.Runs)
			}
			if a.ValidRuns != 3 || a.FailedRuns != 0 {
				t.Fatalf("scenario %q mode %s valid/failed = %d/%d, want 3/0", s.Name, mode, a.ValidRuns, a.FailedRuns)
			}
			if a.MinDuration > a.MedianDuration || a.MedianDuration > a.MaxDuration {
				t.Fatalf("scenario %q mode %s duration order wrong: %v %v %v", s.Name, mode, a.MinDuration, a.MedianDuration, a.MaxDuration)
			}
			if a.MeanThroughput <= 0 {
				t.Fatalf("scenario %q mode %s mean throughput = %v", s.Name, mode, a.MeanThroughput)
			}
		}
		if s.Summary.Fixed.MeanAttempts != 8 || s.Summary.ConcurrencyAdaptive.MeanAttempts != 8 {
			t.Fatalf("scenario %q fixed/concurrency mean attempts = %v/%v, want 8/8", s.Name, s.Summary.Fixed.MeanAttempts, s.Summary.ConcurrencyAdaptive.MeanAttempts)
		}
	}
}

func TestMatrixJSON(t *testing.T) {
	res, _ := runTestMatrix(t, 2)
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	var back MatrixResult
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal = %v", err)
	}
	if len(back.Scenarios) != len(res.Scenarios) {
		t.Fatalf("scenarios = %d, want %d", len(back.Scenarios), len(res.Scenarios))
	}
	if back.Scenarios[0].Name != "baseline" {
		t.Fatalf("first scenario = %q, want baseline", back.Scenarios[0].Name)
	}
	if got := back.Scenarios[0].Fixed[0].Scenario; got != "baseline" {
		t.Fatalf("run scenario = %q, want baseline", got)
	}
	if back.Scenarios[0].Summary.Fixed.Runs != 2 {
		t.Fatalf("summary runs = %d, want 2", back.Scenarios[0].Summary.Fixed.Runs)
	}
	if back.Scenarios[1].Config.NetworkCondition == nil {
		t.Fatal("latency scenario network condition lost in JSON")
	}
}

func TestMatrixTextContainsAllRuns(t *testing.T) {
	res, _ := runTestMatrix(t, 3)
	var buf bytes.Buffer
	res.WriteText(&buf)
	out := buf.String()

	for _, name := range []string{"baseline", "latency"} {
		// 4 modes x (3 run rows + 1 aggregate row) = 16 rows per scenario.
		if got := strings.Count(out, name); got < 16 {
			t.Fatalf("text output mentions %q %d times, want >= 16\n%s", name, got, out)
		}
	}
	for _, mode := range []string{"fixed", "concurrency-adaptive", "chunk-size-adaptive", "joint-adaptive"} {
		if !strings.Contains(out, mode) {
			t.Fatalf("text output missing mode %q\n%s", mode, out)
		}
	}
	if !strings.Contains(out, "Aggregates") {
		t.Fatalf("text output missing aggregates\n%s", out)
	}
}

// failFirstNetwork fails the first tc qdisc replace and succeeds afterwards.
type failFirstNetwork struct {
	mu       sync.Mutex
	calls    []networkCall
	replaces int
}

func (f *failFirstNetwork) run(name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, networkCall{name: name, args: append([]string(nil), args...)})
	if name == "tc" && len(args) >= 2 && args[0] == "qdisc" && args[1] == "replace" {
		f.replaces++
		if f.replaces == 1 {
			return []byte("RTNETLINK answers: Operation not permitted\n"), errors.New("exit status 2")
		}
	}
	return nil, nil
}

func (f *failFirstNetwork) counts() (replaces, dels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.name == "tc" && len(c.args) >= 2 && c.args[0] == "qdisc" {
			switch c.args[1] {
			case "replace":
				replaces++
			case "del":
				dels++
			}
		}
	}
	return replaces, dels
}

func TestMatrixNetworkFailureDoesNotContaminate(t *testing.T) {
	cfgLatency := testScenario()
	cfgLatency.Repetitions = 1
	cfgLatency.NetworkCondition = &network.Config{Latency: 5 * time.Millisecond}

	cfgLoss := testScenario()
	cfgLoss.Repetitions = 1
	cfgLoss.NetworkCondition = &network.Config{LossPercent: 1}

	ff := &failFirstNetwork{}
	mc := MatrixConfig{
		Repetitions:       1,
		NetworkController: &network.Controller{Exec: ff.run},
		Scenarios: []MatrixScenario{
			{Name: "latency", Config: cfgLatency},
			{Name: "loss", Config: cfgLoss},
		},
	}

	res, err := RunMatrix(mc)
	if err != nil {
		t.Fatalf("RunMatrix = %v", err)
	}

	// The first apply fails and is recorded, not fatal.
	if res.Scenarios[0].Fixed[0].Err == "" || !strings.Contains(res.Scenarios[0].Fixed[0].Err, "Operation not permitted") {
		t.Fatalf("first run error = %q, want permission error", res.Scenarios[0].Fixed[0].Err)
	}
	// Later runs in the same and following scenario are unaffected.
	if res.Scenarios[0].ConcurrencyAdaptive[0].Err != "" {
		t.Fatalf("run after failure errored: %q", res.Scenarios[0].ConcurrencyAdaptive[0].Err)
	}
	for _, mode := range []Mode{ModeFixed, ModeConcurrencyAdaptive, ModeChunkSizeAdaptive, ModeJointAdaptive} {
		if res.Scenarios[1].Runs(mode)[0].Err != "" {
			t.Fatalf("later scenario mode %s contaminated: %q", mode, res.Scenarios[1].Runs(mode)[0].Err)
		}
	}

	// Every successful apply was followed by a cleanup: 4 modes x 2 scenarios
	// = 8 applies, the first of which failed, so 7 clears.
	replaces, dels := ff.counts()
	if replaces != 8 {
		t.Fatalf("replace calls = %d, want 8", replaces)
	}
	if dels != 7 {
		t.Fatalf("clear calls = %d, want 7 (all but the failed apply)", dels)
	}
}

func TestNetworkUnavailableExcludedFromAggregates(t *testing.T) {
	cfg := testScenario()
	cfg.Repetitions = 2
	cfg.NetworkInterface = "lo"
	cfg.NetworkCondition = &network.Config{BandwidthBps: 1_000_000}
	ff := &failFirstNetwork{} // first apply fails, the rest succeed

	res, err := run(cfg, AllModes(), &network.Controller{Exec: ff.run})
	if err != nil {
		t.Fatalf("run = %v", err)
	}
	if res.Fixed[0].NetworkConditionStatus != NetworkStatusUnavailable {
		t.Fatalf("fixed[0] status = %q, want unavailable", res.Fixed[0].NetworkConditionStatus)
	}
	if res.Fixed[1].NetworkConditionStatus != NetworkStatusApplied {
		t.Fatalf("fixed[1] status = %q, want applied", res.Fixed[1].NetworkConditionStatus)
	}

	agg := res.Summary().Fixed
	if agg.Runs != 2 || agg.ValidRuns != 1 || agg.FailedRuns != 1 || agg.NetworkUnavailableRuns != 1 {
		t.Fatalf("aggregate = runs %d valid %d failed %d unavailable %d, want 2/1/1/1",
			agg.Runs, agg.ValidRuns, agg.FailedRuns, agg.NetworkUnavailableRuns)
	}
	// The aggregate reflects the valid run only.
	if agg.MeanDuration != res.Fixed[1].Duration {
		t.Fatalf("mean duration = %v, want valid run duration %v", agg.MeanDuration, res.Fixed[1].Duration)
	}
	if agg.MeanAttempts != 8 {
		t.Fatalf("mean attempts = %v, want 8 (from the valid run)", agg.MeanAttempts)
	}
}

func TestRepetitionIndexRecorded(t *testing.T) {
	cfg := testScenario()
	cfg.Repetitions = 3

	res, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	for _, mode := range AllModes() {
		runs := res.Runs(mode)
		if len(runs) != 3 {
			t.Fatalf("mode %s runs = %d, want 3", mode, len(runs))
		}
		for i, r := range runs {
			if r.Repetition != i {
				t.Fatalf("mode %s run %d Repetition = %d", mode, i, r.Repetition)
			}
		}
	}
}

func TestActionCountsRecorded(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if r := res.Fixed[0]; r.ConcurrencyActions != 0 || r.ChunkSizeActions != 0 {
		t.Fatalf("fixed actions = %d/%d, want 0/0", r.ConcurrencyActions, r.ChunkSizeActions)
	}
	if r := res.ConcurrencyAdaptive[0]; r.ConcurrencyActions == 0 || r.ChunkSizeActions != 0 {
		t.Fatalf("concurrency-adaptive actions = %d/%d, want conc>0 chunk=0", r.ConcurrencyActions, r.ChunkSizeActions)
	}
	if r := res.ChunkSizeAdaptive[0]; r.ChunkSizeActions == 0 || r.ConcurrencyActions != 0 {
		t.Fatalf("chunk-size-adaptive actions = %d/%d, want conc=0 chunk>0", r.ConcurrencyActions, r.ChunkSizeActions)
	}
	if r := res.JointAdaptive[0]; r.ConcurrencyActions+r.ChunkSizeActions == 0 {
		t.Fatal("joint-adaptive recorded no actions")
	}
}

func TestAggregateVariabilityComputed(t *testing.T) {
	cfg := testScenario()
	cfg.Repetitions = 4

	res, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	a := res.Summary().ConcurrencyAdaptive
	if a.ValidRuns != 4 {
		t.Fatalf("valid runs = %d, want 4", a.ValidRuns)
	}
	if a.MinDuration > a.MedianDuration || a.MedianDuration > a.MaxDuration {
		t.Fatalf("duration order wrong: %v %v %v", a.MinDuration, a.MedianDuration, a.MaxDuration)
	}
	if a.StdDevDuration < 0 || a.IQRDuration < 0 || a.StdDevThroughput < 0 || a.IQRThroughput < 0 {
		t.Fatalf("negative variability: %+v", a)
	}
	if a.MedianThroughput <= 0 || a.MeanThroughput <= 0 {
		t.Fatalf("throughput stats = mean %v median %v", a.MeanThroughput, a.MedianThroughput)
	}
}

// TestAllModesWithLinUCBIncludesBaseline verifies the canonical ordering: the
// four baseline modes remain first and unchanged, and LinUCB is appended.
func TestAllModesWithLinUCBIncludesBaseline(t *testing.T) {
	base := AllModes()
	if len(base) != 4 {
		t.Fatalf("AllModes() = %d modes, want 4 (default must stay the baseline set)", len(base))
	}
	if IsLinUCBMode(base[0]) {
		t.Fatal("default AllModes must not include LinUCB")
	}
	all := AllModesWithLinUCB()
	if len(all) != 5 {
		t.Fatalf("AllModesWithLinUCB() = %d modes, want 5", len(all))
	}
	for i := range base {
		if all[i] != base[i] {
			t.Fatalf("baseline mode %d changed: %q vs %q", i, all[i], base[i])
		}
	}
	if all[4] != ModeLinUCB || !IsLinUCBMode(all[4]) {
		t.Fatalf("fifth mode = %q, want %q", all[4], ModeLinUCB)
	}
}

// TestRunDefaultExcludesLinUCB verifies the default single-scenario Run does not
// populate the LinUCB result, preserving default behavior.
func TestRunDefaultExcludesLinUCB(t *testing.T) {
	res, err := Run(testScenario())
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if len(res.LinUCB) != 0 {
		t.Fatalf("default Run produced %d LinUCB runs, want 0", len(res.LinUCB))
	}
}

// TestLinUCBModeConstructsAndCompletes verifies the opt-in LinUCB mode runs the
// full chain (LinUCB -> Adapter -> policyRunner -> controllers -> reward ->
// Update) and reconstructs the file correctly, using only deterministic server
// behavior and no tc/netem.
func TestLinUCBModeConstructsAndCompletes(t *testing.T) {
	cfg := testScenario()
	cfg.Repetitions = 2

	res, err := RunSelected(cfg, []Mode{ModeLinUCB})
	if err != nil {
		t.Fatalf("RunSelected(linucb) = %v", err)
	}
	if len(res.LinUCB) != 2 {
		t.Fatalf("len(LinUCB) = %d, want 2", len(res.LinUCB))
	}
	// The four baseline results must remain empty for a LinUCB-only run.
	if len(res.Fixed) != 0 || len(res.ConcurrencyAdaptive) != 0 ||
		len(res.ChunkSizeAdaptive) != 0 || len(res.JointAdaptive) != 0 {
		t.Fatalf("baseline modes populated in a LinUCB-only run")
	}

	for i, r := range res.LinUCB {
		if r.Err != "" {
			t.Fatalf("linucb run %d error: %s", i, r.Err)
		}
		if !r.OutputMatches || !r.NoDuplicateRanges || !r.NoMissingRanges {
			t.Fatalf("linucb run %d incorrect: output=%v dup=%v missing=%v", i, r.OutputMatches, r.NoDuplicateRanges, r.NoMissingRanges)
		}
		if len(r.ConcurrencyTrajectory) == 0 || len(r.ChunkSizeTrajectory) == 0 {
			t.Fatalf("linucb run %d missing trajectories", i)
		}
	}

	agg := res.Summary().LinUCB
	if agg.ValidRuns != 2 || agg.FailedRuns != 0 {
		t.Fatalf("linucb summary valid/failed = %d/%d, want 2/0", agg.ValidRuns, agg.FailedRuns)
	}
}

// TestLinUCBModeInvalidConfig verifies that an invalid LinUCB configuration is
// rejected rather than silently falling back to another policy.
func TestLinUCBModeInvalidConfig(t *testing.T) {
	cfg := testScenario()
	cfg.LinUCBAlpha = math.NaN()
	if _, err := RunSelected(cfg, []Mode{ModeLinUCB}); !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("RunSelected(NaN alpha) = %v, want ErrInvalidScenario", err)
	}

	cfg = testScenario()
	cfg.LinUCBAlpha = -1
	if _, err := RunSelected(cfg, []Mode{ModeLinUCB}); !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("RunSelected(negative alpha) = %v, want ErrInvalidScenario", err)
	}
}

// TestRunSelectedUnknownMode verifies an unknown mode is rejected.
func TestRunSelectedUnknownMode(t *testing.T) {
	if _, err := RunSelected(testScenario(), []Mode{"nope"}); !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("RunSelected(unknown) = %v, want ErrInvalidScenario", err)
	}
	if _, err := RunSelected(testScenario(), nil); !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("RunSelected(nil) = %v, want ErrInvalidScenario", err)
	}
}

// TestMatrixLinUCBMode verifies the matrix supports the opt-in LinUCB mode and
// leaves the four baseline modes intact for the same scenario.
func TestMatrixLinUCBMode(t *testing.T) {
	mc := testMatrixConfig(1)
	mc.Modes = []Mode{ModeFixed, ModeLinUCB}
	fake := &fakeNetwork{}
	mc.NetworkController = fake.controller()

	res, err := RunMatrix(mc)
	if err != nil {
		t.Fatalf("RunMatrix = %v", err)
	}
	for _, s := range res.Scenarios {
		if len(s.Fixed) != 1 {
			t.Fatalf("scenario %q fixed runs = %d, want 1", s.Name, len(s.Fixed))
		}
		if len(s.LinUCB) != 1 {
			t.Fatalf("scenario %q linucb runs = %d, want 1", s.Name, len(s.LinUCB))
		}
		if len(s.ConcurrencyAdaptive) != 0 || len(s.ChunkSizeAdaptive) != 0 || len(s.JointAdaptive) != 0 {
			t.Fatalf("scenario %q ran unselected baseline modes", s.Name)
		}
		if !s.LinUCB[0].OutputMatches {
			t.Fatalf("scenario %q linucb output mismatch", s.Name)
		}
	}
}

func timedRangeGet(t *testing.T, client *http.Client, url string, start, end int64) time.Duration {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	begin := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return time.Since(begin)
}
