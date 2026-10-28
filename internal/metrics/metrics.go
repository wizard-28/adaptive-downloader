// Package metrics records per-request and aggregate download measurements for
// experiments. It is observation-only: nothing here influences downloader
// behavior.
//
// The package is self-contained and depends only on the standard library.
package metrics

import (
	"sync"
	"time"
)

// Record describes one completed request attempt.
type Record struct {
	RequestID  string
	RangeStart int64
	RangeEnd   int64
	Bytes      int64
	Duration   time.Duration
	Latency    time.Duration // time until response headers arrived; 0 if none
	Throughput float64       // bytes per second
	Success    bool
	StatusCode int
	Attempt    int // 1 for the first attempt, 2 for the first retry, ...
}

// Aggregate holds cumulative measurements over all recorded requests.
//
// AverageThroughput, MinThroughput, and MaxThroughput are computed from
// successful requests only, so failed attempts do not skew them.
type Aggregate struct {
	TotalBytes         int64
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64
	TotalAttempts      int64
	TotalDuration      time.Duration
	AverageThroughput  float64
	MinThroughput      float64
	MaxThroughput      float64
}

// Snapshot is an immutable copy of the collector's state at a point in time.
type Snapshot struct {
	Records   []Record
	Aggregate Aggregate
}

// Collector accumulates request records. It is safe for concurrent use.
//
// The zero value is ready to use.
type Collector struct {
	mu sync.Mutex

	records []Record

	totalBytes     int64
	totalRequests  int64
	successful     int64
	failed         int64
	totalAttempts  int64
	totalDuration  time.Duration
	successBytes   int64
	successElapsed time.Duration

	haveThroughput bool
	minThroughput  float64
	maxThroughput  float64
}

// New returns an empty Collector.
func New() *Collector { return &Collector{} }

// Record stores one completed request. It is safe to call concurrently.
//
// Exactly one Record is stored per attempt, including failures, cancellations
// and incomplete bodies: the worker records before it decides success, so the
// attempt history is complete. Failed attempts still count towards TotalBytes
// and TotalRequests (bytes actually received), but not towards the throughput
// statistics, which describe successful transfers only.
func (c *Collector) Record(r Record) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.records = append(c.records, r)
	c.totalBytes += r.Bytes
	c.totalRequests++
	c.totalAttempts++
	if r.Duration > 0 {
		c.totalDuration += r.Duration
	}

	if r.Success {
		c.successful++
		c.successBytes += r.Bytes
		c.successElapsed += r.Duration
		if r.Throughput > 0 {
			if !c.haveThroughput || r.Throughput < c.minThroughput {
				c.minThroughput = r.Throughput
			}
			if !c.haveThroughput || r.Throughput > c.maxThroughput {
				c.maxThroughput = r.Throughput
			}
			c.haveThroughput = true
		}
		return
	}
	c.failed++
}

// Snapshot returns a copy of the collected records and aggregate measurements.
// Mutating the returned Snapshot never affects the Collector.
//
// AverageThroughput is byte-weighted (successful bytes / successful elapsed
// time), not the mean of the per-request throughputs, so one long request does
// not dominate the average. MinThroughput/MaxThroughput are over successful
// attempts only, and stay 0 when no successful attempt reported a rate.
func (c *Collector) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	records := make([]Record, len(c.records))
	copy(records, c.records)

	agg := Aggregate{
		TotalBytes:         c.totalBytes,
		TotalRequests:      c.totalRequests,
		SuccessfulRequests: c.successful,
		FailedRequests:     c.failed,
		TotalAttempts:      c.totalAttempts,
		TotalDuration:      c.totalDuration,
		MinThroughput:      c.minThroughput,
		MaxThroughput:      c.maxThroughput,
	}
	if c.successElapsed > 0 {
		agg.AverageThroughput = float64(c.successBytes) / c.successElapsed.Seconds()
	}

	return Snapshot{Records: records, Aggregate: agg}
}
