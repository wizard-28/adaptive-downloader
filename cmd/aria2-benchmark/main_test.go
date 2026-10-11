package main

import (
	"strings"
	"testing"

	"adaptivedl/internal/experiment/server"
)

func TestParseAria2Log(t *testing.T) {
	log := "[#1 1.0MiB/4.0MiB(25%) CN:4 DL:8.0MiB]\n" +
		"[#1 2.0MiB/4.0MiB(50%) CN:2 DL:8.0MiB]\n" +
		"A retry occurred\n" +
		"Restarting the download\n"
	maxCN, retries := parseAria2Log(log)
	if maxCN != 4 {
		t.Fatalf("maxConnections = %d, want 4", maxCN)
	}
	if retries != 2 {
		t.Fatalf("retries = %d, want 2", retries)
	}
}

func TestParseAria2LogEmpty(t *testing.T) {
	maxCN, retries := parseAria2Log("")
	if maxCN != 0 || retries != 0 {
		t.Fatalf("empty log = %d/%d, want 0/0", maxCN, retries)
	}
}

func TestSummarizeRanges(t *testing.T) {
	rs := []server.Range{
		{Start: 1048576, End: 2097151},
		{Start: 2097152, End: 3145727},
		{Start: 3145728, End: 4194303},
	}
	s := summarizeRanges(rs)
	if s.Count != 3 {
		t.Fatalf("count = %d, want 3", s.Count)
	}
	if s.Min != 1048576 || s.Max != 1048576 || s.Mean != 1048576 {
		t.Fatalf("min/max/mean = %d/%d/%.0f, want all 1048576", s.Min, s.Max, s.Mean)
	}
	if s.Start != 1048576 {
		t.Fatalf("first start = %d, want 1048576", s.Start)
	}
	if len(s.List) != 3 || !strings.Contains(s.List[0], "1048576-2097151") {
		t.Fatalf("list = %v", s.List)
	}
}

func TestContentPatternDeterministic(t *testing.T) {
	a := contentPattern(64)
	b := contentPattern(64)
	if string(a) != string(b) {
		t.Fatal("contentPattern not deterministic")
	}
}
