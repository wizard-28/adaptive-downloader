package fetcher

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWatchdogOnlyConfigDisablesRequestAndDialDeadlines verifies the explicit
// no-timeout configuration produces an HTTP client without a request deadline
// and a dialer without a dial deadline, while the default behavior is
// unchanged.
func TestWatchdogOnlyConfigDisablesRequestAndDialDeadlines(t *testing.T) {
	f := New(Config{NoRequestTimeout: true, NoDialTimeout: true})
	if f.client.Timeout != 0 {
		t.Fatalf("client.Timeout = %v, want 0 (no request deadline)", f.client.Timeout)
	}
	tr, ok := f.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", f.client.Transport)
	}
	if tr.ResponseHeaderTimeout != 0 {
		t.Fatalf("ResponseHeaderTimeout = %v, want 0", tr.ResponseHeaderTimeout)
	}
	if tr.DialContext == nil {
		t.Fatal("DialContext = nil")
	}
	if got := newDialer(0).Timeout; got != 0 {
		t.Fatalf("dialer timeout = %v, want 0 (no dial deadline)", got)
	}
	if got := newDialer(defaultDialTimeout).Timeout; got != defaultDialTimeout {
		t.Fatalf("default dialer timeout = %v, want %v", got, defaultDialTimeout)
	}
	if ka := newDialer(0).KeepAlive; ka <= 0 {
		t.Fatalf("dialer keepalive = %v, want > 0", ka)
	}
	// The zero-value configuration keeps the historical defaults.
	def := New(Config{})
	if def.client.Timeout != DefaultTimeout {
		t.Fatalf("default client.Timeout = %v, want %v", def.client.Timeout, DefaultTimeout)
	}
}

// TestTimeoutConfigValidation rejects ambiguous combinations so "no deadline"
// can never be confused with "default deadline".
func TestTimeoutConfigValidation(t *testing.T) {
	invalid := map[string]Config{
		"request flag plus timeout": {NoRequestTimeout: true, Timeout: time.Second},
		"dial flag plus timeout":    {NoDialTimeout: true, DialTimeout: time.Second},
		"negative request timeout":  {Timeout: -time.Second},
		"negative dial timeout":     {DialTimeout: -time.Second},
		"no-timeout with client":    {Client: &http.Client{}, NoRequestTimeout: true},
		"no-dial with client":       {Client: &http.Client{}, NoDialTimeout: true},
	}
	for name, cfg := range invalid {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate = nil, want error", name)
		}
	}
	valid := []Config{
		{},
		{Timeout: time.Second},
		{NoRequestTimeout: true, NoDialTimeout: true},
		{Client: &http.Client{}, Timeout: time.Second}, // ignored, historically allowed
	}
	for i, cfg := range valid {
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid[%d]: Validate = %v", i, err)
		}
	}
}

// TestNoRequestTimeoutHasNoClientLocalDeadline proves behaviorally that the
// no-timeout mode adds no hidden client-local deadline: a response delayed
// longer than a would-be deadline still succeeds, while an explicit short
// timeout still fails.
func TestNoRequestTimeoutHasNoClientLocalDeadline(t *testing.T) {
	const delay = 200 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		body := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		w.Header().Set("Content-Range", "bytes 0-9/10")
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	res := New(Config{NoRequestTimeout: true, NoDialTimeout: true}).
		Fetch(context.Background(), "id", srv.URL, 0, 9)
	if !res.Success {
		t.Fatalf("no-timeout fetch failed: %v", res.Err)
	}
	if res.Duration < delay {
		t.Fatalf("fetch returned in %v, before the server delay %v", res.Duration, delay)
	}

	deadlined := New(Config{Timeout: 20 * time.Millisecond}).
		Fetch(context.Background(), "id", srv.URL, 0, 9)
	if deadlined.Success {
		t.Fatal("deadlined fetch succeeded, want timeout")
	}
	if !errors.Is(deadlined.Err, ErrTimeout) {
		t.Fatalf("deadlined err = %v, want ErrTimeout", deadlined.Err)
	}
}
