package dqn

// Smoke test for the exported Candidate v5 artifact: loads it exactly as the
// live path does, validates the v5 state/action contract, and runs deterministic
// greedy inference on fixed v5 observations. The artifact is produced by
// python/train_validate_v5.py; the test skips when it is absent so a fresh
// checkout still builds. Override the path with DQN_V5_CANDIDATE.
//
// This does not run the real downloader (no live transfer) and makes no
// performance claim.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"adaptivedl/internal/policy"
)

func v5CandidatePath() string {
	if p := os.Getenv("DQN_V5_CANDIDATE"); p != "" {
		return p
	}
	return filepath.Join(
		"..", "..", "..", "testdata", "diagnostics", "dqn_validation_v5",
		"final_dqn_candidate_v5.json",
	)
}

func v5SmokeFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func v5SmokeSHA(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func v5SmokeExpectedSHA(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		t.Fatalf("empty sha256 file %s", path)
	}
	return fields[0]
}

func TestV5CandidateModelGoInference(t *testing.T) {
	path := v5CandidatePath()
	if !v5SmokeFileExists(path) {
		t.Skipf("v5 candidate not present at %s (run python/train_validate_v5.py first)", path)
	}
	m, err := LoadModel(path)
	if err != nil {
		t.Fatalf("LoadModel(%s) = %v", path, err)
	}
	if m.StateSpec.Version != StateSpecVersionV5 {
		t.Fatalf("state spec version = %d, want %d", m.StateSpec.Version, StateSpecVersionV5)
	}
	if m.NumActions != NumConcurrencyActions {
		t.Fatalf("num actions = %d, want %d", m.NumActions, NumConcurrencyActions)
	}
	wantActions := ConcurrencyActionNames()
	for i, want := range wantActions {
		if m.ActionNames[i] != want {
			t.Fatalf("action name %d = %q, want %q", i, m.ActionNames[i], want)
		}
	}
	wantNames := FeatureNamesV5()
	if len(m.FeatureNames) != len(wantNames) {
		t.Fatalf("feature names = %d, want %d", len(m.FeatureNames), len(wantNames))
	}
	for i := range wantNames {
		if m.FeatureNames[i] != wantNames[i] {
			t.Fatalf("feature %d = %q, want %q", i, m.FeatureNames[i], wantNames[i])
		}
	}
	if shaPath := strings.TrimSuffix(path, ".json") + ".sha256"; v5SmokeFileExists(shaPath) {
		want := v5SmokeExpectedSHA(t, shaPath)
		got := v5SmokeSHA(t, path)
		if want != got {
			t.Fatalf("candidate sha256 mismatch: file says %s, actual %s", want, got)
		}
	}

	p, err := NewLiveConcurrencyPolicyV5(m, 16<<20)
	if err != nil {
		t.Fatalf("NewLiveConcurrencyPolicyV5 = %v", err)
	}
	// One decision per completed attempt, as the serialized runner does.
	ctxs := []policy.Context{
		{
			Concurrency: 1, ChunkSize: 1 << 20, AggregateThroughput: 2 << 20,
			RequestDuration:    500 * time.Millisecond,
			SuccessfulRequests: 1, TotalAttempts: 1, BytesTransferred: 1 << 20,
		},
		{
			Concurrency: 3, ChunkSize: 1 << 20, AggregateThroughput: 8 << 20,
			RequestDuration:    120 * time.Millisecond,
			SuccessfulRequests: 2, TotalAttempts: 2, BytesTransferred: 1 << 20,
		},
		{
			Concurrency: 4, ChunkSize: 1 << 20, AggregateThroughput: 256 << 10,
			RequestDuration:    3 * time.Second,
			SuccessfulRequests: 3, FailedRequests: 1, Retries: 1,
			TotalAttempts: 4, BytesTransferred: 1 << 20,
		},
	}
	first := make([]policy.ActionKind, len(ctxs))
	for i, c := range ctxs {
		a := p.SelectAction(c)
		if err := a.Validate(); err != nil {
			t.Fatalf("context %d action invalid: %v", i, err)
		}
		first[i] = a.Kind
	}
	log := p.Log()
	if len(log) != len(ctxs) {
		t.Fatalf("logged decisions = %d, want %d", len(log), len(ctxs))
	}
	for i, d := range log {
		if len(d.Features) != NumFeaturesV5() {
			t.Fatalf("decision %d features = %d, want %d", i, len(d.Features), NumFeaturesV5())
		}
		if want := int64(16<<20) - int64(i+1)*(1<<20); d.RemainingBytes != want {
			t.Fatalf("decision %d remaining = %d, want %d", i, d.RemainingBytes, want)
		}
	}

	// Deterministic inference: a fresh adapter must select the same actions.
	p2, err := NewLiveConcurrencyPolicyV5(m, 16<<20)
	if err != nil {
		t.Fatalf("second NewLiveConcurrencyPolicyV5 = %v", err)
	}
	for i, c := range ctxs {
		a := p2.SelectAction(c)
		if a.Kind != first[i] {
			t.Fatalf("decision %d not deterministic: %d vs %d", i, a.Kind, first[i])
		}
	}
}
