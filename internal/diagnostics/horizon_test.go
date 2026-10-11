package diagnostics

import (
	"testing"

	"adaptivedl/internal/experiment"
)

// TestHorizonStudyBaseline checks the horizon study on privilege-free baseline
// scenarios: the warm-up is always 5, a larger file yields more total decisions,
// and the post-warm-up budget grows with file size.
func TestHorizonStudyBaseline(t *testing.T) {
	rep, err := RunHorizonStudy(HorizonConfig{
		FileSizes:   []int64{262144, 1048576},
		Scenarios:   []string{"baseline"},
		Modes:       []experiment.Mode{experiment.ModeLinUCB, experiment.ModeFixed},
		Repetitions: 1,
	})
	if err != nil {
		t.Fatalf("RunHorizonStudy = %v", err)
	}
	if len(rep.Aggregates) != 4 {
		t.Fatalf("aggregates = %d, want 4 (2 sizes x 2 modes)", len(rep.Aggregates))
	}
	var small, large *HorizonAggregate
	for i := range rep.Aggregates {
		a := &rep.Aggregates[i]
		if a.Mode != string(experiment.ModeLinUCB) {
			continue
		}
		if a.FileSize == 262144 {
			small = a
		}
		if a.FileSize == 1048576 {
			large = a
		}
	}
	if small == nil || large == nil {
		t.Fatalf("missing LinUCB aggregates: %+v", rep.Aggregates)
	}
	if small.MeanWarmup != WarmupSelections || large.MeanWarmup != WarmupSelections {
		t.Fatalf("warm-up = %v/%v, want %d", small.MeanWarmup, large.MeanWarmup, WarmupSelections)
	}
	if small.ValidRuns != 1 || large.ValidRuns != 1 {
		t.Fatalf("valid runs = %d/%d, want 1/1", small.ValidRuns, large.ValidRuns)
	}
	if large.MeanTotal <= small.MeanTotal {
		t.Fatalf("larger file total %.1f <= smaller %.1f", large.MeanTotal, small.MeanTotal)
	}
	if large.MeanPost <= small.MeanPost {
		t.Fatalf("larger file post %.1f <= smaller %.1f", large.MeanPost, small.MeanPost)
	}
	if small.MeanPost < 1 {
		t.Fatalf("small file post = %.1f, want >= 1", small.MeanPost)
	}
}
