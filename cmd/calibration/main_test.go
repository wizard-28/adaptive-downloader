package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"adaptivedl/internal/experiment"
	"adaptivedl/internal/rl"
)

func TestDistributionString(t *testing.T) {
	d := experiment.DistributionFromActions([]rl.ActionIndex{
		rl.ActionNone, rl.ActionNone, rl.ActionIncreaseConcurrency,
	})
	got := distributionString(d)
	if !strings.Contains(got, "none=2") || !strings.Contains(got, "increase-concurrency=1") {
		t.Fatalf("distributionString = %q", got)
	}
	if empty := distributionString(experiment.ActionDistribution{}); empty != "(no decisions)" {
		t.Fatalf("empty distributionString = %q", empty)
	}
}

func TestWriteJSONRoundTrip(t *testing.T) {
	report := experiment.CalibrationReport{
		Alpha:        0.2,
		RewardConfig: rl.DefaultRewardConfig(),
		Scenarios: []experiment.ScenarioCalibration{
			{
				Name:   "s",
				Runs:   []experiment.RunDiagnostics{{Mode: experiment.ModeLinUCB, Decisions: 3, Valid: true, NumericallyHealthy: true}},
				Pooled: experiment.RunDiagnostics{Mode: experiment.ModeLinUCB, Decisions: 3, Valid: true, NumericallyHealthy: true},
			},
		},
	}
	path := filepath.Join(t.TempDir(), "sub", "calibration.json")
	if err := writeJSON(path, report); err != nil {
		t.Fatalf("writeJSON = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile = %v", err)
	}
	var back experiment.CalibrationReport
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal = %v", err)
	}
	if len(back.Scenarios) != 1 || back.Scenarios[0].Pooled.Decisions != 3 {
		t.Fatalf("round trip mismatch: %+v", back)
	}
}

func TestPrintReportDoesNotPanic(t *testing.T) {
	report := experiment.CalibrationReport{
		Alpha:        0.2,
		RewardConfig: rl.DefaultRewardConfig(),
		Scenarios: []experiment.ScenarioCalibration{{
			Name: "s",
			Runs: []experiment.RunDiagnostics{{Valid: true, NumericallyHealthy: true}},
			Pooled: experiment.RunDiagnostics{
				Decisions:          2,
				Valid:              true,
				NumericallyHealthy: true,
			},
		}},
	}
	var buf bytes.Buffer
	printReport(&buf, report)
	if !strings.Contains(buf.String(), "LinUCB calibration") {
		t.Fatalf("unexpected report output: %q", buf.String())
	}
}
