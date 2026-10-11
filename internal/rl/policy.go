package rl

import (
	"sync"

	"adaptivedl/internal/policy"
)

// Policy is the interface a future learner implements. It sees only the
// observable State and returns a finite ActionIndex; it never touches the
// downloader or its controllers directly.
type Policy interface {
	SelectAction(state State) ActionIndex
	Update(state State, action ActionIndex, reward Reward)
}

// Adapter bridges an RL Policy to the existing downloader policy.Policy
// interface, so a learned policy can replace JointPolicy without changing the
// downloader, the policy runner, or the controllers.
//
// Decision path (unchanged):
//
//	request completes -> metrics recorded -> Context -> Adapter.SelectAction
//	  -> StateFromContext -> Policy.SelectAction -> ActionIndex -> policy.Action
//	  -> policyRunner.applyAction -> controller changes -> next allocation.
type Adapter struct {
	policy Policy
	cfg    Config

	mu         sync.Mutex
	lastState  State
	lastAction ActionIndex
	haveLast   bool
}

// NewAdapter validates cfg and returns an Adapter around p.
func NewAdapter(p Policy, cfg Config) (*Adapter, error) {
	if p == nil {
		return nil, ErrInvalidConfig
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Adapter{policy: p, cfg: cfg}, nil
}

// SelectAction converts the downloader Context into a State, asks the RL policy
// for an ActionIndex, and decodes it into the existing policy.Action. On any
// conversion error it falls back to policy.NoChange() (the downloader interface
// cannot return an error).
func (a *Adapter) SelectAction(ctx policy.Context) policy.Action {
	state, err := StateFromContext(ctx, a.cfg)
	if err != nil {
		return policy.NoChange()
	}
	index := a.policy.SelectAction(state)
	action, err := ActionFromIndex(index)
	if err != nil {
		return policy.NoChange()
	}

	a.mu.Lock()
	a.lastState = state
	a.lastAction = index
	a.haveLast = true
	a.mu.Unlock()

	return action
}

// Update converts the downloader Context/action/reward and forwards them to the
// RL policy. The downloader currently passes a placeholder reward of 0; no
// reward is computed here.
func (a *Adapter) Update(ctx policy.Context, action policy.Action, reward float64) {
	state, err := StateFromContext(ctx, a.cfg)
	if err != nil {
		return
	}
	index, err := ActionIndexFromAction(action)
	if err != nil {
		return
	}
	a.policy.Update(state, index, Reward{Value: reward})
}

// Last returns the most recent (State, ActionIndex) produced by SelectAction.
func (a *Adapter) Last() (State, ActionIndex, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastState, a.lastAction, a.haveLast
}

var _ policy.Policy = (*Adapter)(nil)

// AlwaysActionPolicy is a trivial deterministic Policy used only to exercise
// the adapter. It always returns its configured ActionIndex and ignores Update.
// It performs no learning.
type AlwaysActionPolicy struct {
	Action ActionIndex
}

// SelectAction always returns the configured action.
func (p AlwaysActionPolicy) SelectAction(State) ActionIndex { return p.Action }

// Update is a no-op.
func (p AlwaysActionPolicy) Update(State, ActionIndex, Reward) {}

var _ Policy = AlwaysActionPolicy{}
