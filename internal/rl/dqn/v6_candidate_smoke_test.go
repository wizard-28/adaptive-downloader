package dqn

// Smoke test for the exported Candidate v6 artifact: loads it exactly as the
// live path does, validates the v6 state/action contract, and runs deterministic
// greedy inference on fixed observations. Skips when the artifact is absent;
// override the path with DQN_V6_CANDIDATE. No live transfer; no performance claim.

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

func v6CandidatePath() string {
	if p := os.Getenv("DQN_V6_CANDIDATE"); p != "" {
		return p
	}
	return filepath.Join("..", "..", "..", "testdata", "diagnostics", "dqn_validation_v6",
		"final_dqn_candidate_v6.json")
}

func TestV6CandidateModelGoInference(t *testing.T) {
	path := v6CandidatePath()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("v6 candidate not present at %s (run python/train_validate_v6.py first)", path)
	}
	m, err := LoadModel(path)
	if err != nil {
		t.Fatalf("LoadModel(%s) = %v", path, err)
	}
	if m.StateSpec.Version != StateSpecVersionV6 {
		t.Fatalf("state spec version = %d, want %d", m.StateSpec.Version, StateSpecVersionV6)
	}
	if m.StateSpec.MaxConcurrency != 16 {
		t.Fatalf("max concurrency = %d, want 16", m.StateSpec.MaxConcurrency)
	}
	if m.NumActions != NumConcurrencyActions {
		t.Fatalf("num actions = %d, want %d", m.NumActions, NumConcurrencyActions)
	}
	for i, want := range ConcurrencyActionNames() {
		if m.ActionNames[i] != want {
			t.Fatalf("action name %d = %q, want %q", i, m.ActionNames[i], want)
		}
	}
	for i, want := range FeatureNamesV6() {
		if m.FeatureNames[i] != want {
			t.Fatalf("feature %d = %q, want %q", i, m.FeatureNames[i], want)
		}
	}
	if shaPath := strings.TrimSuffix(path, ".json") + ".sha256"; fileExistsV6(shaPath) {
		if want := readFirstFieldV6(t, shaPath); want != sha256FileV6(t, path) {
			t.Fatalf("candidate sha256 mismatch: file says %s", want)
		}
	}
	p, err := NewLiveConcurrencyPolicyV6(m, 16<<20)
	if err != nil {
		t.Fatalf("NewLiveConcurrencyPolicyV6 = %v", err)
	}
	ctxs := []policy.Context{
		{Concurrency: 1, ChunkSize: 1 << 20, AggregateThroughput: 4 << 20,
			RequestDuration: 200 * time.Millisecond, SuccessfulRequests: 1,
			TotalAttempts: 1, BytesTransferred: 1 << 20},
		{Concurrency: 8, ChunkSize: 1 << 20, AggregateThroughput: 12 << 20,
			RequestDuration: 90 * time.Millisecond, SuccessfulRequests: 2,
			TotalAttempts: 2, BytesTransferred: 1 << 20},
		{Concurrency: 16, ChunkSize: 1 << 20, AggregateThroughput: 256 << 10,
			RequestDuration: 5 * time.Second, SuccessfulRequests: 3,
			FailedRequests: 1, Retries: 1, TotalAttempts: 4, BytesTransferred: 1 << 20},
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
		if len(d.Features) != NumFeaturesV6() {
			t.Fatalf("decision %d features = %d, want %d", i, len(d.Features), NumFeaturesV6())
		}
		if want := int64(16<<20) - int64(i+1)*(1<<20); d.RemainingBytes != want {
			t.Fatalf("decision %d remaining = %d, want %d", i, d.RemainingBytes, want)
		}
	}
	p2, err := NewLiveConcurrencyPolicyV6(m, 16<<20)
	if err != nil {
		t.Fatalf("second NewLiveConcurrencyPolicyV6 = %v", err)
	}
	for i, c := range ctxs {
		if a := p2.SelectAction(c); a.Kind != first[i] {
			t.Fatalf("decision %d not deterministic: %d vs %d", i, a.Kind, first[i])
		}
	}
}

func fileExistsV6(p string) bool { _, err := os.Stat(p); return err == nil }

func readFirstFieldV6(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		t.Fatalf("empty sha file %s", p)
	}
	return f[0]
}

func sha256FileV6(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
