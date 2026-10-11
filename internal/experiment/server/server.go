// Package server provides a deterministic, controllable HTTP Range test server
// for experiments.
//
// It supports RFC 7233 single-range requests and lets tests change server-side
// behavior at runtime: a fixed response delay, an application-level transfer
// rate, per-range delays, and deterministic forced failures.
//
// Two independent rate-transition mechanisms are available: a request-count
// switch (SetRateAfter, e.g. "after N requests") and an experimental
// wall-clock schedule (SetRateSchedule, e.g. "after T elapsed"). The schedule
// is independent of request count, bytes, chunk size, and concurrency.
//
// The rate limit is application-level throttling (bytes / configured
// bytes-per-second), NOT network shaping or TCP emulation. Real packet loss
// belongs to a later tc/netem step.
//
// The package depends only on the standard library and knows nothing about the
// downloader, fetcher, assembler, tracker, policy, metrics, or RL.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrInvalidRange is returned by the range parser for malformed or
// unsatisfiable ranges.
var ErrInvalidRange = errors.New("server: invalid range")

// ErrInvalidRateSchedule indicates an unusable RateSchedule.
var ErrInvalidRateSchedule = errors.New("server: invalid rate schedule")

// RequestEvent records one HTTP request observed by the server. It is populated
// only when request logging is enabled (SetRequestLogEnabled) and is intended
// for experiment diagnostics; it does not affect serving behavior.
type RequestEvent struct {
	Index    int64     `json:"index"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Rate     int64     `json:"rate"`      // effective pacing rate (after the concurrency cap)
	BaseRate int64     `json:"base_rate"` // rate before the concurrency cap
	CapRate  int64     `json:"cap_rate"`  // concurrency-cap rate (0 when the cap is inactive)
	ActiveK  int64     `json:"active_k"`  // active requests at start (0 when the cap is inactive)
	Range    Range     `json:"range"`
	HasRange bool      `json:"has_range"`
}

// RateSchedule is a wall-clock rate transition for the experiment server:
// InitialRate is used until TransitionAfter has elapsed since the schedule's
// start, then SubsequentRate is used. It is independent of the number of
// requests, bytes, chunk size, and concurrency. It is experimental
// infrastructure and takes precedence over SetRateAfter while active.
type RateSchedule struct {
	InitialRate     int64
	TransitionAfter time.Duration
	SubsequentRate  int64
}

// Validate rejects an unusable schedule: both rates must be > 0 and the
// transition must be >= 0.
func (r RateSchedule) Validate() error {
	switch {
	case r.InitialRate <= 0:
		return fmt.Errorf("%w: initial rate must be > 0, got %d", ErrInvalidRateSchedule, r.InitialRate)
	case r.SubsequentRate <= 0:
		return fmt.Errorf("%w: subsequent rate must be > 0, got %d", ErrInvalidRateSchedule, r.SubsequentRate)
	case r.TransitionAfter < 0:
		return fmt.Errorf("%w: transition must be >= 0, got %v", ErrInvalidRateSchedule, r.TransitionAfter)
	}
	return nil
}

// Range is an inclusive byte range [Start, End], matching HTTP range syntax.
type Range struct {
	Start int64
	End   int64
}

// Length returns the number of bytes in the range.
func (r Range) Length() int64 {
	if r.End < r.Start {
		return 0
	}
	return r.End - r.Start + 1
}

// Stats is a snapshot of server counters. RequestedRanges is a copy.
type Stats struct {
	TotalRequests   int64
	Successful      int64
	Failed          int64
	BytesServed     int64
	RequestedRanges []Range
	// CurrentRate is the pacing rate the server would select for a request
	// starting now (wall-clock schedule if active, else the request-count
	// switch, else the fixed rate). RateScheduleActive reports whether a
	// wall-clock schedule is configured.
	CurrentRate        int64
	RateScheduleActive bool
}

// failRule is a deterministic failure condition. remaining < 0 means the rule
// applies indefinitely; remaining == 0 rules are removed.
type failRule struct {
	remaining int
	status    int
	match     func(req Range, hasRange bool, header http.Header) bool
}

// contentSource provides the bytes served by a Server. Implementations must be
// immutable after construction and safe for concurrent use.
type contentSource interface {
	// size returns the total logical size in bytes.
	size() int64
	// write writes the inclusive byte range [start, end] to w, pacing at rate
	// (bytes per second; <= 0 means unlimited). It must write exactly
	// end-start+1 bytes and reports false on client/context failure.
	write(ctx context.Context, w http.ResponseWriter, start, end, rate int64) bool
}

// finiteContent serves a private in-memory byte slice (the original behavior).
type finiteContent struct{ data []byte }

func (f finiteContent) size() int64 { return int64(len(f.data)) }

func (f finiteContent) write(ctx context.Context, w http.ResponseWriter, start, end int64, rate int64) bool {
	return writeThrottled(w, ctx, f.data[start:end+1], rate)
}

// Server is a controllable HTTP range server. It is safe for concurrent use.
type Server struct {
	content contentSource

	mu           sync.Mutex
	delay        time.Duration
	rate         int64 // bytes per second; <= 0 means unlimited
	rateSwitchAt int64 // when > 0, switch to rateAfter after this many requests
	rateAfter    int64
	rangeDelays  map[Range]time.Duration
	failRules    []failRule

	// Wall-clock rate schedule (experimental). When active it takes precedence
	// over the request-count switch and the fixed rate.
	schedule       RateSchedule
	scheduleStart  time.Time
	scheduleActive bool

	// Optional soft N-dependent service cap (experimental). When active, a
	// request's effective rate is min(base rate, C/(k*(1+gamma*(k-1)))) where
	// k is the number of active requests at the request's start (including
	// itself). C <= 0 disables it. This is a rate throttle, not a failure
	// injector: it never changes status codes or retry behavior.
	capC      int64
	capGamma  float64
	capActive bool

	// activeRequests counts requests currently being served (cap accounting).
	activeRequests atomic.Int64

	// now returns the current wall-clock time. It is injectable so tests can
	// exercise wall-clock schedules deterministically without sleeping.
	now func() time.Time

	// Opt-in per-request timing log (diagnostics only).
	logRequests bool
	requestLog  []RequestEvent

	totalRequests   int64
	successful      int64
	failed          int64
	bytesServed     int64
	requestedRanges []Range
}

// newServer assembles a Server around a content source. The source must be
// immutable after construction.
func newServer(src contentSource) *Server {
	return &Server{
		content:     src,
		rangeDelays: make(map[Range]time.Duration),
		now:         time.Now,
	}
}

// New returns a Server serving a private copy of content.
func New(content []byte) *Server {
	c := make([]byte, len(content))
	copy(c, content)
	return newServer(finiteContent{data: c})
}

// Size returns the number of bytes served.
func (s *Server) Size() int64 { return s.content.size() }

// SetDelay sets the base response delay applied before any body bytes.
func (s *Server) SetDelay(d time.Duration) {
	s.mu.Lock()
	s.delay = d
	s.mu.Unlock()
}

// SetRate sets the application-level transfer rate in bytes per second. A value
// of zero or less disables throttling.
func (s *Server) SetRate(bytesPerSecond int64) {
	s.mu.Lock()
	s.rate = bytesPerSecond
	s.mu.Unlock()
}

// SetRateAfter changes the transfer rate to bytesPerSecond once more than n
// requests have been served. The transition is deterministic and based on the
// request count, not wall-clock time: requests 1..n use the current rate and
// request n+1 onward use bytesPerSecond.
func (s *Server) SetRateAfter(n int64, bytesPerSecond int64) {
	s.mu.Lock()
	s.rateSwitchAt = n
	s.rateAfter = bytesPerSecond
	s.mu.Unlock()
}

// SetRateScheduleAt installs a wall-clock rate schedule starting at start. The
// schedule takes precedence over SetRateAfter and the fixed rate while active.
// It returns ErrInvalidRateSchedule for an unusable schedule.
func (s *Server) SetRateScheduleAt(start time.Time, schedule RateSchedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedule = schedule
	s.scheduleStart = start
	s.scheduleActive = true
	return nil
}

// SetRateSchedule installs a wall-clock rate schedule starting now (as seen by
// the server clock).
func (s *Server) SetRateSchedule(schedule RateSchedule) error {
	return s.SetRateScheduleAt(s.clock(), schedule)
}

// ClearRateSchedule removes any active wall-clock schedule. The fixed rate and
// request-count switch (if configured) then apply again.
func (s *Server) ClearRateSchedule() {
	s.mu.Lock()
	s.scheduleActive = false
	s.mu.Unlock()
}

// ErrInvalidConcurrencyCap indicates an unusable concurrency-cap configuration.
var ErrInvalidConcurrencyCap = errors.New("server: invalid concurrency cap")

// SetConcurrencyCap installs a soft N-dependent service cap: a request starting
// while k requests are active (including itself) is paced at
// min(base rate, C/(k*(1+gamma*(k-1)))). It throttles rate only; it never
// changes status codes, failures, or retry behavior. C must be > 0 and
// gamma >= 0.
func (s *Server) SetConcurrencyCap(C int64, gamma float64) error {
	if C <= 0 {
		return fmt.Errorf("%w: C must be > 0, got %d", ErrInvalidConcurrencyCap, C)
	}
	if gamma < 0 {
		return fmt.Errorf("%w: gamma must be >= 0, got %v", ErrInvalidConcurrencyCap, gamma)
	}
	s.mu.Lock()
	s.capC = C
	s.capGamma = gamma
	s.capActive = true
	s.mu.Unlock()
	return nil
}

// ClearConcurrencyCap removes the concurrency cap (default behavior).
func (s *Server) ClearConcurrencyCap() {
	s.mu.Lock()
	s.capActive = false
	s.mu.Unlock()
}

// ConcurrencyCap returns the configured cap and whether it is active.
func (s *Server) ConcurrencyCap() (int64, float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capC, s.capGamma, s.capActive
}

// ActiveRequests returns the number of requests currently being served.
func (s *Server) ActiveRequests() int64 { return s.activeRequests.Load() }

// concurrencyCapRate returns C/(k*(1+gamma*(k-1))) for k >= 1.
func concurrencyCapRate(C int64, gamma float64, k int64) int64 {
	if k < 1 {
		k = 1
	}
	den := float64(k) * (1.0 + gamma*float64(k-1))
	r := float64(C) / den
	if r < 1 {
		return 1
	}
	return int64(r)
}

// Schedule returns the configured schedule, its start time, and whether it is
// active.
func (s *Server) Schedule() (RateSchedule, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.schedule, s.scheduleStart, s.scheduleActive
}

// CurrentRate returns the pacing rate the server would select for a request
// starting now.
func (s *Server) CurrentRate() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentRateLocked(s.clock())
}

// SetRequestLogEnabled enables or disables the opt-in per-request timing log.
// Disabling discards any previously recorded events.
func (s *Server) SetRequestLogEnabled(enabled bool) {
	s.mu.Lock()
	s.logRequests = enabled
	if !enabled {
		s.requestLog = nil
	}
	s.mu.Unlock()
}

// RequestLog returns a copy of the per-request timing log. Requests still in
// flight have a zero End.
func (s *Server) RequestLog() []RequestEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RequestEvent(nil), s.requestLog...)
}

// clock returns the current time from the injectable clock.
func (s *Server) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// currentRateLocked centralizes rate selection. Precedence: an active
// wall-clock schedule, then the request-count switch, then the fixed rate. It
// must be called with s.mu held.
func (s *Server) currentRateLocked(now time.Time) int64 {
	if s.scheduleActive {
		if now.Sub(s.scheduleStart) < s.schedule.TransitionAfter {
			return s.schedule.InitialRate
		}
		return s.schedule.SubsequentRate
	}
	if s.rateSwitchAt > 0 && s.totalRequests > s.rateSwitchAt {
		return s.rateAfter
	}
	return s.rate
}

// SetRangeDelay overrides the base delay for requests matching the exact
// inclusive range [start, end].
func (s *Server) SetRangeDelay(start, end int64, d time.Duration) {
	s.mu.Lock()
	s.rangeDelays[Range{Start: start, End: end}] = d
	s.mu.Unlock()
}

// FailNext makes the next n requests fail with HTTP 500.
func (s *Server) FailNext(n int) { s.FailNextStatus(n, http.StatusInternalServerError) }

// FailNextStatus makes the next n requests fail with the given status.
func (s *Server) FailNextStatus(n, status int) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	s.failRules = append(s.failRules, failRule{
		remaining: n,
		status:    status,
		match:     func(Range, bool, http.Header) bool { return true },
	})
	s.mu.Unlock()
}

// FailRange makes every request for the exact inclusive range [start, end]
// fail with HTTP 500.
func (s *Server) FailRange(start, end int64) {
	target := Range{Start: start, End: end}
	s.mu.Lock()
	s.failRules = append(s.failRules, failRule{
		remaining: -1,
		status:    http.StatusInternalServerError,
		match: func(req Range, hasRange bool, _ http.Header) bool {
			return hasRange && req == target
		},
	})
	s.mu.Unlock()
}

// FailHeader makes every request carrying the header name with the exact value
// fail with HTTP 500.
func (s *Server) FailHeader(name, value string) {
	s.mu.Lock()
	s.failRules = append(s.failRules, failRule{
		remaining: -1,
		status:    http.StatusInternalServerError,
		match: func(_ Range, _ bool, header http.Header) bool {
			return header.Get(name) == value
		},
	})
	s.mu.Unlock()
}

// ClearFailures removes all failure rules.
func (s *Server) ClearFailures() {
	s.mu.Lock()
	s.failRules = nil
	s.mu.Unlock()
}

// Stats returns a snapshot of the counters, safe to call while requests run.
func (s *Server) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		TotalRequests:      s.totalRequests,
		Successful:         s.successful,
		Failed:             s.failed,
		BytesServed:        s.bytesServed,
		RequestedRanges:    append([]Range(nil), s.requestedRanges...),
		CurrentRate:        s.currentRateLocked(s.clock()),
		RateScheduleActive: s.scheduleActive,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	size := s.content.size()
	reqRange, hasRange, err := parseRangeHeader(r.Header.Get("Range"), size)

	s.mu.Lock()
	s.totalRequests++
	if hasRange && err == nil {
		s.requestedRanges = append(s.requestedRanges, reqRange)
	}

	status := 0
	switch {
	case hasRange && err != nil:
		status = http.StatusRequestedRangeNotSatisfiable
	default:
		if failStatus, ok := s.matchFailureLocked(reqRange, hasRange, r.Header); ok {
			status = failStatus
		}
	}

	delay := s.delay
	if hasRange && err == nil {
		if d, ok := s.rangeDelays[reqRange]; ok {
			delay = d
		}
	}
	baseRate := s.currentRateLocked(s.clock())
	rate := baseRate
	var capRate, k int64
	if s.capActive {
		k = s.activeRequests.Add(1)
		capRate = concurrencyCapRate(s.capC, s.capGamma, k)
		if baseRate <= 0 || capRate < baseRate {
			rate = capRate
		}
	}
	logIdx := -1
	if s.logRequests {
		s.requestLog = append(s.requestLog, RequestEvent{
			Index:    s.totalRequests,
			Start:    s.clock(),
			Rate:     rate,
			BaseRate: baseRate,
			CapRate:  capRate,
			ActiveK:  k,
			Range:    reqRange,
			HasRange: hasRange && err == nil,
		})
		logIdx = len(s.requestLog) - 1
	}
	s.mu.Unlock()
	if k > 0 {
		defer s.activeRequests.Add(-1)
	}

	if logIdx >= 0 {
		defer func() {
			s.mu.Lock()
			s.requestLog[logIdx].End = s.clock()
			s.mu.Unlock()
		}()
	}

	if status != 0 {
		s.recordFailure()
		if status == http.StatusRequestedRangeNotSatisfiable {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		}
		w.WriteHeader(status)
		return
	}

	start, end := int64(0), size-1
	length := size
	statusCode := http.StatusOK
	if hasRange {
		start, end = reqRange.Start, reqRange.End
		length = reqRange.Length()
		statusCode = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", reqRange.Start, reqRange.End, size))
	}

	if !sleepContext(r.Context(), delay) {
		s.recordFailure()
		return
	}

	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(statusCode)

	if !s.content.write(r.Context(), w, start, end, rate) {
		s.recordFailure()
		return
	}

	s.mu.Lock()
	s.successful++
	s.bytesServed += length
	s.mu.Unlock()
}

func (s *Server) matchFailureLocked(req Range, hasRange bool, header http.Header) (int, bool) {
	for i := range s.failRules {
		rule := &s.failRules[i]
		if rule.remaining == 0 || !rule.match(req, hasRange, header) {
			continue
		}
		if rule.remaining > 0 {
			rule.remaining--
		}
		status := rule.status
		s.compactFailRulesLocked()
		return status, true
	}
	return 0, false
}

func (s *Server) compactFailRulesLocked() {
	out := s.failRules[:0]
	for _, rule := range s.failRules {
		if rule.remaining != 0 {
			out = append(out, rule)
		}
	}
	s.failRules = out
}

func (s *Server) recordFailure() {
	s.mu.Lock()
	s.failed++
	s.mu.Unlock()
}

// parseRangeHeader parses a single "bytes=START-END" header. It returns
// (range, present, err). A missing header yields present=false. An end beyond
// the content is clamped to the last byte.
func parseRangeHeader(header string, size int64) (Range, bool, error) {
	if header == "" {
		return Range{}, false, nil
	}
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return Range{}, true, ErrInvalidRange
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if spec == "" || strings.Contains(spec, ",") {
		return Range{}, true, ErrInvalidRange
	}

	startStr, endStr, ok := strings.Cut(spec, "-")
	if !ok {
		return Range{}, true, ErrInvalidRange
	}
	start, err := strconv.ParseInt(strings.TrimSpace(startStr), 10, 64)
	if err != nil {
		return Range{}, true, ErrInvalidRange
	}

	var end int64
	if strings.TrimSpace(endStr) == "" {
		end = size - 1
	} else {
		end, err = strconv.ParseInt(strings.TrimSpace(endStr), 10, 64)
		if err != nil {
			return Range{}, true, ErrInvalidRange
		}
	}

	if start < 0 || end < start || start >= size {
		return Range{}, true, ErrInvalidRange
	}
	if end >= size {
		end = size - 1
	}
	return Range{Start: start, End: end}, true, nil
}

// writeThrottled writes body in small chunks, sleeping proportionally to
// chunk size / rate before each chunk. It is application-level pacing, not
// network shaping. It returns false if the client goes away or a write fails.
func writeThrottled(w http.ResponseWriter, ctx context.Context, body []byte, rate int64) bool {
	if rate <= 0 {
		_, err := w.Write(body)
		return err == nil
	}
	// Pace in reasonably large chunks so that per-chunk write overhead does not
	// dominate the modelled transfer time.
	const chunkSize = 16 << 10
	for off := 0; off < len(body); off += chunkSize {
		end := off + chunkSize
		if end > len(body) {
			end = len(body)
		}
		n := end - off
		d := time.Duration(float64(n) / float64(rate) * float64(time.Second))
		if !sleepContext(ctx, d) {
			return false
		}
		if _, err := w.Write(body[off:end]); err != nil {
			return false
		}
	}
	return true
}

// sleepContext sleeps for d, returning false if ctx is cancelled first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
