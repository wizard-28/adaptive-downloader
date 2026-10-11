package metrics

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRecordSuccess(t *testing.T) {
	c := New()
	c.Record(Record{
		RequestID:  "req-1",
		RangeStart: 0,
		RangeEnd:   99,
		Bytes:      100,
		Duration:   2 * time.Second,
		Throughput: 50,
		Success:    true,
		StatusCode: 206,
		Attempt:    1,
	})

	s := c.Snapshot()
	if len(s.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(s.Records))
	}
	got := s.Records[0]
	if got.RequestID != "req-1" || got.RangeStart != 0 || got.RangeEnd != 99 {
		t.Errorf("record = %+v", got)
	}
	if got.Bytes != 100 || got.Duration != 2*time.Second || got.Throughput != 50 {
		t.Errorf("record = %+v", got)
	}
	if !got.Success || got.StatusCode != 206 || got.Attempt != 1 {
		t.Errorf("record = %+v", got)
	}

	agg := s.Aggregate
	if agg.TotalBytes != 100 {
		t.Errorf("TotalBytes = %d, want 100", agg.TotalBytes)
	}
	if agg.TotalRequests != 1 || agg.TotalAttempts != 1 {
		t.Errorf("TotalRequests = %d, TotalAttempts = %d, want 1, 1", agg.TotalRequests, agg.TotalAttempts)
	}
	if agg.SuccessfulRequests != 1 || agg.FailedRequests != 0 {
		t.Errorf("Successful = %d, Failed = %d, want 1, 0", agg.SuccessfulRequests, agg.FailedRequests)
	}
	if agg.TotalDuration != 2*time.Second {
		t.Errorf("TotalDuration = %v, want 2s", agg.TotalDuration)
	}
	if agg.AverageThroughput != 50 || agg.MinThroughput != 50 || agg.MaxThroughput != 50 {
		t.Errorf("throughput = avg %v min %v max %v, want all 50", agg.AverageThroughput, agg.MinThroughput, agg.MaxThroughput)
	}
}

func TestRecordFailure(t *testing.T) {
	c := New()
	c.Record(Record{
		RequestID:  "req-1",
		RangeStart: 0,
		RangeEnd:   99,
		Bytes:      40, // partial data before failure
		Duration:   time.Second,
		Throughput: 40,
		Success:    false,
		StatusCode: 500,
		Attempt:    1,
	})

	s := c.Snapshot()
	agg := s.Aggregate
	if agg.TotalRequests != 1 || agg.TotalAttempts != 1 {
		t.Errorf("counts = %d, %d, want 1, 1", agg.TotalRequests, agg.TotalAttempts)
	}
	if agg.SuccessfulRequests != 0 || agg.FailedRequests != 1 {
		t.Errorf("Successful = %d, Failed = %d, want 0, 1", agg.SuccessfulRequests, agg.FailedRequests)
	}
	if agg.TotalBytes != 40 {
		t.Errorf("TotalBytes = %d, want 40", agg.TotalBytes)
	}
	// Failed requests do not contribute to throughput statistics.
	if agg.AverageThroughput != 0 || agg.MinThroughput != 0 || agg.MaxThroughput != 0 {
		t.Errorf("throughput = avg %v min %v max %v, want all 0", agg.AverageThroughput, agg.MinThroughput, agg.MaxThroughput)
	}
}

func TestMultipleRequests(t *testing.T) {
	c := New()
	for i := 0; i < 5; i++ {
		c.Record(Record{
			RequestID: fmt.Sprintf("req-%d", i),
			Bytes:     10,
			Duration:  time.Second,
			Success:   true,
			Attempt:   1,
		})
	}

	agg := c.Snapshot().Aggregate
	if agg.TotalBytes != 50 {
		t.Errorf("TotalBytes = %d, want 50", agg.TotalBytes)
	}
	if agg.TotalRequests != 5 || agg.TotalAttempts != 5 {
		t.Errorf("TotalRequests = %d, TotalAttempts = %d, want 5, 5", agg.TotalRequests, agg.TotalAttempts)
	}
	if agg.SuccessfulRequests != 5 || agg.FailedRequests != 0 {
		t.Errorf("Successful = %d, Failed = %d, want 5, 0", agg.SuccessfulRequests, agg.FailedRequests)
	}
	if agg.TotalDuration != 5*time.Second {
		t.Errorf("TotalDuration = %v, want 5s", agg.TotalDuration)
	}
}

func TestRetryAttempts(t *testing.T) {
	c := New()
	c.Record(Record{RequestID: "req-1", Bytes: 0, Duration: time.Second, Success: false, StatusCode: 500, Attempt: 1})
	c.Record(Record{RequestID: "req-1", Bytes: 100, Duration: time.Second, Throughput: 100, Success: true, StatusCode: 206, Attempt: 2})

	s := c.Snapshot()
	if len(s.Records) != 2 {
		t.Fatalf("len(Records) = %d, want 2", len(s.Records))
	}
	if s.Records[0].Attempt != 1 || s.Records[1].Attempt != 2 {
		t.Errorf("attempts = %d, %d, want 1, 2", s.Records[0].Attempt, s.Records[1].Attempt)
	}
	agg := s.Aggregate
	if agg.TotalAttempts != 2 {
		t.Errorf("TotalAttempts = %d, want 2", agg.TotalAttempts)
	}
	if agg.SuccessfulRequests != 1 || agg.FailedRequests != 1 {
		t.Errorf("Successful = %d, Failed = %d, want 1, 1", agg.SuccessfulRequests, agg.FailedRequests)
	}
	if agg.TotalBytes != 100 {
		t.Errorf("TotalBytes = %d, want 100", agg.TotalBytes)
	}
}

func TestThroughputCalculations(t *testing.T) {
	c := New()
	c.Record(Record{Bytes: 1000, Duration: time.Second, Throughput: 1000, Success: true})
	c.Record(Record{Bytes: 4000, Duration: time.Second, Throughput: 4000, Success: true})
	// A failed request must not skew the throughput statistics.
	c.Record(Record{Bytes: 0, Duration: 2 * time.Second, Throughput: 0, Success: false})

	agg := c.Snapshot().Aggregate
	if agg.TotalBytes != 5000 {
		t.Errorf("TotalBytes = %d, want 5000", agg.TotalBytes)
	}
	if agg.TotalDuration != 4*time.Second {
		t.Errorf("TotalDuration = %v, want 4s", agg.TotalDuration)
	}
	// Average over successful requests: (1000+4000) / (1s+1s) = 2500.
	if agg.AverageThroughput != 2500 {
		t.Errorf("AverageThroughput = %v, want 2500", agg.AverageThroughput)
	}
	if agg.MinThroughput != 1000 {
		t.Errorf("MinThroughput = %v, want 1000", agg.MinThroughput)
	}
	if agg.MaxThroughput != 4000 {
		t.Errorf("MaxThroughput = %v, want 4000", agg.MaxThroughput)
	}
}

func TestConcurrentRecording(t *testing.T) {
	c := New()
	const (
		goroutines = 16
		perWorker  = 100
	)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				c.Record(Record{
					RequestID:  fmt.Sprintf("g%d-r%d", id, i),
					RangeStart: int64(i),
					RangeEnd:   int64(i),
					Bytes:      1,
					Duration:   time.Millisecond,
					Throughput: 1000,
					Success:    i%10 != 0,
					StatusCode: 206,
					Attempt:    1,
				})
			}
		}(g)
	}
	wg.Wait()

	s := c.Snapshot()
	wantTotal := int64(goroutines * perWorker)
	if s.Aggregate.TotalRequests != wantTotal || s.Aggregate.TotalAttempts != wantTotal {
		t.Fatalf("TotalRequests = %d, TotalAttempts = %d, want %d", s.Aggregate.TotalRequests, s.Aggregate.TotalAttempts, wantTotal)
	}
	if len(s.Records) != int(wantTotal) {
		t.Fatalf("len(Records) = %d, want %d", len(s.Records), wantTotal)
	}
	if s.Aggregate.TotalBytes != wantTotal {
		t.Fatalf("TotalBytes = %d, want %d", s.Aggregate.TotalBytes, wantTotal)
	}
	if s.Aggregate.SuccessfulRequests+s.Aggregate.FailedRequests != wantTotal {
		t.Fatalf("success+fail = %d, want %d", s.Aggregate.SuccessfulRequests+s.Aggregate.FailedRequests, wantTotal)
	}
	// Throughput stats come only from successful records, all at 1000 B/s.
	if s.Aggregate.MinThroughput != 1000 || s.Aggregate.MaxThroughput != 1000 {
		t.Fatalf("throughput = min %v max %v, want 1000/1000", s.Aggregate.MinThroughput, s.Aggregate.MaxThroughput)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	c := New()
	c.Record(Record{RequestID: "req-1", Bytes: 100, Duration: time.Second, Throughput: 100, Success: true})

	first := c.Snapshot()

	// Recording more requests must not change an earlier snapshot.
	c.Record(Record{RequestID: "req-2", Bytes: 200, Duration: time.Second, Throughput: 200, Success: true})
	if len(first.Records) != 1 || first.Aggregate.TotalBytes != 100 {
		t.Fatalf("earlier snapshot changed: %+v", first.Aggregate)
	}

	// Mutating a snapshot must not affect the collector or later snapshots.
	first.Records[0].Bytes = 9999
	first.Aggregate.TotalBytes = 9999
	first.Aggregate.SuccessfulRequests = 9999

	second := c.Snapshot()
	if second.Aggregate.TotalBytes != 300 {
		t.Fatalf("TotalBytes = %d, want 300", second.Aggregate.TotalBytes)
	}
	if second.Aggregate.SuccessfulRequests != 2 {
		t.Fatalf("SuccessfulRequests = %d, want 2", second.Aggregate.SuccessfulRequests)
	}
	if second.Records[0].Bytes != 100 {
		t.Fatalf("record bytes = %d, want 100", second.Records[0].Bytes)
	}
}

func TestZeroValueCollector(t *testing.T) {
	var c Collector
	c.Record(Record{Bytes: 7, Duration: time.Second, Throughput: 7, Success: true})

	agg := c.Snapshot().Aggregate
	if agg.TotalBytes != 7 || agg.TotalRequests != 1 {
		t.Fatalf("aggregate = %+v", agg)
	}
}
