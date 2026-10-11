package rl

import (
	"errors"
	"fmt"

	"adaptivedl/internal/policy"
)

// ErrInvalidActionIndex indicates an index outside the supported action space.
var ErrInvalidActionIndex = errors.New("rl: invalid action index")

// ErrUnsupportedAction indicates an existing policy.Action that is not part of
// the learning action space (the reserved explicit-target actions).
var ErrUnsupportedAction = errors.New("rl: unsupported action")

// ActionIndex is the finite action space for the learner. It maps directly onto
// the existing policy.ActionKind; no parallel action system is introduced.
//
//	0 -> policy.ActionNone
//	1 -> policy.ActionIncreaseConcurrency
//	2 -> policy.ActionDecreaseConcurrency
//	3 -> policy.ActionIncreaseChunkSize
//	4 -> policy.ActionDecreaseChunkSize
//
// The reserved explicit-target actions (ActionSetConcurrency, ActionSetChunkSize)
// are NOT part of the action space.
type ActionIndex uint8

const (
	ActionNone ActionIndex = iota
	ActionIncreaseConcurrency
	ActionDecreaseConcurrency
	ActionIncreaseChunkSize
	ActionDecreaseChunkSize
)

// NumActions is the size of the supported action space.
const NumActions = 5

// Valid reports whether a is a supported action index.
func (a ActionIndex) Valid() bool { return a < NumActions }

// String renders the action index.
func (a ActionIndex) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionIncreaseConcurrency:
		return "increase-concurrency"
	case ActionDecreaseConcurrency:
		return "decrease-concurrency"
	case ActionIncreaseChunkSize:
		return "increase-chunk-size"
	case ActionDecreaseChunkSize:
		return "decrease-chunk-size"
	default:
		return fmt.Sprintf("invalid(%d)", uint8(a))
	}
}

// ActionFromIndex decodes an ActionIndex into the existing policy.Action.
func ActionFromIndex(a ActionIndex) (policy.Action, error) {
	switch a {
	case ActionNone:
		return policy.NoChange(), nil
	case ActionIncreaseConcurrency:
		return policy.IncreaseConcurrency(), nil
	case ActionDecreaseConcurrency:
		return policy.DecreaseConcurrency(), nil
	case ActionIncreaseChunkSize:
		return policy.IncreaseChunkSize(), nil
	case ActionDecreaseChunkSize:
		return policy.DecreaseChunkSize(), nil
	default:
		return policy.Action{}, fmt.Errorf("%w: %d", ErrInvalidActionIndex, uint8(a))
	}
}

// ActionIndexFromAction encodes an existing policy.Action into an ActionIndex.
// The reserved explicit-target actions return ErrUnsupportedAction.
func ActionIndexFromAction(action policy.Action) (ActionIndex, error) {
	switch action.Kind {
	case policy.ActionNone:
		return ActionNone, nil
	case policy.ActionIncreaseConcurrency:
		return ActionIncreaseConcurrency, nil
	case policy.ActionDecreaseConcurrency:
		return ActionDecreaseConcurrency, nil
	case policy.ActionIncreaseChunkSize:
		return ActionIncreaseChunkSize, nil
	case policy.ActionDecreaseChunkSize:
		return ActionDecreaseChunkSize, nil
	case policy.ActionSetConcurrency, policy.ActionSetChunkSize:
		return 0, fmt.Errorf("%w: kind %d is reserved", ErrUnsupportedAction, action.Kind)
	default:
		return 0, fmt.Errorf("%w: kind %d", ErrUnsupportedAction, action.Kind)
	}
}
