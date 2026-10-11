package dqn

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestV5StateParityFixture recomputes every vector in the Python-generated
// parity fixture (python/gen_state_parity_v5.py) with ObservationV5.FeaturesV5
// and requires agreement within the fixture's tolerance. It also pins the
// feature order and the normalization constants.
func TestV5StateParityFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "diagnostics", "dqn_validation_v5", "state_parity_v5.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("parity fixture absent: %v", err)
	}
	var fx struct {
		Tolerance float64 `json:"tolerance"`
		Spec      struct {
			Version         int     `json:"version"`
			MinConcurrency  int     `json:"min_concurrency"`
			MaxConcurrency  int     `json:"max_concurrency"`
			ThroughputScale float64 `json:"throughput_scale"`
			DurationScale   float64 `json:"duration_scale"`
			StalenessScale  float64 `json:"staleness_scale"`
			EWMAAlpha       float64 `json:"ewma_alpha"`
			CounterWindow   int     `json:"counter_window"`
		} `json:"state_spec"`
		FeatureNames []string `json:"feature_names"`
		Cases        []struct {
			Name        string             `json:"name"`
			Observation map[string]float64 `json:"observation"`
			Features    []float64          `json:"features"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	spec := DefaultStateSpecV5()
	if fx.Spec.Version != spec.Version || fx.Spec.MinConcurrency != spec.MinConcurrency ||
		fx.Spec.MaxConcurrency != spec.MaxConcurrency || fx.Spec.ThroughputScale != spec.ThroughputScale ||
		fx.Spec.DurationScale != spec.DurationScale || fx.Spec.StalenessScale != spec.StalenessScale ||
		fx.Spec.EWMAAlpha != spec.EWMAAlpha || fx.Spec.CounterWindow != spec.CounterWindow {
		t.Fatalf("state spec drift: fixture %+v vs Go %+v", fx.Spec, spec)
	}
	names := FeatureNamesV5()
	if len(fx.FeatureNames) != len(names) {
		t.Fatalf("feature count %d vs %d", len(fx.FeatureNames), len(names))
	}
	for i := range names {
		if fx.FeatureNames[i] != names[i] {
			t.Fatalf("feature %d name %q vs %q", i, fx.FeatureNames[i], names[i])
		}
	}
	if len(fx.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}

	sec := func(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
	for _, c := range fx.Cases {
		o := c.Observation
		obs := ObservationV5{
			Concurrency:         int(o["concurrency"]),
			TotalBytes:          int64(o["total_bytes"]),
			DoneBytes:           int64(o["done_bytes"]),
			AggregateThroughput: o["aggregate_throughput"],
			ThroughputEWMA:      o["throughput_ewma"],
			PrevThroughput:      o["prev_throughput"],
			ThroughputVol:       o["throughput_vol"],
			RequestDuration:     sec(o["request_duration_s"]),
			PrevDuration:        sec(o["prev_duration_s"]),
			SinceConcChange:     sec(o["since_conc_change_s"]),
			WindowFailures:      int64(o["window_failures"]),
			WindowRetries:       int64(o["window_retries"]),
			WindowAttempts:      int64(o["window_attempts"]),
			WindowCompleted:     int64(o["window_completed"]),
		}
		got, err := obs.FeaturesV5(spec)
		if err != nil {
			t.Fatalf("%s: FeaturesV5 = %v", c.Name, err)
		}
		if len(got) != len(c.Features) {
			t.Fatalf("%s: len %d vs %d", c.Name, len(got), len(c.Features))
		}
		for i := range got {
			if d := math.Abs(got[i] - c.Features[i]); d > fx.Tolerance {
				t.Errorf("%s: feature %d (%s) go=%.15f py=%.15f diff=%g", c.Name, i, names[i], got[i], c.Features[i], d)
			}
		}
	}
}
