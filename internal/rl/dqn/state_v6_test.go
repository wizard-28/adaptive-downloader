package dqn

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"adaptivedl/internal/policy"
)

// v6ToyModel builds a valid Candidate v6 (state version 3) concurrency model.
func v6ToyModel(t *testing.T) *Model {
	t.Helper()
	in := NumFeaturesV6()
	hidden := 4
	m := &Model{
		ModelVersion: ModelVersion,
		Name:         "toy-v6",
		StateSpec:    DefaultStateSpecV6(),
		FeatureNames: FeatureNamesV6(),
		NumActions:   NumConcurrencyActions,
		ActionNames:  ConcurrencyActionNames(),
		Layers: []Layer{
			{In: in, Out: hidden, Activation: "relu", Weights: make([][]float64, hidden), Bias: make([]float64, hidden)},
			{In: hidden, Out: NumConcurrencyActions, Activation: "linear", Weights: make([][]float64, NumConcurrencyActions), Bias: make([]float64, NumConcurrencyActions)},
		},
	}
	for o := range m.Layers[0].Weights {
		m.Layers[0].Weights[o] = make([]float64, in)
	}
	for o := range m.Layers[1].Weights {
		m.Layers[1].Weights[o] = make([]float64, hidden)
	}
	m.Layers[1].Weights[1][0] = 10
	m.Layers[1].Bias[1] = 1
	return m
}

func TestV6FeatureDimNamesAndSpec(t *testing.T) {
	if NumFeaturesV6() != 13 {
		t.Fatalf("NumFeaturesV6 = %d, want 13", NumFeaturesV6())
	}
	if StateSpecVersionV6 == StateSpecVersion || StateSpecVersionV6 == StateSpecVersionV5 {
		t.Fatal("v6 state version must differ from v1 and v5")
	}
	spec := DefaultStateSpecV6()
	if spec.Version != StateSpecVersionV6 || spec.MaxConcurrency != 16 || spec.MinConcurrency != 1 {
		t.Fatalf("v6 spec bounds wrong: %+v", spec)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("v6 spec invalid: %v", err)
	}
	for i := range FeatureNamesV5() {
		if FeatureNamesV6()[i] != FeatureNamesV5()[i] {
			t.Fatalf("v6 feature %d differs from v5 layout: %q vs %q", i, FeatureNamesV6()[i], FeatureNamesV5()[i])
		}
	}
}

// TestV6FeatureParityContractExact pins the exact v6 feature vector shared with
// python/tests/test_state_v6.py (same observation as the v5 parity test; only
// the concurrency normalization bound changes, 4 -> 16).
func TestV6FeatureParityContractExact(t *testing.T) {
	obs := ObservationV6{
		Concurrency:         3,
		TotalBytes:          2000,
		DoneBytes:           500,
		AggregateThroughput: 5 * 1024 * 1024,
		ThroughputEWMA:      4 * 1024 * 1024,
		PrevThroughput:      2 * 1024 * 1024,
		ThroughputVol:       1 * 1024 * 1024,
		RequestDuration:     500 * time.Millisecond,
		PrevDuration:        250 * time.Millisecond,
		SinceConcChange:     3 * time.Second,
		WindowFailures:      3,
		WindowRetries:       2,
		WindowAttempts:      7,
		WindowCompleted:     5,
	}
	want := []float64{
		2.0 / 15.0, 0.75, 0.625, 0.5, 0.375, 0.125, 0.5, 0.25,
		3.0 / 7.0, 2.0 / 7.0, 1.0, 0.625, 1.0,
	}
	got, err := obs.FeaturesV6(DefaultStateSpecV6())
	if err != nil {
		t.Fatalf("FeaturesV6 = %v", err)
	}
	for i, w := range want {
		if math.Abs(got[i]-w) > 1e-12 {
			t.Fatalf("feature %d (%s) = %.15f, want %.15f", i, FeatureNamesV6()[i], got[i], w)
		}
	}
}

func TestV6FeaturesRejectV5Spec(t *testing.T) {
	obs := ObservationV6{Concurrency: 1, TotalBytes: 10}
	if _, err := obs.FeaturesV6(DefaultStateSpecV5()); err == nil {
		t.Fatal("FeaturesV6 accepted a v5 spec")
	}
	if _, err := (ObservationV5{}).FeaturesV5(DefaultStateSpecV6()); err == nil {
		t.Fatal("FeaturesV5 accepted a v6 spec")
	}
}

func TestV6ModelValidateRoundTripAndGreedy(t *testing.T) {
	m := v6ToyModel(t)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	obs := ObservationV6{Concurrency: 16, TotalBytes: 100, DoneBytes: 10}
	idx, err := m.SelectActionV6(obs)
	if err != nil {
		t.Fatalf("SelectActionV6 = %v", err)
	}
	if idx != 1 {
		t.Fatalf("greedy action = %d, want 1", idx)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	got, err := ParseModel(data)
	if err != nil {
		t.Fatalf("ParseModel = %v", err)
	}
	idx2, _ := got.SelectActionV6(obs)
	if idx != idx2 {
		t.Fatalf("round-trip action changed: %d vs %d", idx, idx2)
	}
}

// TestV6LiveDecisionCadence locks the live-side timing contract: the live policy
// is called once per completed attempt and produces exactly one decision per
// call, with Features in the v6 contract and progress from successful bytes.
func TestV6LiveDecisionCadence(t *testing.T) {
	m := v6ToyModel(t)
	p, err := NewLiveConcurrencyPolicyV6(m, 16<<20)
	if err != nil {
		t.Fatalf("NewLiveConcurrencyPolicyV6 = %v", err)
	}
	calls := 4
	for i := 1; i <= calls; i++ {
		ctx := policy.Context{
			Concurrency:         1,
			ChunkSize:           1 << 20,
			AggregateThroughput: 4 << 20,
			RequestDuration:     200 * time.Millisecond,
			SuccessfulRequests:  int64(i),
			TotalAttempts:       int64(i),
			BytesTransferred:    1 << 20,
		}
		a := p.SelectAction(ctx)
		if err := a.Validate(); err != nil {
			t.Fatalf("call %d action invalid: %v", i, err)
		}
	}
	log := p.Log()
	if len(log) != calls {
		t.Fatalf("decisions = %d, want one per completed attempt (%d)", len(log), calls)
	}
	for i, d := range log {
		if len(d.Features) != NumFeaturesV6() {
			t.Fatalf("decision %d features = %d, want %d", i, len(d.Features), NumFeaturesV6())
		}
		if want := int64(16<<20) - int64(i+1)*(1<<20); d.RemainingBytes != want {
			t.Fatalf("decision %d remaining = %d, want %d", i, d.RemainingBytes, want)
		}
	}
	// A v6 policy must reject v5 and v1 models.
	if _, err := NewLiveConcurrencyPolicyV6(v5ToyModel(t), 16<<20); err == nil {
		t.Fatal("NewLiveConcurrencyPolicyV6 accepted a v5 model")
	}
	if _, err := NewLiveConcurrencyPolicyV6(toyModel(t), 16<<20); err == nil {
		t.Fatal("NewLiveConcurrencyPolicyV6 accepted a v1 model")
	}
}
