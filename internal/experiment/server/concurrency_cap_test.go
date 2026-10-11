package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestConcurrencyCapRate(t *testing.T) {
	const mib = 1 << 20
	C := int64(8 * mib)
	if got := concurrencyCapRate(C, 0.05, 1); got != C {
		t.Fatalf("k=1 rate = %d, want %d", got, C)
	}
	want4 := int64(float64(C) / (4 * (1 + 0.05*3)))
	if got := concurrencyCapRate(C, 0.05, 4); got != want4 {
		t.Fatalf("k=4 rate = %d, want %d", got, want4)
	}
	if concurrencyCapRate(C, 0.05, 16) >= concurrencyCapRate(C, 0.05, 8) {
		t.Fatalf("cap rate must decrease with k")
	}
	if got := concurrencyCapRate(C, 0.05, 0); got != C {
		t.Fatalf("k=0 must clamp to k=1, got %d", got)
	}
}

func TestConcurrencyCapValidation(t *testing.T) {
	s := New([]byte("abc"))
	if err := s.SetConcurrencyCap(0, 0.1); err == nil {
		t.Fatal("C=0 must be rejected")
	}
	if err := s.SetConcurrencyCap(1<<20, -0.1); err == nil {
		t.Fatal("negative gamma must be rejected")
	}
	if err := s.SetConcurrencyCap(1<<20, 0.05); err != nil {
		t.Fatalf("valid cap rejected: %v", err)
	}
	if C, g, active := s.ConcurrencyCap(); !active || C != 1<<20 || g != 0.05 {
		t.Fatalf("cap = (%d, %v, %v), want (1048576, 0.05, true)", C, g, active)
	}
	s.ClearConcurrencyCap()
	if _, _, active := s.ConcurrencyCap(); active {
		t.Fatal("cap must be inactive after ClearConcurrencyCap")
	}
}

func TestConcurrencyCapDisabledByDefault(t *testing.T) {
	content := make([]byte, 4096)
	s := New(content)
	s.SetRate(1 << 20)
	s.SetRequestLogEnabled(true)
	ts := httptest.NewServer(s)
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	log := s.RequestLog()
	if len(log) != 1 {
		t.Fatalf("log len = %d, want 1", len(log))
	}
	if log[0].ActiveK != 0 || log[0].CapRate != 0 || log[0].BaseRate != log[0].Rate {
		t.Fatalf("default log = %+v, want no cap fields", log[0])
	}
}

func TestConcurrencyCapConcurrent(t *testing.T) {
	const mib = 1 << 20
	content := make([]byte, 64*1024)
	s := New(content)
	s.SetRate(100 * mib) // base far above the cap so the cap dominates
	s.SetDelay(120 * time.Millisecond)
	s.SetRequestLogEnabled(true)
	if err := s.SetConcurrencyCap(8*mib, 0.05); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	const k = 4
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, k)
	for i := 0; i < k; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, err := http.Get(ts.URL)
			if err != nil {
				errs <- err
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	log := s.RequestLog()
	if len(log) != k {
		t.Fatalf("log len = %d, want %d", len(log), k)
	}
	seen := map[int64]bool{}
	for _, e := range log {
		seen[e.ActiveK] = true
		want := concurrencyCapRate(8*mib, 0.05, e.ActiveK)
		if e.CapRate != want || e.Rate != want {
			t.Fatalf("k=%d rate=%d cap=%d want %d", e.ActiveK, e.Rate, e.CapRate, want)
		}
		if e.BaseRate != 100*mib {
			t.Fatalf("base rate = %d, want %d", e.BaseRate, 100*mib)
		}
	}
	for i := int64(1); i <= k; i++ {
		if !seen[i] {
			t.Fatalf("active k=%d never observed (log: %s)", i, fmt.Sprint(log))
		}
	}
	if got := s.ActiveRequests(); got != 0 {
		t.Fatalf("active requests after completion = %d, want 0", got)
	}
}

func TestConcurrencyCapDoesNotAffectFailures(t *testing.T) {
	s := New([]byte("abc"))
	if err := s.SetConcurrencyCap(1<<20, 0.05); err != nil {
		t.Fatal(err)
	}
	s.FailNext(1)
	ts := httptest.NewServer(s)
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if s.ActiveRequests() != 0 {
		t.Fatal("active count must return to zero after a failed request")
	}
	_ = context.Background()
}
