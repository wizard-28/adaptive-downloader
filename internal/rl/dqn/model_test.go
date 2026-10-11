package dqn

import (
	"encoding/json"
	"math"
	"testing"
)

// toyModel builds a small valid model with hand-set weights.
func toyModel(t *testing.T) *Model {
	t.Helper()
	in := NumFeatures()
	hidden := 4
	m := &Model{
		ModelVersion: ModelVersion,
		Name:         "toy",
		StateSpec:    DefaultStateSpec(),
		FeatureNames: FeatureNames(),
		NumActions:   NumActions(),
		ActionNames:  ActionNames(),
		Layers: []Layer{
			{In: in, Out: hidden, Activation: "relu", Weights: make([][]float64, hidden), Bias: make([]float64, hidden)},
			{In: hidden, Out: NumActions(), Activation: "linear", Weights: make([][]float64, NumActions()), Bias: make([]float64, NumActions())},
		},
	}
	for o := range m.Layers[0].Weights {
		m.Layers[0].Weights[o] = make([]float64, in)
	}
	for o := range m.Layers[1].Weights {
		m.Layers[1].Weights[o] = make([]float64, hidden)
	}
	// Action 1 (increase concurrency) responds to feature 0 (concurrency).
	m.Layers[1].Weights[1][0] = 10
	m.Layers[1].Bias[1] = 1
	return m
}

func TestModelForwardAndGreedy(t *testing.T) {
	m := toyModel(t)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	// High concurrency raises action 1's Q.
	high := Observation{Concurrency: 4, ChunkSize: DefaultStateSpec().MinChunkSize, TotalBytes: 100, DoneBytes: 10}
	idx, err := m.SelectAction(high)
	if err != nil {
		t.Fatalf("SelectAction = %v", err)
	}
	if idx != 1 {
		t.Fatalf("greedy action = %d, want 1", idx)
	}
	// Deterministic.
	idx2, _ := m.SelectAction(high)
	if idx != idx2 {
		t.Fatal("greedy inference not deterministic")
	}
	q, err := m.Forward(mustFeatures(t, high))
	if err != nil {
		t.Fatalf("Forward = %v", err)
	}
	if len(q) != NumActions() {
		t.Fatalf("q length = %d", len(q))
	}
	for _, v := range q {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatal("non-finite Q")
		}
	}
}

func mustFeatures(t *testing.T, o Observation) []float64 {
	t.Helper()
	x, err := o.Features(DefaultStateSpec())
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestModelRejectsMalformed(t *testing.T) {
	// Wrong feature order.
	m := toyModel(t)
	m.FeatureNames[0] = "wrong"
	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted wrong feature order")
	}
	// Non-finite weight.
	m = toyModel(t)
	m.Layers[0].Weights[0][0] = math.Inf(1)
	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted non-finite weight")
	}
	// Dimension mismatch.
	m = toyModel(t)
	m.Layers[0].In = NumFeatures() + 1
	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted dimension mismatch")
	}
	// Output mismatch.
	m = toyModel(t)
	m.Layers[1].Out = NumActions() + 1
	m.Layers[1].Weights = append(m.Layers[1].Weights, make([]float64, 4))
	m.Layers[1].Bias = append(m.Layers[1].Bias, 0)
	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted output mismatch")
	}
}

func TestModelRoundTrip(t *testing.T) {
	m := toyModel(t)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	got, err := ParseModel(data)
	if err != nil {
		t.Fatalf("ParseModel = %v", err)
	}
	obs := Observation{Concurrency: 4, ChunkSize: DefaultStateSpec().MinChunkSize, TotalBytes: 100, DoneBytes: 10}
	a1, _ := m.SelectAction(obs)
	a2, _ := got.SelectAction(obs)
	if a1 != a2 {
		t.Fatalf("round-trip action changed: %d vs %d", a1, a2)
	}
	q1, _ := m.Forward(mustFeatures(t, obs))
	q2, _ := got.Forward(mustFeatures(t, obs))
	for i := range q1 {
		if q1[i] != q2[i] {
			t.Fatalf("round-trip Q[%d] changed: %v vs %v", i, q1[i], q2[i])
		}
	}
}

func TestVariedStateSelection(t *testing.T) {
	// Missing features called out explicitly.
	if len(FeatureNames()) != 13 {
		t.Fatalf("feature count changed: %d", len(FeatureNames()))
	}
}
