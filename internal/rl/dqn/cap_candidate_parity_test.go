package dqn

// Parity test for the capped-server candidate (testdata/rl_cap_train): every
// fixture case holds a raw observation from the Python simulator together with
// the Python features and greedy action. Go must build the same features and
// pick the same action from the exported model, so the agent evaluated in the
// simulator is the agent that runs live. Skips when the artifact is absent.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type capFixture struct {
	ModelSHA256 string `json:"model_sha256"`
	Cases       []struct {
		Obs struct {
			Concurrency         int     `json:"concurrency"`
			TotalBytes          int64   `json:"total_bytes"`
			DoneBytes           int64   `json:"done_bytes"`
			AggregateThroughput float64 `json:"aggregate_throughput"`
			ThroughputEWMA      float64 `json:"throughput_ewma"`
			PrevThroughput      float64 `json:"prev_throughput"`
			ThroughputVol       float64 `json:"throughput_vol"`
			RequestDurationS    float64 `json:"request_duration_s"`
			PrevDurationS       float64 `json:"prev_duration_s"`
			SinceConcChangeS    float64 `json:"since_conc_change_s"`
			WindowFailures      int64   `json:"window_failures"`
			WindowRetries       int64   `json:"window_retries"`
			WindowAttempts      int64   `json:"window_attempts"`
			WindowCompleted     int64   `json:"window_completed"`
		} `json:"obs"`
		Features []float64 `json:"features"`
		Action   int       `json:"action"`
	} `json:"cases"`
}

func secs(s float64) time.Duration { return time.Duration(math.Round(s * float64(time.Second))) }

func TestCapCandidateMatchesPythonInference(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "rl_cap_train", "model")
	modelPath := filepath.Join(dir, "dqn_cap_candidate.json")
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("capped-server candidate not present at %s (run python/train_cap.py)", modelPath)
	}
	raw, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	var fx capFixture
	fb, err := os.ReadFile(filepath.Join(dir, "parity_fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fb, &fx); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != fx.ModelSHA256 {
		t.Fatalf("model sha256 = %s, fixture was made for %s", got, fx.ModelSHA256)
	}
	m, err := LoadModel(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	// It must load through the same constructor the live harness uses.
	if _, err := NewLiveConcurrencyPolicyV6(m, 512<<20); err != nil {
		t.Fatalf("NewLiveConcurrencyPolicyV6: %v", err)
	}
	if len(fx.Cases) < 100 {
		t.Fatalf("fixture has %d cases, want >= 100", len(fx.Cases))
	}
	for i, c := range fx.Cases {
		o := c.Obs
		obs := ObservationV6{
			Concurrency:         o.Concurrency,
			TotalBytes:          o.TotalBytes,
			DoneBytes:           o.DoneBytes,
			AggregateThroughput: o.AggregateThroughput,
			RequestDuration:     secs(o.RequestDurationS),
			ThroughputEWMA:      o.ThroughputEWMA,
			PrevThroughput:      o.PrevThroughput,
			ThroughputVol:       o.ThroughputVol,
			PrevDuration:        secs(o.PrevDurationS),
			SinceConcChange:     secs(o.SinceConcChangeS),
			WindowFailures:      o.WindowFailures,
			WindowRetries:       o.WindowRetries,
			WindowAttempts:      o.WindowAttempts,
			WindowCompleted:     o.WindowCompleted,
		}
		x, err := obs.FeaturesV6(m.StateSpec)
		if err != nil {
			t.Fatalf("case %d: features: %v", i, err)
		}
		for j := range x {
			if math.Abs(x[j]-c.Features[j]) > 1e-6 {
				t.Fatalf("case %d feature %d (%s): go %.9f python %.9f", i, j, FeatureNamesV6()[j], x[j], c.Features[j])
			}
		}
		a, err := m.SelectActionV6(obs)
		if err != nil {
			t.Fatalf("case %d: select: %v", i, err)
		}
		if a != c.Action {
			t.Fatalf("case %d: go action %d, python action %d", i, a, c.Action)
		}
	}
}
