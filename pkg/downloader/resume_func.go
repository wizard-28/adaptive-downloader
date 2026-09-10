package downloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"adaptive-downloader/pkg/rangetracker"
)

const resumeFileSuffix = ".resume"

type resumeState struct {
	URL             string                  `json:"url"`
	TotalSize       int64                   `json:"total_size"`
	CompletedRanges []rangetracker.Interval `json:"completed_ranges"`
}

func resumePath(outputPath string) string {
	return outputPath + resumeFileSuffix
}

func loadResumeState(
	outputPath string,
	targetURL string,
	totalSize int64,
) ([]rangetracker.Interval, bool, error) {
	statePath := resumePath(outputPath)

	data, err := os.ReadFile(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf(
			"read resume state %q: %w",
			statePath,
			err,
		)
	}

	var state resumeState

	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, fmt.Errorf(
			"parse resume state %q: %w",
			statePath,
			err,
		)
	}

	if state.URL != targetURL {
		return nil, false, fmt.Errorf(
			"resume state belongs to a different URL; remove %q to start a new download",
			statePath,
		)
	}

	if state.TotalSize != totalSize {
		return nil, false, fmt.Errorf(
			"resume state has size %d, but server reports %d; remove %q to start a new download",
			state.TotalSize,
			totalSize,
			statePath,
		)
	}

	if err := validateResumeRanges(
		state.CompletedRanges,
		totalSize,
	); err != nil {
		return nil, false, fmt.Errorf(
			"invalid resume state %q: %w",
			statePath,
			err,
		)
	}

	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, fmt.Errorf(
				"resume state %q exists but output file %q is missing",
				statePath,
				outputPath,
			)
		}

		return nil, false, fmt.Errorf(
			"stat output file %q: %w",
			outputPath,
			err,
		)
	}

	if fileInfo.Size() != totalSize {
		return nil, false, fmt.Errorf(
			"partial output file has size %d, expected %d; remove %q to start a new download",
			fileInfo.Size(),
			totalSize,
			statePath,
		)
	}

	return state.CompletedRanges, true, nil
}

func saveResumeState(
	outputPath string,
	targetURL string,
	totalSize int64,
	completed []rangetracker.Interval,
) error {
	state := resumeState{
		URL:             targetURL,
		TotalSize:       totalSize,
		CompletedRanges: completed,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode resume state: %w", err)
	}

	statePath := resumePath(outputPath)
	tempPath := statePath + ".tmp"

	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf(
			"write temporary resume state %q: %w",
			tempPath,
			err,
		)
	}

	if err := os.Rename(tempPath, statePath); err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf(
			"replace resume state %q: %w",
			statePath,
			err,
		)
	}

	return nil
}

func removeResumeState(outputPath string) error {
	err := os.Remove(resumePath(outputPath))

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(
			"remove resume state: %w",
			err,
		)
	}

	return nil
}

func validateResumeRanges(
	ranges []rangetracker.Interval,
	totalSize int64,
) error {
	var previousEnd int64 = -1

	for _, interval := range ranges {
		if interval.Start < 0 {
			return fmt.Errorf(
				"range start cannot be negative: %d",
				interval.Start,
			)
		}

		if interval.End < interval.Start {
			return fmt.Errorf(
				"invalid range: %s",
				interval,
			)
		}

		if interval.End >= totalSize {
			return fmt.Errorf(
				"range %s exceeds total size %d",
				interval,
				totalSize,
			)
		}

		if interval.Start <= previousEnd {
			return fmt.Errorf(
				"completed ranges overlap or are not sorted",
			)
		}

		previousEnd = interval.End
	}

	return nil
}

func ensureResumeDirectory(outputPath string) error {
	dir := filepath.Dir(outputPath)

	if dir == "." {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	return nil
}
