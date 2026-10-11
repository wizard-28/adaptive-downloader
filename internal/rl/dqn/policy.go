package dqn

import (
	"fmt"

	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

// ActionNames maps the DQN action index to its canonical name. The mapping is
// identical to the existing rl.ActionIndex order; no parallel action system is
// introduced. It is the action space of the historical v1/v2/v3 models.
func ActionNames() []string {
	return []string{
		"none",
		"increase-concurrency",
		"decrease-concurrency",
		"increase-chunk-size",
		"decrease-chunk-size",
	}
}

// ConcurrencyActionNames is the concurrency-only action space of Candidate v4:
// chunk size is fixed at 1 MiB and is not a control variable.
//
//	0 -> none
//	1 -> increase-concurrency
//	2 -> decrease-concurrency
func ConcurrencyActionNames() []string {
	return []string{
		"none",
		"increase-concurrency",
		"decrease-concurrency",
	}
}

// NumConcurrencyActions is the size of the concurrency-only action space.
const NumConcurrencyActions = 3

// ConcurrencyActionToIndex maps a policy.Action to the concurrency-only space.
// Chunk-size and reserved Set actions are unsupported here.
func ConcurrencyActionToIndex(a policy.Action) (int, error) {
	switch a.Kind {
	case policy.ActionNone:
		return 0, nil
	case policy.ActionIncreaseConcurrency:
		return 1, nil
	case policy.ActionDecreaseConcurrency:
		return 2, nil
	default:
		return -1, fmt.Errorf("%w: kind %d not in concurrency-only space", ErrInvalidAction, a.Kind)
	}
}

// ConcurrencyIndexToAction decodes a concurrency-only action index.
func ConcurrencyIndexToAction(i int) (policy.Action, error) {
	switch i {
	case 0:
		return policy.NoChange(), nil
	case 1:
		return policy.IncreaseConcurrency(), nil
	case 2:
		return policy.DecreaseConcurrency(), nil
	default:
		return policy.Action{}, fmt.Errorf("%w: %d", ErrInvalidAction, i)
	}
}

// ActionToIndex and IndexToAction reuse the existing rl.ActionIndex contract.
func ActionToIndex(a policy.Action) (int, error) {
	idx, err := rl.ActionIndexFromAction(a)
	if err != nil {
		return -1, fmt.Errorf("%w: %v", ErrInvalidAction, err)
	}
	return int(idx), nil
}

// IndexToAction decodes a DQN action index into the existing policy.Action.
func IndexToAction(i int) (policy.Action, error) {
	if i < 0 || i >= rl.NumActions {
		return policy.Action{}, fmt.Errorf("%w: %d", ErrInvalidAction, i)
	}
	return rl.ActionFromIndex(rl.ActionIndex(i))
}

// Policy adapts a loaded DQN Model to the existing policy.Policy interface. It
// uses deterministic greedy inference (epsilon = 0): no random exploration
// occurs in production. The latest (observation, action) is retained so a
// caller can compute the next state/reward and update its trackers.
type Policy struct {
	model *Model

	lastObs    Observation
	lastAction int
	haveLast   bool
}

// NewPolicy returns an adapter around a validated model.
func NewPolicy(m *Model) (*Policy, error) {
	if m == nil {
		return nil, ErrInvalidModel
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &Policy{model: m}, nil
}

// SelectAction returns the greedy action for the observation.
func (p *Policy) SelectAction(obs Observation) (policy.Action, error) {
	idx, err := p.model.SelectAction(obs)
	if err != nil {
		return policy.Action{}, err
	}
	action, err := IndexToAction(idx)
	if err != nil {
		return policy.Action{}, err
	}
	p.lastObs, p.lastAction, p.haveLast = obs, idx, true
	return action, nil
}

// Last returns the most recent (observation, actionIndex).
func (p *Policy) Last() (Observation, int, bool) { return p.lastObs, p.lastAction, p.haveLast }
