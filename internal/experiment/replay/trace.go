// Package replay implements the experiment-only Mahimahi Stage 0 replay
// harness: it starts a logical HTTP Range workload, launches an mm-link
// --once replay with a downloader child, treats trace EOF as the measurement
// endpoint, and enforces an independent watchdog and cleanup.
//
// It performs no trace-time anchoring, epoch construction, or research
// measurement: it only provides the raw lifecycle/measurement plumbing.
package replay

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// TraceInfo describes a Mahimahi delivery-opportunity trace.
type TraceInfo struct {
	Path     string
	Lines    int
	Duration time.Duration
}

// ReadTrace parses a Mahimahi trace file: one delivery-opportunity timestamp
// per line, in milliseconds since trace start. Blank lines and lines starting
// with '#' are ignored. The trace duration is the largest timestamp.
func ReadTrace(path string) (TraceInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return TraceInfo{}, fmt.Errorf("replay: open trace: %w", err)
	}
	defer f.Close()

	info := TraceInfo{Path: path}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var lastMs int64
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ms, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return TraceInfo{}, fmt.Errorf("replay: trace %s line %d: %w", path, n, err)
		}
		if ms < 0 {
			return TraceInfo{}, fmt.Errorf("replay: trace %s line %d: negative timestamp %d", path, n, ms)
		}
		if ms > lastMs {
			lastMs = ms
		}
		info.Lines++
	}
	if err := scanner.Err(); err != nil {
		return TraceInfo{}, fmt.Errorf("replay: read trace %s: %w", path, err)
	}
	if info.Lines == 0 {
		return TraceInfo{}, fmt.Errorf("replay: trace %s has no delivery opportunities", path)
	}
	info.Duration = time.Duration(lastMs) * time.Millisecond
	if info.Duration <= 0 {
		return TraceInfo{}, fmt.Errorf("replay: trace %s must last a nonzero amount of time", path)
	}
	return info, nil
}
