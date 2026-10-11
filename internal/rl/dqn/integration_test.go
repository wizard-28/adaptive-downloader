package dqn_test

import (
	"os"
	"path/filepath"
	"testing"

	"adaptivedl/internal/rl/dqn"
)

// TestExportedModelLoadsAndInfers verifies the Python-exported model artifact
// loads in Go and produces deterministic greedy actions. It is skipped if the
// artifact is absent (for example in a fresh checkout without training).
func TestExportedModelLoadsAndInfers(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "diagnostics", "dqn", "exported_model.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("exported model not present: %v", err)
	}
	m, err := dqn.LoadModel(path)
	if err != nil {
		t.Fatalf("LoadModel = %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	obs := dqn.Observation{
		Concurrency: 1, ChunkSize: 32768, TotalBytes: 4 << 20, DoneBytes: 1 << 20,
		AggregateThroughput: 4 << 20, ThroughputEWMA: 4 << 20, PrevThroughput: 2 << 20,
	}
	a1, err := m.SelectAction(obs)
	if err != nil {
		t.Fatalf("SelectAction = %v", err)
	}
	a2, _ := m.SelectAction(obs)
	if a1 != a2 {
		t.Fatalf("Go inference not deterministic: %d vs %d", a1, a2)
	}
	if a1 < 0 || a1 >= dqn.NumActions() {
		t.Fatalf("action out of range: %d", a1)
	}
}
