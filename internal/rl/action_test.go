package rl

import (
	"errors"
	"strings"
	"testing"

	"adaptivedl/internal/policy"
)

func TestActionFromIndexValid(t *testing.T) {
	cases := map[ActionIndex]policy.ActionKind{
		ActionNone:                policy.ActionNone,
		ActionIncreaseConcurrency: policy.ActionIncreaseConcurrency,
		ActionDecreaseConcurrency: policy.ActionDecreaseConcurrency,
		ActionIncreaseChunkSize:   policy.ActionIncreaseChunkSize,
		ActionDecreaseChunkSize:   policy.ActionDecreaseChunkSize,
	}
	for index, wantKind := range cases {
		action, err := ActionFromIndex(index)
		if err != nil {
			t.Fatalf("ActionFromIndex(%d) = %v", index, err)
		}
		if action.Kind != wantKind {
			t.Fatalf("ActionFromIndex(%d).Kind = %d, want %d", index, action.Kind, wantKind)
		}
	}
}

func TestActionFromIndexInvalid(t *testing.T) {
	for _, index := range []ActionIndex{NumActions, 200, 255} {
		if _, err := ActionFromIndex(index); !errors.Is(err, ErrInvalidActionIndex) {
			t.Fatalf("ActionFromIndex(%d) = %v, want ErrInvalidActionIndex", index, err)
		}
	}
}

func TestActionIndexFromActionValid(t *testing.T) {
	cases := map[policy.Action]ActionIndex{
		policy.NoChange():            ActionNone,
		policy.IncreaseConcurrency(): ActionIncreaseConcurrency,
		policy.DecreaseConcurrency(): ActionDecreaseConcurrency,
		policy.IncreaseChunkSize():   ActionIncreaseChunkSize,
		policy.DecreaseChunkSize():   ActionDecreaseChunkSize,
	}
	for action, want := range cases {
		got, err := ActionIndexFromAction(action)
		if err != nil {
			t.Fatalf("ActionIndexFromAction(%v) = %v", action, err)
		}
		if got != want {
			t.Fatalf("ActionIndexFromAction(%v) = %d, want %d", action, got, want)
		}
	}
}

func TestActionIndexFromActionUnsupported(t *testing.T) {
	for _, action := range []policy.Action{policy.SetConcurrency(4), policy.SetChunkSize(64)} {
		if _, err := ActionIndexFromAction(action); !errors.Is(err, ErrUnsupportedAction) {
			t.Fatalf("ActionIndexFromAction(%v) = %v, want ErrUnsupportedAction", action, err)
		}
	}
}

func TestActionRoundTrip(t *testing.T) {
	for index := ActionIndex(0); index < NumActions; index++ {
		action, err := ActionFromIndex(index)
		if err != nil {
			t.Fatalf("ActionFromIndex(%d) = %v", index, err)
		}
		back, err := ActionIndexFromAction(action)
		if err != nil {
			t.Fatalf("ActionIndexFromAction(%v) = %v", action, err)
		}
		if back != index {
			t.Fatalf("round trip %d -> %v -> %d", index, action, back)
		}
	}
}

func TestActionIndexValidAndString(t *testing.T) {
	if !ActionNone.Valid() || ActionIncreaseConcurrency.Valid() != true {
		t.Fatal("valid indices reported invalid")
	}
	if ActionIndex(NumActions).Valid() {
		t.Fatal("NumActions reported valid")
	}
	for index := ActionIndex(0); index < NumActions; index++ {
		if index.String() == "" || strings.HasPrefix(index.String(), "invalid") {
			t.Fatalf("String(%d) = %q", index, index.String())
		}
	}
	if got := ActionIndex(NumActions).String(); !strings.HasPrefix(got, "invalid") {
		t.Fatalf("String(NumActions) = %q, want invalid prefix", got)
	}
}
