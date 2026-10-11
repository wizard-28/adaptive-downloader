package dqn

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
)

var (
	// ErrInvalidModel indicates a malformed or unusable model artifact.
	ErrInvalidModel = errors.New("dqn: invalid model")
	// ErrInvalidAction indicates an invalid action index.
	ErrInvalidAction = errors.New("dqn: invalid action")
)

// ModelVersion is the on-disk model format version.
const ModelVersion = 1

// Layer is one fully-connected layer with a bias and an activation name.
type Layer struct {
	// In and Out are the layer dimensions.
	In  int `json:"in"`
	Out int `json:"out"`
	// Weights is row-major [Out][In].
	Weights [][]float64 `json:"weights"`
	// Bias has length Out.
	Bias []float64 `json:"bias"`
	// Activation is "relu" or "linear".
	Activation string `json:"activation"`
}

// Model is a portable, deterministic MLP Q-network artifact. It carries the
// exact state specification it expects, the action mapping, and training
// metadata, so Go inference cannot silently run against a different feature
// layout.
type Model struct {
	ModelVersion int       `json:"model_version"`
	Name         string    `json:"name"`
	StateSpec    StateSpec `json:"state_spec"`
	// FeatureNames is recorded for an explicit ordering check.
	FeatureNames []string `json:"feature_names"`
	// NumActions is the action dimension.
	NumActions int `json:"num_actions"`
	// ActionNames maps index -> human-readable name (for diagnostics).
	ActionNames []string `json:"action_names"`
	// Layers is the feed-forward network.
	Layers []Layer `json:"layers"`
	// Training metadata (informational; never used for inference semantics).
	Training Metadata `json:"training"`
}

// Metadata records how a model was produced. It is informational only.
type Metadata struct {
	Seed          int     `json:"seed"`
	Episodes      int     `json:"episodes"`
	Gamma         float64 `json:"gamma"`
	LearningRate  float64 `json:"learning_rate"`
	HiddenSizes   []int   `json:"hidden_sizes"`
	EpsilonFinal  float64 `json:"epsilon_final"`
	SimulatorSpec string  `json:"simulator_spec"`
	Label         string  `json:"label"`
}

// Validate checks the model's structure, dimensions, and finite weights, and that
// its recorded state/feature ordering matches the current contract.
func (m Model) Validate() error {
	if m.ModelVersion != ModelVersion {
		return fmt.Errorf("%w: unsupported model version %d", ErrInvalidModel, m.ModelVersion)
	}
	if err := m.StateSpec.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidModel, err)
	}
	wantNames := FeatureNames()
	wantDim := NumFeatures()
	switch m.StateSpec.Version {
	case StateSpecVersionV5:
		wantNames = FeatureNamesV5()
		wantDim = NumFeaturesV5()
	case StateSpecVersionV6:
		wantNames = FeatureNamesV6()
		wantDim = NumFeaturesV6()
	}
	if len(m.FeatureNames) != wantDim {
		return fmt.Errorf("%w: model records %d feature names, want %d", ErrInvalidModel, len(m.FeatureNames), wantDim)
	}
	for i := range wantNames {
		if m.FeatureNames[i] != wantNames[i] {
			return fmt.Errorf("%w: feature %d is %q, want %q", ErrStateSpecMismatch, i, m.FeatureNames[i], wantNames[i])
		}
	}
	if m.NumActions < 1 {
		return fmt.Errorf("%w: num actions must be >= 1", ErrInvalidModel)
	}
	if len(m.ActionNames) != m.NumActions {
		return fmt.Errorf("%w: %d action names for %d actions", ErrInvalidModel, len(m.ActionNames), m.NumActions)
	}
	if len(m.Layers) == 0 {
		return fmt.Errorf("%w: no layers", ErrInvalidModel)
	}
	prev := wantDim
	for li, l := range m.Layers {
		if l.In != prev {
			return fmt.Errorf("%w: layer %d input %d != previous output %d", ErrInvalidModel, li, l.In, prev)
		}
		if l.Out < 1 {
			return fmt.Errorf("%w: layer %d output must be >= 1", ErrInvalidModel, li)
		}
		if len(l.Weights) != l.Out {
			return fmt.Errorf("%w: layer %d has %d weight rows, want %d", ErrInvalidModel, li, len(l.Weights), l.Out)
		}
		for r := range l.Weights {
			if len(l.Weights[r]) != l.In {
				return fmt.Errorf("%w: layer %d row %d has %d weights, want %d", ErrInvalidModel, li, r, len(l.Weights[r]), l.In)
			}
			for _, w := range l.Weights[r] {
				if !finite(w) {
					return fmt.Errorf("%w: layer %d has non-finite weight", ErrInvalidModel, li)
				}
			}
		}
		if len(l.Bias) != l.Out {
			return fmt.Errorf("%w: layer %d has %d biases, want %d", ErrInvalidModel, li, len(l.Bias), l.Out)
		}
		for _, b := range l.Bias {
			if !finite(b) {
				return fmt.Errorf("%w: layer %d has non-finite bias", ErrInvalidModel, li)
			}
		}
		switch l.Activation {
		case "relu", "linear":
		default:
			return fmt.Errorf("%w: layer %d has unknown activation %q", ErrInvalidModel, li, l.Activation)
		}
		prev = l.Out
	}
	if prev != m.NumActions {
		return fmt.Errorf("%w: network output %d != num actions %d", ErrInvalidModel, prev, m.NumActions)
	}
	return nil
}

// LoadModel reads and validates a portable model artifact from path.
func LoadModel(path string) (*Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dqn: read model: %w", err)
	}
	return ParseModel(data)
}

// ParseModel validates model JSON bytes and returns the model.
func ParseModel(data []byte) (*Model, error) {
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidModel, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// SelectAction returns the deterministic greedy action (argmax Q) for a v1
// observation. Ties resolve to the lowest action index.
func (m *Model) SelectAction(obs Observation) (int, error) {
	x, err := obs.Features(m.StateSpec)
	if err != nil {
		return -1, err
	}
	return m.greedyFromFeatures(x)
}

// SelectActionV5 returns the deterministic greedy action for a v2 (Candidate v5)
// observation. The model must record state spec version 2.
func (m *Model) SelectActionV5(obs ObservationV5) (int, error) {
	x, err := obs.FeaturesV5(m.StateSpec)
	if err != nil {
		return -1, err
	}
	return m.greedyFromFeatures(x)
}

// SelectActionV6 returns the deterministic greedy action for a v3 (Candidate v6)
// observation. The model must record state spec version 3.
func (m *Model) SelectActionV6(obs ObservationV6) (int, error) {
	x, err := obs.FeaturesV6(m.StateSpec)
	if err != nil {
		return -1, err
	}
	return m.greedyFromFeatures(x)
}

func (m *Model) greedyFromFeatures(x []float64) (int, error) {
	q, err := m.Forward(x)
	if err != nil {
		return -1, err
	}
	best, bestQ := 0, math.Inf(-1)
	for i, v := range q {
		if v > bestQ {
			bestQ = v
			best = i
		}
	}
	return best, nil
}

// Forward runs the deterministic MLP forward pass on a feature vector and
// returns the per-action Q-values. ReLU uses max(0, x).
func (m *Model) Forward(x []float64) ([]float64, error) {
	wantDim := NumFeatures()
	switch m.StateSpec.Version {
	case StateSpecVersionV5:
		wantDim = NumFeaturesV5()
	case StateSpecVersionV6:
		wantDim = NumFeaturesV6()
	}
	if len(x) != wantDim {
		return nil, fmt.Errorf("%w: got %d features, want %d", ErrStateSpecMismatch, len(x), wantDim)
	}
	for i, v := range x {
		if !finite(v) {
			return nil, fmt.Errorf("%w: feature %d not finite", ErrInvalidState, i)
		}
	}
	cur := x
	for li, l := range m.Layers {
		out := make([]float64, l.Out)
		for o := 0; o < l.Out; o++ {
			sum := l.Bias[o]
			row := l.Weights[o]
			for i := 0; i < l.In; i++ {
				sum += row[i] * cur[i]
			}
			if l.Activation == "relu" && sum < 0 {
				sum = 0
			}
			out[o] = sum
		}
		if li == len(m.Layers)-1 {
			for _, v := range out {
				if !finite(v) {
					return nil, fmt.Errorf("%w: non-finite network output", ErrInvalidModel)
				}
			}
		}
		cur = out
	}
	return cur, nil
}
