package main

import (
	"math"
	"testing"
)

func TestBandDecideNearest(t *testing.T) {
	levels := []float64{146 * 1024, 293 * 1024, 585 * 1024}
	cases := []struct {
		samples []float64
		want    int
	}{
		{[]float64{150 * 1024, 140 * 1024, 145 * 1024}, 0},
		{[]float64{300 * 1024, 290 * 1024, 295 * 1024}, 1},
		{[]float64{600 * 1024, 570 * 1024, 580 * 1024}, 2},
		{[]float64{210 * 1024, 210 * 1024, 210 * 1024}, 0},
		{[]float64{440 * 1024, 440 * 1024, 440 * 1024}, 2},
	}
	for _, c := range cases {
		if got := bandDecide(c.samples, levels); got != c.want {
			t.Errorf("bandDecide(%v) = %d, want %d", c.samples, got, c.want)
		}
	}
}

func TestBandDecideTieBreakFirst(t *testing.T) {
	levels := []float64{100, 200}
	if got := bandDecide([]float64{150, 150, 150}, levels); got != 0 {
		t.Errorf("tie should resolve to the first (more degraded) target, got %d", got)
	}
}

func TestParseBandLevels(t *testing.T) {
	got, err := parseBandLevels("1024, 2048.5,")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []float64{1024, 2048.5}
	if len(got) != len(want) || math.Abs(got[1]-want[1]) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := parseBandLevels("0"); err == nil {
		t.Fatal("zero level must be rejected")
	}
	if _, err := parseBandLevels("abc"); err == nil {
		t.Fatal("non-numeric level must be rejected")
	}
}

func TestParseBandTargets(t *testing.T) {
	ns, ss, err := parseBandTargets("4:4194304, 16:1048576")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ns) != 2 || ns[0] != 4 || ns[1] != 16 || ss[0] != 4194304 || ss[1] != 1048576 {
		t.Fatalf("got %v %v", ns, ss)
	}
	if _, _, err := parseBandTargets("4"); err == nil {
		t.Fatal("missing colon must be rejected")
	}
	if _, _, err := parseBandTargets("x:1024"); err == nil {
		t.Fatal("non-numeric N must be rejected")
	}
}
