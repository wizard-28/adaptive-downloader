package policy

import (
	"errors"
	"testing"
	"time"
)

func TestFixedPolicyAlwaysNoChange(t *testing.T) {
	var p Policy = FixedPolicy{}

	contexts := []Context{
		{},
		{Concurrency: 4, ChunkSize: 1 << 20, Throughput: 1234.5},
		{Concurrency: 1, ChunkSize: 1, FailedRequests: 3, Retries: 3},
		{Concurrency: 0, ChunkSize: 0, Throughput: 0},
	}

	for i, ctx := range contexts {
		action := p.SelectAction(ctx)
		if !action.IsNoChange() {
			t.Fatalf("context %d: SelectAction = %v, want no change", i, action)
		}
		if err := action.Validate(); err != nil {
			t.Fatalf("context %d: action invalid: %v", i, err)
		}
		// Update must be safe to call and must not panic.
		p.Update(ctx, action, 0)
		p.Update(ctx, action, -1)
		p.Update(ctx, action, 42.5)
	}
}

func TestContextRepresentsObservations(t *testing.T) {
	ctx := Context{
		Concurrency:         6,
		ChunkSize:           1 << 20,
		Throughput:          8_000_000,
		AggregateThroughput: 24_000_000,
		RequestDuration:     250 * time.Millisecond,
		BytesTransferred:    2_000_000,
		SuccessfulRequests:  12,
		FailedRequests:      2,
		Retries:             2,
		TotalAttempts:       14,
	}

	if ctx.Concurrency != 6 || ctx.ChunkSize != 1<<20 {
		t.Errorf("configuration fields not preserved: %+v", ctx)
	}
	if ctx.Throughput != 8_000_000 || ctx.AggregateThroughput != 24_000_000 ||
		ctx.RequestDuration != 250*time.Millisecond || ctx.BytesTransferred != 2_000_000 {
		t.Errorf("recent observation fields not preserved: %+v", ctx)
	}
	if ctx.SuccessfulRequests != 12 || ctx.FailedRequests != 2 || ctx.Retries != 2 || ctx.TotalAttempts != 14 {
		t.Errorf("outcome fields not preserved: %+v", ctx)
	}
	if err := ctx.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestZeroContextIsValid(t *testing.T) {
	var ctx Context
	if err := ctx.Validate(); err != nil {
		t.Fatalf("zero Context Validate() = %v, want nil", err)
	}
}

func TestActionConstructors(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		kind   ActionKind
		value  int64
	}{
		{"no change", NoChange(), ActionNone, 0},
		{"increase concurrency", IncreaseConcurrency(), ActionIncreaseConcurrency, 0},
		{"decrease concurrency", DecreaseConcurrency(), ActionDecreaseConcurrency, 0},
		{"increase chunk size", IncreaseChunkSize(), ActionIncreaseChunkSize, 0},
		{"decrease chunk size", DecreaseChunkSize(), ActionDecreaseChunkSize, 0},
		{"set concurrency", SetConcurrency(8), ActionSetConcurrency, 8},
		{"set chunk size", SetChunkSize(4096), ActionSetChunkSize, 4096},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.action.Kind != tc.kind {
				t.Errorf("Kind = %d, want %d", tc.action.Kind, tc.kind)
			}
			if tc.action.Value != tc.value {
				t.Errorf("Value = %d, want %d", tc.action.Value, tc.value)
			}
			if err := tc.action.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestZeroActionIsNoChange(t *testing.T) {
	var a Action
	if !a.IsNoChange() {
		t.Fatalf("zero Action IsNoChange() = false, want true")
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("zero Action Validate() = %v, want nil", err)
	}
}

func TestActionValidateRejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		action Action
	}{
		{"unknown kind", Action{Kind: ActionKind(200)}},
		{"set concurrency zero", SetConcurrency(0)},
		{"set concurrency negative", SetConcurrency(-3)},
		{"set chunk size zero", SetChunkSize(0)},
		{"set chunk size negative", SetChunkSize(-1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.action.Validate(); !errors.Is(err, ErrInvalidAction) {
				t.Fatalf("Validate() = %v, want ErrInvalidAction", err)
			}
		})
	}
}

func TestContextValidateRejectsNegative(t *testing.T) {
	cases := []struct {
		name string
		ctx  Context
	}{
		{"concurrency", Context{Concurrency: -1}},
		{"chunk size", Context{ChunkSize: -1}},
		{"throughput", Context{Throughput: -0.1}},
		{"aggregate throughput", Context{AggregateThroughput: -0.1}},
		{"request duration", Context{RequestDuration: -time.Second}},
		{"bytes", Context{BytesTransferred: -1}},
		{"successful", Context{SuccessfulRequests: -1}},
		{"failed", Context{FailedRequests: -1}},
		{"retries", Context{Retries: -1}},
		{"attempts", Context{TotalAttempts: -1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.ctx.Validate(); !errors.Is(err, ErrInvalidContext) {
				t.Fatalf("Validate() = %v, want ErrInvalidContext", err)
			}
		})
	}
}

// recordingPolicy is a minimal Policy implementation used to prove the
// interface is implementable outside the package.
type recordingPolicy struct {
	selected []Context
	updates  []update
}

type update struct {
	ctx    Context
	action Action
	reward float64
}

func (p *recordingPolicy) SelectAction(ctx Context) Action {
	p.selected = append(p.selected, ctx)
	return IncreaseConcurrency()
}

func (p *recordingPolicy) Update(ctx Context, action Action, reward float64) {
	p.updates = append(p.updates, update{ctx: ctx, action: action, reward: reward})
}

func TestPolicyInterfaceImplementable(t *testing.T) {
	var p Policy = &recordingPolicy{}

	ctx := Context{Concurrency: 2, ChunkSize: 1024, Throughput: 500}
	action := p.SelectAction(ctx)
	if action.Kind != ActionIncreaseConcurrency {
		t.Fatalf("SelectAction = %v, want increase concurrency", action)
	}

	p.Update(ctx, action, 1.5)

	rp, ok := p.(*recordingPolicy)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if len(rp.selected) != 1 || rp.selected[0] != ctx {
		t.Fatalf("selected = %+v, want one %+v", rp.selected, ctx)
	}
	if len(rp.updates) != 1 {
		t.Fatalf("len(updates) = %d, want 1", len(rp.updates))
	}
	if got := rp.updates[0]; got.ctx != ctx || got.action != action || got.reward != 1.5 {
		t.Fatalf("update = %+v, want ctx=%+v action=%v reward=1.5", got, ctx, action)
	}
}

func TestActionString(t *testing.T) {
	cases := map[Action]string{
		NoChange():              "none",
		IncreaseConcurrency():   "increase-concurrency",
		DecreaseConcurrency():   "decrease-concurrency",
		IncreaseChunkSize():     "increase-chunk-size",
		DecreaseChunkSize():     "decrease-chunk-size",
		SetConcurrency(7):       "set-concurrency(7)",
		SetChunkSize(2048):      "set-chunk-size(2048)",
		{Kind: ActionKind(255)}: "unknown(255)",
	}
	for action, want := range cases {
		if got := action.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", action.Kind, got, want)
		}
	}
}
