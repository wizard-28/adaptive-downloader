package replay

import (
	"bufio"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Canonical trace-time anchor.
//
// Mahimahi's trace clock starts when mm-link starts replaying the trace, which
// is not an observable instant. The canonical definition used here is:
//
//	trace_start (t=0) = mm-link launch + StartupOffset
//
// where StartupOffset is a constant measured by the synthetic calibration
// experiment (`TestAnchorCalibration`) from traces with known delivery-window
// edges, and AlignmentTolerance bounds the observed |prediction residual|.
// Trace time is then `application_monotonic_time - trace_start`; first-byte
// arrival is a measured event, never the origin.
//
// The constants are frozen from calibration and must not be changed to make
// later epoch/oracle results convenient.
const (
	// StartupOffset is the measured mean delay from launching mm-link to
	// Mahimahi trace t=0: calibration over 9 runs (C={1,4,16}, 3 each) gave
	// mean 59.6 ms, median 52.2 ms, min 45.4 ms, max 132.5 ms, SD 26.0 ms.
	// Frozen at 60 ms.
	StartupOffset = 60 * time.Millisecond
	// AlignmentTolerance bounds |observed - predicted| alignment residual.
	// The worst absolute second-edge residual observed in calibration was
	// 73.6 ms; frozen conservatively at 100 ms. This value is based only on
	// the calibration precision and must not be relaxed for later epoch or
	// oracle convenience.
	AlignmentTolerance = 100 * time.Millisecond
)

// buildAnchor assembles the machine-readable anchor record from raw
// timestamps. Derived fields are reproducible from the raw fields and the
// frozen constants.
func buildAnchor(mml MMLinkResult, traceDuration time.Duration, summary *ChildSummary, downlinkLogPath string) *Anchor {
	if mml.LaunchedAt.IsZero() {
		return nil
	}
	a := &Anchor{
		LaunchParentMono:     mml.LaunchedAt,
		ExitParentMono:       mml.EndedAt,
		TraceDuration:        traceDuration,
		TraceEOF:             traceDuration,
		StartupOffset:        StartupOffset,
		AlignmentTolerance:   AlignmentTolerance,
		TraceStartParentMono: PredictedTraceStart(mml.LaunchedAt),
	}
	if downlinkLogPath != "" {
		if lines, lastMs, err := ParseTraceLog(downlinkLogPath); err == nil {
			a.DownlinkLogLines = lines
			a.DownlinkLogLastMs = lastMs
		}
	}
	if summary != nil {
		a.ChildStartWallUnixNano = summary.StartWallUnixNano
		a.HasFirstByte = summary.HasFirstByte
		a.FirstByteSinceStartNanos = summary.FirstByteSinceStartNanos
		a.FirstByteWallUnixNano = summary.FirstByteWallUnixNano
		if summary.HasFirstByte {
			// Bridge the child wall timestamp into the parent monotonic clock
			// using the launch pair (both processes share the kernel wall
			// clock). Wall is used only as a cross-process epoch; durations
			// stay monotonic.
			firstByteParent := mml.LaunchedAt.Add(time.Duration(summary.FirstByteWallUnixNano - mml.LaunchedAt.UnixNano()))
			a.FirstByteTraceTime = TraceTime(firstByteParent, a.TraceStartParentMono)
		}
	}
	return a
}

// Event is one raw child byte observation, preserved for anchor reconstruction.
// SinceStartNanos is monotonic elapsed time inside the child process.
type Event struct {
	SinceStartNanos int64  `json:"since_start_ns"`
	WallUnixNano    int64  `json:"wall_unix_nano"`
	Delta           int64  `json:"delta"`
	Cumulative      int64  `json:"cumulative"`
	ConnID          string `json:"conn_id,omitempty"`
}

// Anchor is the machine-readable trace-time anchor record for one replay run.
// Raw timestamps are retained; derived fields are reproducible from them plus
// the frozen calibration constants.
type Anchor struct {
	// Raw parent-side (monotonic) timestamps.
	LaunchParentMono time.Time `json:"launch_parent_mono"`
	ExitParentMono   time.Time `json:"exit_parent_mono"`

	// Raw child-side timestamps from the child summary; wall values exist
	// only to bridge process clocks, never for duration arithmetic.
	ChildStartWallUnixNano   int64 `json:"child_start_wall_unix_nano"`
	HasFirstByte             bool  `json:"has_first_byte"`
	FirstByteSinceStartNanos int64 `json:"first_byte_since_start_ns"`
	FirstByteWallUnixNano    int64 `json:"first_byte_wall_unix_nano"`

	// Trace structure / EOF endpoint.
	TraceDuration     time.Duration `json:"trace_duration"`
	DownlinkLogLines  int64         `json:"downlink_log_lines,omitempty"`
	DownlinkLogLastMs int64         `json:"downlink_log_last_ms,omitempty"`

	// Derived mapping (parent clock).
	StartupOffset        time.Duration `json:"startup_offset"`
	AlignmentTolerance   time.Duration `json:"alignment_tolerance"`
	TraceStartParentMono time.Time     `json:"trace_start_parent_mono"`
	TraceEOF             time.Duration `json:"trace_eof"`

	// First-byte trace time: a measurement derived through the anchor, not the
	// origin.
	FirstByteTraceTime time.Duration `json:"first_byte_trace_time"`
}

// PredictedTraceStart returns the parent-monotonic instant of trace t=0.
func PredictedTraceStart(launch time.Time) time.Time {
	return launch.Add(StartupOffset)
}

// TraceTime maps an application instant to trace time using the canonical
// anchor. Both operands carry Go monotonic readings when they come from
// time.Now, so this is monotonic-clock arithmetic.
func TraceTime(event, traceStart time.Time) time.Duration {
	return event.Sub(traceStart)
}

// WithinTolerance reports whether an alignment residual is inside the frozen
// tolerance (boundary inclusive).
func WithinTolerance(residual time.Duration) bool {
	if residual < 0 {
		residual = -residual
	}
	return residual <= AlignmentTolerance
}

// StartupOffsetSample computes the ground-truth launch->t=0 offset from an
// event whose trace time is known from the trace structure (a synthetic
// delivery-window edge). Wall differences bridge the parent and child process
// clocks; all other arithmetic is duration-based.
func StartupOffsetSample(launchWallUnixNano, childStartWallUnixNano int64, eventSinceStart time.Duration, edgeTraceTime time.Duration) time.Duration {
	launchInChild := time.Duration(launchWallUnixNano - childStartWallUnixNano)
	trace0InChild := eventSinceStart - edgeTraceTime
	return trace0InChild - launchInChild
}

// ResumeTimes returns the first event of every active cluster, i.e. each event
// preceded by a quiet gap of at least minGap. The first cluster is not
// returned (it has no preceding gap). Times are since-start nanoseconds.
func ResumeTimes(events []Event, minGap time.Duration) []int64 {
	var out []int64
	for i := 1; i < len(events); i++ {
		if time.Duration(events[i].SinceStartNanos-events[i-1].SinceStartNanos) >= minGap {
			out = append(out, events[i].SinceStartNanos)
		}
	}
	return out
}

// Stats summarizes a set of calibration durations.
type Stats struct {
	N      int           `json:"n"`
	Mean   time.Duration `json:"mean"`
	Median time.Duration `json:"median"`
	Min    time.Duration `json:"min"`
	Max    time.Duration `json:"max"`
	StdDev time.Duration `json:"stddev"`
}

// Summarize computes descriptive statistics over durations. It returns a zero
// Stats for an empty slice.
func Summarize(ds []time.Duration) Stats {
	if len(ds) == 0 {
		return Stats{}
	}
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum float64
	for _, d := range ds {
		sum += float64(d)
	}
	mean := sum / float64(len(ds))
	var sq float64
	for _, d := range ds {
		diff := float64(d) - mean
		sq += diff * diff
	}
	return Stats{
		N:      len(ds),
		Mean:   time.Duration(mean),
		Median: sorted[len(sorted)/2],
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		StdDev: time.Duration(math.Sqrt(sq / float64(len(ds)))),
	}
}

// Residual returns offset minus the summary mean.
func (s Stats) Residual(offset time.Duration) time.Duration { return offset - s.Mean }

// ParseTraceLog reads an mm-link uplink/downlink log and returns the number of
// packet records and the last trace-relative timestamp in milliseconds.
// Header lines beginning with '#' are ignored. A missing file returns 0, 0,
// nil so log capture remains optional.
func ParseTraceLog(path string) (lines int64, lastMs int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		msStr, _, ok := strings.Cut(line, "#")
		if !ok {
			continue
		}
		ms, err := strconv.ParseInt(strings.TrimSpace(msStr), 10, 64)
		if err != nil {
			continue
		}
		lines++
		if ms > lastMs {
			lastMs = ms
		}
	}
	if err := scanner.Err(); err != nil {
		return lines, lastMs, err
	}
	return lines, lastMs, nil
}
