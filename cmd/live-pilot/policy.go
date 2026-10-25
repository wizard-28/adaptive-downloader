package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl/dqn"
)

// Opt-in arm policies. An Arm with Policy == "" and no extra options runs
// exactly as before. Policy arms are listed in Manifest.PolicyArms.
const (
	policyDQN6 = "dqn6"
	policyDrop = "drop"

	defaultDQN6Window = 16
	defaultDropWindow = 3
	defaultDropRatio  = 0.5
	defaultCappedN    = 4

	policyMinN = 1
	policyMaxN = 16
)

// hasPolicyOptions reports whether the arm uses any opt-in field.
func (a Arm) hasPolicyOptions() bool {
	return a.Policy != "" || a.DQNModel != "" || a.Window != 0 || a.CappedN != 0 ||
		a.DropRatio != 0 || a.NoRequestTimeout || a.Split
}

func (a Arm) validatePolicy() error {
	if a.Kind != "ours" {
		return fmt.Errorf("manifest: policy arm kind must be \"ours\", got %q", a.Kind)
	}
	if a.N <= 0 || a.S <= 0 {
		return fmt.Errorf("manifest: policy arm needs positive n and s: %+v", a)
	}
	switch a.Policy {
	case "":
		if a.DQNModel != "" || a.Window != 0 || a.CappedN != 0 || a.DropRatio != 0 {
			return errors.New("manifest: policy options set without a policy")
		}
	case policyDQN6:
		if a.DQNModel == "" {
			return errors.New("manifest: policy dqn6 requires dqn_model")
		}
	case policyDrop:
		if a.DQNModel != "" {
			return errors.New("manifest: dqn_model is only valid with policy dqn6")
		}
	default:
		return fmt.Errorf("manifest: unknown policy %q", a.Policy)
	}
	if a.Window < 0 || a.CappedN < 0 || a.DropRatio < 0 || a.DropRatio >= 1 && a.DropRatio != 0 {
		return fmt.Errorf("manifest: bad policy parameters: %+v", a)
	}
	if a.CappedN > policyMaxN {
		return fmt.Errorf("manifest: capped_n above %d", policyMaxN)
	}
	return nil
}

// decisionTrace is one compact policy decision.
type decisionTrace struct {
	TimeMs      float64 `json:"time_ms"`
	Concurrency int     `json:"concurrency"`
	Action      string  `json:"action"`
}

func actionString(a policy.Action) string {
	switch a.Kind {
	case policy.ActionNone:
		return "none"
	case policy.ActionIncreaseConcurrency:
		return fmt.Sprintf("inc-n:%d", a.Value)
	case policy.ActionDecreaseConcurrency:
		return fmt.Sprintf("dec-n:%d", a.Value)
	case policy.ActionIncreaseChunkSize:
		return "inc-s"
	case policy.ActionDecreaseChunkSize:
		return "dec-s"
	default:
		return "other"
	}
}

// recordingPolicy wraps a policy and records each decision.
type recordingPolicy struct {
	inner  policy.Policy
	start  time.Time
	mu     sync.Mutex
	events []decisionTrace
}

func (p *recordingPolicy) SelectAction(ctx policy.Context) policy.Action {
	a := p.inner.SelectAction(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, decisionTrace{
		TimeMs:      float64(time.Since(p.start).Microseconds()) / 1000.0,
		Concurrency: ctx.Concurrency, Action: actionString(a),
	})
	return a
}

func (p *recordingPolicy) Update(ctx policy.Context, a policy.Action, r float64) {
	p.inner.Update(ctx, a, r)
}

func (p *recordingPolicy) decisions() []decisionTrace {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]decisionTrace(nil), p.events...)
}

// dropPolicy is the causal drop rule (port of the "drop" mode of
// testdata/rl_cap_train_v2/tools/v2_bench.go targetPolicy): track the running
// maximum of the median of the last k per-attempt throughputs; the first time
// the median falls below ratio x that maximum, switch once to cappedN and stay
// there. Chunk size is fixed in live-pilot, so only concurrency actions are
// produced, in steps of at most 16 like the harness.
type dropPolicy struct {
	k       int
	ratio   float64
	cappedN int

	mu       sync.Mutex
	samples  []float64
	baseline float64
	decided  bool
}

func newDropPolicy(k int, ratio float64, cappedN int) *dropPolicy {
	return &dropPolicy{k: k, ratio: ratio, cappedN: cappedN}
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	return c[len(c)/2]
}

func (p *dropPolicy) SelectAction(ctx policy.Context) policy.Action {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples = append(p.samples, ctx.Throughput)
	if !p.decided && len(p.samples) >= p.k {
		med := median(p.samples[len(p.samples)-p.k:])
		if med > p.baseline {
			p.baseline = med
		}
		if p.baseline > 0 && med < p.ratio*p.baseline {
			p.decided = true
		}
	}
	if !p.decided {
		return policy.NoChange()
	}
	switch {
	case ctx.Concurrency > p.cappedN:
		d := int64(ctx.Concurrency - p.cappedN)
		if d > 16 {
			d = 16
		}
		return policy.Action{Kind: policy.ActionDecreaseConcurrency, Value: d}
	case ctx.Concurrency < p.cappedN:
		d := int64(p.cappedN - ctx.Concurrency)
		if d > 16 {
			d = 16
		}
		return policy.Action{Kind: policy.ActionIncreaseConcurrency, Value: d}
	}
	return policy.NoChange()
}

func (p *dropPolicy) Update(policy.Context, policy.Action, float64) {}

// armPolicy is the per-run policy wiring for an arm.
type armPolicy struct {
	rec       *recordingPolicy
	window    int
	modelSHA  string
	modelPath string
}

// applyPolicy configures cfg for the arm's opt-in options and returns the
// policy wiring (nil when the arm has no policy).
func applyPolicy(cfg *downloader.Config, a Arm, totalSize int64, start time.Time) (*armPolicy, error) {
	// Opt-in arms follow the harness: the request deadline is off only when
	// no_request_timeout is set (otherwise the fetcher default applies).
	cfg.Timeout, cfg.NoRequestTimeout = 0, a.NoRequestTimeout
	cfg.SplitStragglers = a.Split
	if a.Policy == "" {
		return nil, nil
	}
	ap := &armPolicy{}
	var inner policy.Policy
	switch a.Policy {
	case policyDQN6:
		raw, err := os.ReadFile(a.DQNModel)
		if err != nil {
			return nil, fmt.Errorf("dqn model: %w", err)
		}
		sum := sha256.Sum256(raw)
		ap.modelSHA, ap.modelPath = hex.EncodeToString(sum[:]), a.DQNModel
		m, err := dqn.LoadModel(a.DQNModel)
		if err != nil {
			return nil, fmt.Errorf("dqn model: %w", err)
		}
		lp, err := dqn.NewLiveConcurrencyPolicyV6(m, totalSize)
		if err != nil {
			return nil, fmt.Errorf("dqn policy: %w", err)
		}
		inner = lp
		ap.window = a.Window
		if ap.window == 0 {
			ap.window = defaultDQN6Window
		}
	case policyDrop:
		k, ratio, capped := a.Window, a.DropRatio, a.CappedN
		if k == 0 {
			k = defaultDropWindow
		}
		if ratio == 0 {
			ratio = defaultDropRatio
		}
		if capped == 0 {
			capped = defaultCappedN
		}
		inner = newDropPolicy(k, ratio, capped)
		ap.window = k
	default:
		return nil, fmt.Errorf("unknown policy %q", a.Policy)
	}
	ap.rec = &recordingPolicy{inner: inner, start: start}
	cfg.Policy = ap.rec
	cfg.PolicyWindowSize = ap.window
	cfg.MinConcurrency, cfg.MaxConcurrency = policyMinN, policyMaxN
	cfg.MinChunkSize, cfg.MaxChunkSize = a.S, a.S
	cfg.InitialChunkSize, cfg.ChunkSizeStep = a.S, a.S
	return ap, nil
}
