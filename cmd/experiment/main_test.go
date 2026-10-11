package main

import (
	"reflect"
	"testing"

	"adaptivedl/internal/experiment"
)

func TestParseModes(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want []experiment.Mode
	}{
		{"empty defaults to baseline", "", experiment.AllModes()},
		{"baseline explicit", "fixed,joint-adaptive", []experiment.Mode{experiment.ModeFixed, experiment.ModeJointAdaptive}},
		{"linucb only", "linucb", []experiment.Mode{experiment.ModeLinUCB}},
		{"baseline plus linucb", "fixed,linucb", []experiment.Mode{experiment.ModeFixed, experiment.ModeLinUCB}},
		{"spaces tolerated", " fixed , linucb ", []experiment.Mode{experiment.ModeFixed, experiment.ModeLinUCB}},
		{"duplicates collapsed", "linucb,linucb", []experiment.Mode{experiment.ModeLinUCB}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseModes(tc.spec)
			if err != nil {
				t.Fatalf("parseModes(%q) = %v", tc.spec, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseModes(%q) = %v, want %v", tc.spec, got, tc.want)
			}
		})
	}
}

func TestParseModesUnknown(t *testing.T) {
	if _, err := parseModes("fixed,nope"); err == nil {
		t.Fatal("parseModes(unknown) = nil, want error")
	}
}

func TestApplyFileSizeOverride(t *testing.T) {
	const fourMiB = int64(4 << 20)

	// Zero is a no-op: the locked 256 KiB default is preserved.
	mc := experiment.DefaultMatrixConfig()
	applyFileSizeOverride(&mc, 0)
	for _, sc := range mc.Scenarios {
		if sc.Config.FileSize != int64(256<<10) {
			t.Fatalf("zero override changed %s file size to %d", sc.Name, sc.Config.FileSize)
		}
	}

	// Positive override applies to every scenario.
	mc = experiment.DefaultMatrixConfig()
	applyFileSizeOverride(&mc, fourMiB)
	if len(mc.Scenarios) == 0 {
		t.Fatal("no scenarios")
	}
	for _, sc := range mc.Scenarios {
		if sc.Config.FileSize != fourMiB {
			t.Fatalf("%s file size = %d, want %d", sc.Name, sc.Config.FileSize, fourMiB)
		}
	}

	// DefaultMatrixConfig itself must be unaffected by a prior override.
	fresh := experiment.DefaultMatrixConfig()
	for _, sc := range fresh.Scenarios {
		if sc.Config.FileSize != int64(256<<10) {
			t.Fatalf("DefaultMatrixConfig mutated: %s = %d", sc.Name, sc.Config.FileSize)
		}
	}
}

func TestApplyLinUCBIntervalOverride(t *testing.T) {
	// Default is zero (standard LinUCB).
	mc := experiment.DefaultMatrixConfig()
	applyLinUCBIntervalOverride(&mc, 0)
	for _, sc := range mc.Scenarios {
		if sc.Config.LinUCBExplorationInterval != 0 {
			t.Fatalf("%s interval = %d, want 0", sc.Name, sc.Config.LinUCBExplorationInterval)
		}
	}
	// Explicit override applies to every scenario.
	mc = experiment.DefaultMatrixConfig()
	applyLinUCBIntervalOverride(&mc, 5)
	for _, sc := range mc.Scenarios {
		if sc.Config.LinUCBExplorationInterval != 5 {
			t.Fatalf("%s interval = %d, want 5", sc.Name, sc.Config.LinUCBExplorationInterval)
		}
	}
	// A fresh config is unaffected.
	for _, sc := range experiment.DefaultMatrixConfig().Scenarios {
		if sc.Config.LinUCBExplorationInterval != 0 {
			t.Fatalf("DefaultMatrixConfig mutated: %s interval = %d", sc.Name, sc.Config.LinUCBExplorationInterval)
		}
	}
}
