package tau

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Event is one raw child byte event from the JSONL timeline, retained exactly
// as written by the replay child.
type Event struct {
	SinceStartNanos int64  `json:"since_start_ns"`
	WallUnixNano    int64  `json:"wall_unix_nano"`
	Delta           int64  `json:"delta"`
	Cumulative      int64  `json:"cumulative"`
	ConnID          string `json:"conn_id,omitempty"`
}

// LoadEvents reads a child JSONL event timeline.
func LoadEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("tau: open events: %w", err)
	}
	defer f.Close()
	var out []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("tau: decode event: %w", err)
		}
		out = append(out, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("tau: read events: %w", err)
	}
	return out, nil
}

// WindowBytes sums read-time deltas whose trace time falls in the half-open
// window [start, end). traceStartWallUnixNano is the frozen anchor origin
// (mm-link launch wall + StartupOffset). Completion timestamps are never used.
func WindowBytes(events []Event, traceStartWallUnixNano int64, start, end time.Duration) int64 {
	var total int64
	for _, e := range events {
		if e.Delta <= 0 {
			continue
		}
		t := time.Duration(e.WallUnixNano - traceStartWallUnixNano)
		if t >= start && t < end {
			total += e.Delta
		}
	}
	return total
}

// TauSeconds applies the frozen transition-cost formula for one injection: the
// switch-induced byte deficit over the identical window, expressed as the time
// the matched control rate needs to deliver that deficit (seconds of
// equivalent B_e = 5 MiB completion time). Negative deficits are preserved.
func TauSeconds(switchedBytes, controlBytes int64, window time.Duration) (tau float64, rate float64, err error) {
	if window <= 0 {
		return 0, 0, fmt.Errorf("tau: window must be positive")
	}
	if controlBytes <= 0 {
		return 0, 0, fmt.Errorf("tau: zero control bytes: denominator undefined")
	}
	rate = float64(controlBytes) / window.Seconds()
	return float64(controlBytes-switchedBytes) / rate, rate, nil
}
