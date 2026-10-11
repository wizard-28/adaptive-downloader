package fetcher

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// ByteEvent is one incremental received-byte observation taken at the moment
// bytes were actually read from an HTTP response body.
//
// Time is the read time from time.Now (which carries a monotonic clock
// reading). RequestCumulative is the number of response-body bytes read for
// RequestID so far; AggregateCumulative is the total across all requests seen
// by the observer. Delta is the bytes read in this call and is always > 0.
type ByteEvent struct {
	Time                time.Time
	RequestID           string
	ConnID              string
	Delta               int64
	RequestCumulative   int64
	AggregateCumulative int64
}

// ByteObserver receives incremental byte events from the response-body read
// path. Implementations must be safe for concurrent use when a Fetcher is used
// from multiple goroutines. ObserveBytes is called synchronously and must not
// block for long.
type ByteObserver interface {
	ObserveBytes(ByteEvent)
}

// ByteMeter is a concurrency-safe ByteObserver that retains every event in
// memory and maintains the aggregate cumulative byte count.
//
// The meter stamps AggregateCumulative in append order. To keep the stream's
// timestamps monotonically nondecreasing even when independent requests' reads
// complete slightly out of scheduler order, a timestamp earlier than the
// previous event's is clamped to the previous timestamp; per-request read
// timestamps are otherwise preserved.
type ByteMeter struct {
	mu        sync.Mutex
	events    []ByteEvent
	aggregate int64
	last      time.Time
}

// NewByteMeter returns an empty meter.
func NewByteMeter() *ByteMeter { return &ByteMeter{} }

// ObserveBytes appends e (stamping AggregateCumulative) and implements
// ByteObserver.
func (m *ByteMeter) ObserveBytes(e ByteEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.aggregate += e.Delta
	e.AggregateCumulative = m.aggregate
	if !m.last.IsZero() && e.Time.Before(m.last) {
		e.Time = m.last
	}
	m.last = e.Time
	m.events = append(m.events, e)
}

// Snapshot returns a copy of the recorded events in append order.
func (m *ByteMeter) Snapshot() []ByteEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ByteEvent(nil), m.events...)
}

// Total returns the cumulative bytes observed so far.
func (m *ByteMeter) Total() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.aggregate
}

// connIDHolder carries the connection identity captured by httptrace before
// the response body is read. It is safe for concurrent use because the trace
// callback and the body reader may run on different goroutines.
type connIDHolder struct {
	addr atomic.Pointer[string]
}

func (h *connIDHolder) set(addr string) { h.addr.Store(&addr) }

func (h *connIDHolder) get() string {
	if p := h.addr.Load(); p != nil {
		return *p
	}
	return ""
}

// countingReader wraps a response body and reports each successful Read as an
// incremental ByteEvent. It preserves the underlying reader's n/err contract
// exactly: bytes are counted before any accompanying error is returned, so a
// partial read that ends in an error still contributes its bytes.
type countingReader struct {
	r         io.Reader
	requestID string
	conn      *connIDHolder
	observer  ByteObserver
	read      int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		now := time.Now()
		c.read += int64(n)
		c.observer.ObserveBytes(ByteEvent{
			Time:              now,
			RequestID:         c.requestID,
			ConnID:            c.conn.get(),
			Delta:             int64(n),
			RequestCumulative: c.read,
		})
	}
	return n, err
}
