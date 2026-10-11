package dqn

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestFeatureDimAndNames(t *testing.T) {
	if NumFeatures() != 13 {
		t.Fatalf("NumFeatures = %d, want 13", NumFeatures())
	}
	if len(FeatureNames()) != NumFeatures() {
		t.Fatalf("names %d != features %d", len(FeatureNames()), NumFeatures())
	}
}

func TestStateNormalizationAndDeterminism(t *testing.T) {
	spec := DefaultStateSpec()
	obs := Observation{
		Concurrency: 3, ChunkSize: 65536, TotalBytes: 1000, DoneBytes: 250,
		AggregateThroughput: 4 << 20, RequestDuration: 500 * time.Millisecond,
		ThroughputEWMA: 4 << 20, PrevThroughput: 2 << 20, ThroughputVol: 1 << 20,
		PrevDuration:    250 * time.Millisecond,
		SinceConcChange: 2 * time.Second, SinceChunkChange: time.Second,
		WindowFailures: 2, WindowRetries: 1, WindowAttempts: 8,
	}
	x, err := obs.Features(spec)
	if err != nil {
		t.Fatalf("Features = %v", err)
	}
	if len(x) != NumFeatures() {
		t.Fatalf("len = %d", len(x))
	}
	for i, v := range x {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("feature %d non-finite", i)
		}
		if v < -1 || v > 1 {
			t.Fatalf("feature %d (%s) = %v out of [-1,1]", i, FeatureNames()[i], v)
		}
	}
	// concurrency (1..4): 3 -> 2/3
	if math.Abs(x[0]-2.0/3.0) > 1e-9 {
		t.Fatalf("concurrency_norm = %v, want 2/3", x[0])
	}
	// remaining fraction 750/1000 = 0.75
	if math.Abs(x[2]-0.75) > 1e-9 {
		t.Fatalf("remaining_bytes_frac = %v, want 0.75", x[2])
	}
	// failure rate 2/8, retry rate 1/8
	if math.Abs(x[9]-0.25) > 1e-9 || math.Abs(x[10]-0.125) > 1e-9 {
		t.Fatalf("rates = %v/%v", x[9], x[10])
	}
	y, _ := obs.Features(spec)
	if !reflect.DeepEqual(x, y) {
		t.Fatal("Features not deterministic")
	}
}

// TestRemainingBytesInformativeAcrossEpisode checks the progress feature stays
// informative (does not saturate like cumulative bytes).
func TestRemainingBytesInformativeAcrossEpisode(t *testing.T) {
	spec := DefaultStateSpec()
	seen := map[float64]bool{}
	for done := int64(0); done <= 1000; done += 100 {
		obs := Observation{Concurrency: 1, ChunkSize: spec.MinChunkSize, TotalBytes: 1000, DoneBytes: done}
		x, err := obs.Features(spec)
		if err != nil {
			t.Fatal(err)
		}
		seen[math.Round(x[2]*1000)/1000] = true
	}
	if len(seen) < 10 {
		t.Fatalf("remaining fraction took only %d distinct values", len(seen))
	}
	// completion -> 0
	obs := Observation{Concurrency: 1, ChunkSize: spec.MinChunkSize, TotalBytes: 1000, DoneBytes: 1000}
	x, _ := obs.Features(spec)
	if x[2] != 0 {
		t.Fatalf("remaining at completion = %v, want 0", x[2])
	}
}

func TestStateRejectsMalformed(t *testing.T) {
	spec := DefaultStateSpec()
	cases := map[string]Observation{
		"negative concurrency": {Concurrency: -1, TotalBytes: 10, ChunkSize: 1},
		"done exceeds total":   {Concurrency: 1, ChunkSize: 1, TotalBytes: 10, DoneBytes: 11},
		"nan throughput":       {Concurrency: 1, ChunkSize: 1, AggregateThroughput: math.NaN()},
		"negative duration":    {Concurrency: 1, ChunkSize: 1, RequestDuration: -time.Second},
		"negative window":      {Concurrency: 1, ChunkSize: 1, WindowFailures: -1},
	}
	for name, obs := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := obs.Features(spec); err == nil {
				t.Fatalf("Features(%s) = nil, want error", name)
			}
		})
	}
}

func TestStateSpecValidation(t *testing.T) {
	cases := map[string]func(*StateSpec){
		"bad version":     func(s *StateSpec) { s.Version = 99 },
		"zero scale":      func(s *StateSpec) { s.ThroughputScale = 0 },
		"bad ewma":        func(s *StateSpec) { s.EWMAAlpha = 0 },
		"zero window":     func(s *StateSpec) { s.CounterWindow = 0 },
		"bad conc bounds": func(s *StateSpec) { s.MaxConcurrency = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := DefaultStateSpec()
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}
