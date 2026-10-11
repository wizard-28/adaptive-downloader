package main

import (
	"testing"

	"adaptivedl/internal/experiment/server"
)

func TestSummarizeRangesAria2(t *testing.T) {
	rs := []server.Range{
		{Start: 0, End: 1048575},
		{Start: 1048576, End: 2097151},
		{Start: 2097152, End: 3145727},
		{Start: 3145728, End: 4194303},
	}
	s := summarizeRanges(rs)
	if s.Count != 4 || s.Min != 1048576 || s.Max != 1048576 || s.Mean != 1048576 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestCountRetries(t *testing.T) {
	if got := countRetries("downloading...\n"); got != 0 {
		t.Fatalf("countRetries(clean) = %d, want 0", got)
	}
	if got := countRetries("retry\nRestarting the download\n"); got != 2 {
		t.Fatalf("countRetries(retries) = %d, want 2", got)
	}
}

func TestContentPatternDeterministic(t *testing.T) {
	if string(contentPattern(128)) != string(contentPattern(128)) {
		t.Fatal("contentPattern not deterministic")
	}
}

func TestAria2BaseCommandLocked(t *testing.T) {
	// The locked configuration must include the frozen flags.
	want := map[string]bool{
		"--split=4": false, "--min-split-size=1048576": false,
		"--max-connection-per-server=4": false, "--max-tries=3": false,
	}
	for _, a := range aria2BaseCommand() {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("locked aria2 config missing %q", k)
		}
	}
}
