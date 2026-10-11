package main

import (
	"math"
	"testing"
	"time"

	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl/dqn"
)

func v5DispatchTestModel() *dqn.Model {
	in := dqn.NumFeaturesV5()
	hidden := 2
	m := &dqn.Model{
		ModelVersion: dqn.ModelVersion,
		Name:         "dispatch-test-v5",
		StateSpec:    dqn.DefaultStateSpecV5(),
		FeatureNames: dqn.FeatureNamesV5(),
		NumActions:   dqn.NumConcurrencyActions,
		ActionNames:  dqn.ConcurrencyActionNames(),
		Layers: []dqn.Layer{
			{
				In: in, Out: hidden, Activation: "relu",
				Weights: make([][]float64, hidden), Bias: make([]float64, hidden),
			},
			{
				In: hidden, Out: dqn.NumConcurrencyActions, Activation: "linear",
				Weights: make([][]float64, dqn.NumConcurrencyActions),
				Bias:    make([]float64, dqn.NumConcurrencyActions),
			},
		},
	}
	for o := range m.Layers[0].Weights {
		m.Layers[0].Weights[o] = make([]float64, in)
	}
	for o := range m.Layers[1].Weights {
		m.Layers[1].Weights[o] = make([]float64, hidden)
	}
	return m
}

// TestNewLivePolicyForModelDispatchesV5 guards a silent-layout hazard: a
// 3-action Candidate v5 model must use the v5 observation adapter. The v1
// concurrency adapter also accepts a 13-feature input, so without the version
// check it would run without error but with the wrong feature semantics.
func TestNewLivePolicyForModelDispatchesV5(t *testing.T) {
	m := v5DispatchTestModel()
	if err := m.Validate(); err != nil {
		t.Fatalf("test model invalid: %v", err)
	}
	p, err := newLivePolicyForModel(m, 16<<20)
	if err != nil {
		t.Fatalf("newLivePolicyForModel = %v", err)
	}
	a := p.SelectAction(policy.Context{
		Concurrency:         1,
		ChunkSize:           1 << 20,
		AggregateThroughput: 4 << 20,
		RequestDuration:     100 * time.Millisecond,
		SuccessfulRequests:  1,
		TotalAttempts:       1,
		BytesTransferred:    1 << 20,
	})
	if err := a.Validate(); err != nil {
		t.Fatalf("action invalid: %v", err)
	}
	d := p.Log()[0]
	// v5 feature 1 is remaining_bytes_frac = 15/16. The v1 layout would put
	// chunk_size_norm there (0 for a 1 MiB min==max spec).
	if got := d.Features[1]; math.Abs(got-0.9375) > 1e-12 {
		t.Fatalf("feature[1] = %v, want 0.9375 (v5 adapter not selected)", got)
	}
}
