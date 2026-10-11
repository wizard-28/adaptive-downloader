package rl

import (
	"errors"
	"math"
	"testing"
	"time"

	"adaptivedl/internal/policy"
)

func testConfig() Config {
	return Config{
		MinConcurrency:     1,
		MaxConcurrency:     4,
		MinChunkSize:       16,
		MaxChunkSize:       128,
		MaxThroughput:      1000,
		MaxRequestDuration: time.Second,
		CounterScale:       100,
	}
}

func TestStateFromContextNormal(t *testing.T) {
	ctx := policy.Context{
		Concurrency:         2,
		ChunkSize:           32,
		Throughput:          500,
		AggregateThroughput: 800,
		RequestDuration:     250 * time.Millisecond,
		BytesTransferred:    40,
		SuccessfulRequests:  5,
		FailedRequests:      1,
		Retries:             1,
		TotalAttempts:       6,
	}
	s, err := StateFromContext(ctx, testConfig())
	if err != nil {
		t.Fatalf("StateFromContext = %v", err)
	}
	if s.Concurrency != 2 || s.ChunkSize != 32 || s.Throughput != 500 || s.AggregateThroughput != 800 {
		t.Fatalf("state = %+v", s)
	}
	if s.RequestDuration != 250*time.Millisecond || s.BytesTransferred != 40 {
		t.Fatalf("state = %+v", s)
	}
	if s.SuccessfulRequests != 5 || s.FailedRequests != 1 || s.Retries != 1 || s.TotalAttempts != 6 {
		t.Fatalf("state = %+v", s)
	}
}

func TestStateFromContextZeroValues(t *testing.T) {
	s, err := StateFromContext(policy.Context{}, testConfig())
	if err != nil {
		t.Fatalf("StateFromContext = %v", err)
	}
	if s != (State{}) {
		t.Fatalf("zero context -> %+v, want zero state", s)
	}
	features, err := s.Features(testConfig())
	if err != nil {
		t.Fatalf("Features = %v", err)
	}
	for i, f := range features {
		if math.IsNaN(f) || f < 0 || f > 1 {
			t.Fatalf("feature %d = %v, want finite in [0,1]", i, f)
		}
	}
}

func TestStateFromContextBoundaries(t *testing.T) {
	cfg := testConfig()
	cases := []struct {
		name string
		ctx  policy.Context
		want []float64
	}{
		{
			"concurrency at min",
			policy.Context{Concurrency: 1, ChunkSize: 16},
			[]float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			"concurrency at max, chunk at max",
			policy.Context{Concurrency: 4, ChunkSize: 128},
			[]float64{1, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			"midpoints",
			policy.Context{
				Concurrency: 2, ChunkSize: 72, Throughput: 500, AggregateThroughput: 250,
				RequestDuration: 500 * time.Millisecond, BytesTransferred: 50,
				SuccessfulRequests: 50,
			},
			[]float64{1.0 / 3.0, 0.5, 0.5, 0.25, 0.5, 0.5, 0.5, 0, 0, 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := StateFromContext(tc.ctx, cfg)
			if err != nil {
				t.Fatalf("StateFromContext = %v", err)
			}
			got, err := s.Features(cfg)
			if err != nil {
				t.Fatalf("Features = %v", err)
			}
			for i := range tc.want {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Fatalf("feature %d = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestFeaturesClampAboveMax(t *testing.T) {
	cfg := testConfig()
	ctx := policy.Context{
		Concurrency: 99, ChunkSize: 9999, Throughput: 1e9, AggregateThroughput: 1e9,
		RequestDuration: 10 * time.Second, BytesTransferred: 1e6,
		SuccessfulRequests: 1e6, FailedRequests: 1e6, Retries: 1e6, TotalAttempts: 1e6,
	}
	s, err := StateFromContext(ctx, cfg)
	if err != nil {
		t.Fatalf("StateFromContext = %v", err)
	}
	features, err := s.Features(cfg)
	if err != nil {
		t.Fatalf("Features = %v", err)
	}
	for i, f := range features {
		if f != 1 {
			t.Fatalf("feature %d = %v, want clamped to 1", i, f)
		}
	}
}

func TestFeaturesZeroRangeNoDivideByZero(t *testing.T) {
	cfg := testConfig()
	cfg.MinConcurrency = 2
	cfg.MaxConcurrency = 2
	cfg.MinChunkSize = 64
	cfg.MaxChunkSize = 64
	s, err := StateFromContext(policy.Context{Concurrency: 2, ChunkSize: 64}, cfg)
	if err != nil {
		t.Fatalf("StateFromContext = %v", err)
	}
	features, err := s.Features(cfg)
	if err != nil {
		t.Fatalf("Features = %v", err)
	}
	if features[0] != 0 || features[1] != 0 {
		t.Fatalf("zero-range features = %v, %v; want 0", features[0], features[1])
	}
}

func TestStateFromContextInvalid(t *testing.T) {
	cfg := testConfig()
	cases := map[string]policy.Context{
		"negative concurrency": {Concurrency: -1},
		"negative chunk":       {ChunkSize: -1},
		"negative throughput":  {Throughput: -1},
		"nan throughput":       {Throughput: math.NaN()},
		"inf throughput":       {Throughput: math.Inf(1)},
		"negative aggregate":   {AggregateThroughput: -1},
		"nan aggregate":        {AggregateThroughput: math.NaN()},
		"inf aggregate":        {AggregateThroughput: math.Inf(1)},
		"negative duration":    {RequestDuration: -time.Second},
		"negative bytes":       {BytesTransferred: -1},
		"negative successes":   {SuccessfulRequests: -1},
		"negative failures":    {FailedRequests: -1},
		"negative retries":     {Retries: -1},
		"negative attempts":    {TotalAttempts: -1},
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := StateFromContext(ctx, cfg); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("StateFromContext = %v, want ErrInvalidState", err)
			}
		})
	}
}

func TestStateFromContextInvalidConfig(t *testing.T) {
	cases := map[string]func(*Config){
		"min concurrency zero": func(c *Config) { c.MinConcurrency = 0 },
		"max below min":        func(c *Config) { c.MaxConcurrency = c.MinConcurrency - 1 },
		"min chunk zero":       func(c *Config) { c.MinChunkSize = 0 },
		"max chunk below min":  func(c *Config) { c.MaxChunkSize = c.MinChunkSize - 1 },
		"zero max throughput":  func(c *Config) { c.MaxThroughput = 0 },
		"nan max throughput":   func(c *Config) { c.MaxThroughput = math.NaN() },
		"zero max duration":    func(c *Config) { c.MaxRequestDuration = 0 },
		"zero counter scale":   func(c *Config) { c.CounterScale = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			mutate(&cfg)
			if _, err := StateFromContext(policy.Context{}, cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("StateFromContext = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestFeaturesDeterministic(t *testing.T) {
	cfg := testConfig()
	ctx := policy.Context{Concurrency: 3, ChunkSize: 48, Throughput: 250, RequestDuration: 100 * time.Millisecond}
	s1, _ := StateFromContext(ctx, cfg)
	s2, _ := StateFromContext(ctx, cfg)
	f1, err := s1.Features(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := s2.Features(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := range f1 {
		if f1[i] != f2[i] {
			t.Fatalf("feature %d not deterministic: %v vs %v", i, f1[i], f2[i])
		}
	}
}

func TestFeatureNamesOrder(t *testing.T) {
	want := []string{
		"concurrency",
		"chunk_size",
		"throughput",
		"aggregate_throughput",
		"request_duration",
		"bytes_transferred",
		"successful_requests",
		"failed_requests",
		"retries",
		"total_attempts",
	}
	got := FeatureNames()
	if len(got) != len(want) {
		t.Fatalf("FeatureNames length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FeatureNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestAggregateThroughputFeature verifies the aggregate-throughput feature is
// normalized independently of the per-request throughput feature.
func TestAggregateThroughputFeature(t *testing.T) {
	cfg := testConfig() // MaxThroughput = 1000
	s, err := StateFromContext(policy.Context{Throughput: 500, AggregateThroughput: 250}, cfg)
	if err != nil {
		t.Fatalf("StateFromContext = %v", err)
	}
	f, err := s.Features(cfg)
	if err != nil {
		t.Fatalf("Features = %v", err)
	}
	if f[2] != 0.5 {
		t.Fatalf("throughput feature = %v, want 0.5", f[2])
	}
	if f[3] != 0.25 {
		t.Fatalf("aggregate_throughput feature = %v, want 0.25", f[3])
	}
}

func TestFeatureNamesMatchFeatures(t *testing.T) {
	cfg := testConfig()
	s, _ := StateFromContext(policy.Context{}, cfg)
	features, err := s.Features(cfg)
	if err != nil {
		t.Fatal(err)
	}
	names := FeatureNames()
	if len(names) != len(features) {
		t.Fatalf("names %d != features %d", len(names), len(features))
	}
}
