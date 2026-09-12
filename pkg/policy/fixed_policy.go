package policy

import "errors"

type FixedPolicy struct {
	action Action
}

func NewFixed(action Action) (*FixedPolicy, error) {
	if !action.Valid() {
		return nil, errors.New("invalid fixed policy action")
	}

	return &FixedPolicy{
		action: action,
	}, nil
}

func (p *FixedPolicy) SelectAction() Action {
	return p.action
}

func (p *FixedPolicy) Observe(ctx Context) {
	// FixedPolicy does not learn.
}

// Compile time check that FixedPolicy implements Policy
var _ Policy = (*FixedPolicy)(nil)
