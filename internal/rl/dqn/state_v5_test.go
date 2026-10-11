package dqn

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"adaptivedl/internal/policy"
)

// v5ToyModel builds a valid Candidate v5 (state version 2) model.
func v5ToyModel(t *testing.T) *Model {
	t.Helper()
	in := NumFeaturesV5()
	hidden := 4
	m := &Model{
		ModelVersion: ModelVersion,
		Name:         "toy-v5",
		StateSpec:    DefaultStateSpecV5(),
		FeatureNames: FeatureNamesV5(),
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

func TestV5FeatureDimAndNames(t *testing.T) {
	if NumFeaturesV5() != 13 {
		t.Fatalf("NumFeaturesV5 = %d, want 13", NumFeaturesV5())
	}
	if StateSpecVersionV5 == StateSpecVersion {
		t.Fatal("v5 state version must differ from v1")
	}
	if DefaultStateSpecV5().Version != StateSpecVersionV5 {
		t.Fatal("DefaultStateSpecV5 version mismatch")
	}
	if err := DefaultStateSpecV5().Validate(); err != nil {
		t.Fatalf("v5 spec invalid: %v", err)
	}
}

func TestV5FeaturesNormalizedAndDeterministic(t *testing.T) {
	spec := DefaultStateSpecV5()
	obs := ObservationV5{
		Concurrency: 3, TotalBytes: 1000, DoneBytes: 250,
		AggregateThroughput: 4 << 20, RequestDuration: 500 * time.Millisecond,
		ThroughputEWMA: 4 << 20, PrevThroughput: 2 << 20, ThroughputVol: 1 << 20,
		PrevDuration: 250 * time.Millisecond, SinceConcChange: 2 * time.Second,
		WindowFailures: 2, WindowRetries: 1, WindowAttempts: 8, WindowCompleted: 3,
	}
	x, err := obs.FeaturesV5(spec)
	if err != nil {
		t.Fatalf("FeaturesV5 = %v", err)
	}
	if len(x) != NumFeaturesV5() {
		t.Fatalf("len = %d", len(x))
	}
	for i, v := range x {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("feature %d non-finite", i)
		}
		if v < -1 || v > 1 {
			t.Fatalf("feature %d (%s) = %v out of [-1,1]", i, FeatureNamesV5()[i], v)
		}
	}
	if math.Abs(x[0]-2.0/3.0) > 1e-9 {
		t.Fatalf("concurrency_norm = %v, want 2/3", x[0])
	}
	if math.Abs(x[1]-0.75) > 1e-9 {
		t.Fatalf("remaining_bytes_frac = %v, want 0.75", x[1])
	}
	if math.Abs(x[8]-0.25) > 1e-9 || math.Abs(x[9]-0.125) > 1e-9 {
		t.Fatalf("rates = %v/%v", x[8], x[9])
	}
	if math.Abs(x[11]-3.0/8.0) > 1e-9 {
		t.Fatalf("recent_completed_norm = %v, want 3/8", x[11])
	}
	y, _ := obs.FeaturesV5(spec)
	for i := range x {
		if x[i] != y[i] {
			t.Fatal("FeaturesV5 not deterministic")
		}
	}
}

func TestV5FeaturesRejectV1Spec(t *testing.T) {
	obs := ObservationV5{Concurrency: 1, TotalBytes: 10}
	if _, err := obs.FeaturesV5(DefaultStateSpec()); err == nil {
		t.Fatal("FeaturesV5 accepted a v1 spec")
	}
}

func TestV5ModelValidateRoundTripAndGreedy(t *testing.T) {
	m := v5ToyModel(t)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	high := ObservationV5{Concurrency: 4, TotalBytes: 100, DoneBytes: 10}
	idx, err := m.SelectActionV5(high)
	if err != nil {
		t.Fatalf("SelectActionV5 = %v", err)
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
	idx2, _ := got.SelectActionV5(high)
	if idx != idx2 {
		t.Fatalf("round-trip action changed: %d vs %d", idx, idx2)
	}
}

func TestV5ModelRejectsWrongFeatureList(t *testing.T) {
	m := v5ToyModel(t)
	m.FeatureNames = FeatureNames() // v1 names with v2 spec
	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted v1 feature names with a v2 spec")
	}
}

func TestV5LivePolicy(t *testing.T) {
	m := v5ToyModel(t)
	p, err := NewLiveConcurrencyPolicyV5(m, 16<<20)
	if err != nil {
		t.Fatalf("NewLiveConcurrencyPolicyV5 = %v", err)
	}
	ctx := policy.Context{
		Concurrency:         2,
		ChunkSize:           1 << 20,
		AggregateThroughput: 2 << 20,
		RequestDuration:     100 * time.Millisecond,
		SuccessfulRequests:  1,
		TotalAttempts:       1,
		BytesTransferred:    1 << 20,
	}
	a := p.SelectAction(ctx)
	if a.Validate() != nil {
		t.Fatalf("action invalid: %v", a)
	}
	if len(p.Log()) != 1 {
		t.Fatalf("expected 1 logged decision, got %d", len(p.Log()))
	}
	if len(p.Log()[0].Features) != NumFeaturesV5() {
		t.Fatalf("logged features = %d, want %d", len(p.Log()[0].Features), NumFeaturesV5())
	}
	// A v5 policy must reject a v1 model.
	if _, err := NewLiveConcurrencyPolicyV5(toyModel(t), 16<<20); err == nil {
		t.Fatal("NewLiveConcurrencyPolicyV5 accepted a v1 model")
	}
}

// TestV5FeatureParityContractExact pins the exact v5 feature vector shared with
// python/tests/test_state_v5.py (test_features_exact_values_match_go_contract).
// If either side changes the v5 feature math or order, both tests must change
// together and the state spec version must be bumped.
func TestV5FeatureParityContractExact(t *testing.T) {
	obs := ObservationV5{
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
		2.0 / 3.0, 0.75, 0.625, 0.5, 0.375, 0.125, 0.5, 0.25,
		3.0 / 7.0, 2.0 / 7.0, 1.0, 0.625, 1.0,
	}
	got, err := obs.FeaturesV5(DefaultStateSpecV5())
	if err != nil {
		t.Fatalf("FeaturesV5 = %v", err)
	}
	for i, w := range want {
		if math.Abs(got[i]-w) > 1e-12 {
			t.Fatalf("feature %d (%s) = %.15f, want %.15f", i, FeatureNamesV5()[i], got[i], w)
		}
	}
}

// TestV5LivePolicyProgressFromSuccessBytes checks that live progress is the sum
// of successful attempts' bytes, not successes x last-bytes. A failed attempt
// with a large error body must not move progress backwards.
func TestV5LivePolicyProgressFromSuccessBytes(t *testing.T) {
	m := v5ToyModel(t)
	p, err := NewLiveConcurrencyPolicyV5(m, 16<<20)
	if err != nil {
		t.Fatalf("NewLiveConcurrencyPolicyV5 = %v", err)
	}
	base := policy.Context{
		Concurrency:         1,
		ChunkSize:           1 << 20,
		AggregateThroughput: 4 << 20,
		RequestDuration:     100 * time.Millisecond,
	}

	success1 := base
	success1.SuccessfulRequests = 1
	success1.TotalAttempts = 1
	success1.BytesTransferred = 1 << 20
	p.SelectAction(success1)

	failed := base
	failed.SuccessfulRequests = 1
	failed.FailedRequests = 1
	failed.Retries = 1
	failed.TotalAttempts = 2
	failed.BytesTransferred = 512 << 10
	p.SelectAction(failed)

	success2 := base
	success2.SuccessfulRequests = 2
	success2.TotalAttempts = 3
	success2.BytesTransferred = 1 << 20
	p.SelectAction(success2)

	log := p.Log()
	if len(log) != 3 {
		t.Fatalf("decisions = %d, want 3", len(log))
	}
	want := []int64{15 << 20, 15 << 20, 14 << 20}
	for i, w := range want {
		if log[i].RemainingBytes != w {
			t.Fatalf("decision %d remaining = %d, want %d", i, log[i].RemainingBytes, w)
		}
	}
	if got := log[2].Features[1]; math.Abs(got-14.0/16.0) > 1e-12 {
		t.Fatalf("remaining_bytes_frac = %v, want 0.875", got)
	}
}
