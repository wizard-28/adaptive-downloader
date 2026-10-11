package rl

// This file is a DIAGNOSTIC, not a behavioral test. It instruments the real
// LinUCB score computation to explain why LinUCB remains on ActionNone. It does
// not change production code: it only calls the unexported score/updateArm
// methods from within the package. Output is captured by the exploration
// diagnostic command/go test run. See docs/PROJECT_CONTEXT.md
// "LinUCB Exploration Diagnostic".

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func reprState(rate float64) State {
	return State{
		Concurrency:         1,
		ChunkSize:           32768,
		Throughput:          rate,
		AggregateThroughput: rate,
		RequestDuration:     time.Duration(32768.0 / rate * float64(time.Second)),
		BytesTransferred:    32768,
		SuccessfulRequests:  1,
		TotalAttempts:       1,
	}
}

func vecNorm(x []float64) float64 {
	var s float64
	for _, v := range x {
		s += v * v
	}
	return math.Sqrt(s)
}

func actionLabel(a ActionIndex) string { return a.String() }

// TestExplorationDiagnosticInitialScores reports the actual LinUCB score for
// every action on a representative first-decision state of each benchmark
// scenario. At initialization A_a = I and b_a = 0, so every action's score is
// alpha*||x|| and the deterministic lowest-index tie-break picks ActionNone.
func TestExplorationDiagnosticInitialScores(t *testing.T) {
	type scen struct {
		name string
		rate float64
	}
	scenarios := []scen{
		{"baseline", 4 << 20},
		{"server-throttle", 512 << 10},
		{"latency", 4 << 20},
		{"loss", 4 << 20},
		{"combined", 1 << 20},
	}
	const alpha = 0.2
	cfg := defaultFeatureConfig()

	fmt.Println("=== INITIAL LINUCB SCORES (alpha=0.2, A=I, b=0) ===")
	for _, sc := range scenarios {
		s := reprState(sc.rate)
		x, err := s.Features(cfg)
		if err != nil {
			t.Fatalf("features: %v", err)
		}
		l, err := NewLinUCB(LinUCBConfig{Alpha: alpha, NumActions: NumActions, Dimension: len(x)})
		if err != nil {
			t.Fatalf("NewLinUCB: %v", err)
		}
		fmt.Printf("\nscenario=%s observed_rate=%.0f B/s  ||x||=%.6f  alpha*||x||=%.6f\n", sc.name, sc.rate, vecNorm(x), alpha*vecNorm(x))
		fmt.Printf("  features: %v\n", formatFeatures(x))
		for a := ActionIndex(0); a < NumActions; a++ {
			fmt.Printf("    %-20s score=%.9f\n", actionLabel(a), l.score(int(a), x))
		}
	}
}

func formatFeatures(x []float64) string {
	out := ""
	for i, v := range x {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%.6f", v)
	}
	return out
}

// TestExplorationDiagnosticLockIn simulates the greedy lock-in: once ActionNone
// has been selected and updated with a positive reward, its estimated mean can
// exceed the (bounded) uncertainty bonus of every untried action, so none of
// them is ever selected. x is held fixed for clarity.
func TestExplorationDiagnosticLockIn(t *testing.T) {
	const alpha = 0.2
	cfg := defaultFeatureConfig()
	s := reprState(4 << 20) // baseline
	x, err := s.Features(cfg)
	if err != nil {
		t.Fatalf("features: %v", err)
	}
	l, err := NewLinUCB(LinUCBConfig{Alpha: alpha, NumActions: NumActions, Dimension: len(x)})
	if err != nil {
		t.Fatalf("NewLinUCB: %v", err)
	}
	const reward = 3.4 // measured baseline ActionNone reward (frozen benchmark)

	fmt.Println("\n=== GREEDY LOCK-IN (baseline x, ActionNone reward=3.4) ===")
	fmt.Println("step | " + scoresHeader())
	for step := 0; step <= 8; step++ {
		if step > 0 {
			l.updateArm(int(ActionNone), x, reward)
		}
		fmt.Printf("%4d | %s\n", step, scoresRow(l, x))
	}
}

func scoresHeader() string {
	out := ""
	for a := ActionIndex(0); a < NumActions; a++ {
		if a > 0 {
			out += " "
		}
		out += fmt.Sprintf("%-10s", actionLabel(a))
	}
	return out
}

func scoresRow(l *LinUCB, x []float64) string {
	out := ""
	for a := ActionIndex(0); a < NumActions; a++ {
		if a > 0 {
			out += " "
		}
		out += fmt.Sprintf("%10.6f", l.score(int(a), x))
	}
	return out
}
