package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(string) slog.Handler { return h }

func (h *captureHandler) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}

func newCapturedServer(t *testing.T) (*Server, *captureHandler) {
	t.Helper()

	s := testServer()
	cap := &captureHandler{}

	s.logger = slog.New(cap)
	s.requestSeq.Store(0)

	return s, cap
}

func recordByMessage(t *testing.T, cap *captureHandler, msg string) slog.Record {
	t.Helper()

	cap.mu.Lock()
	defer cap.mu.Unlock()

	var last slog.Record
	found := false

	for _, r := range cap.records {
		if r.Message == msg {
			last = r
			found = true
		}
	}

	if found {
		return last
	}

	t.Fatalf("no recorded log message %q (have %d records)", msg, len(cap.records))
	return slog.Record{}
}

func attrValue(t *testing.T, rec slog.Record, key string) slog.Value {
	t.Helper()

	var value slog.Value
	found := false

	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			value = a.Value
			found = true
			return false
		}
		return true
	})

	if !found {
		t.Fatalf("record %q has no attribute %q", rec.Message, key)
	}

	return value
}

var errSimulatedWrite = errors.New("boom")

type failWriter struct {
	header http.Header
}

func newFailWriter() *failWriter {
	return &failWriter{header: make(http.Header)}
}

func (f *failWriter) Header() http.Header { return f.header }

func (f *failWriter) WriteHeader(int) {}

func (f *failWriter) Write([]byte) (int, error) {
	return 0, errSimulatedWrite
}

func TestResponseRecorder(t *testing.T) {
	t.Run("captures first WriteHeader only", func(t *testing.T) {
		rw := &responseRecorder{ResponseWriter: httptest.NewRecorder()}

		rw.WriteHeader(http.StatusNotFound)
		assert.Equal(t, http.StatusNotFound, rw.statusCode)

		rw.WriteHeader(http.StatusOK)
		assert.Equal(t, http.StatusNotFound, rw.statusCode)
	})

	t.Run("accumulates bytes across writes", func(t *testing.T) {
		rw := &responseRecorder{ResponseWriter: httptest.NewRecorder()}

		_, _ = rw.Write([]byte("hello"))
		_, _ = rw.Write([]byte(" world"))

		assert.Equal(t, int64(11), rw.bytes)
	})

	t.Run("records the first write error", func(t *testing.T) {
		rw := &responseRecorder{ResponseWriter: newFailWriter()}

		_, err := rw.Write([]byte("x"))

		require.Error(t, err)
		assert.Same(t, errSimulatedWrite, rw.writeError)
	})

	t.Run("Unwrap returns the original writer", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseRecorder{ResponseWriter: rec}

		assert.Same(t, rec, rw.Unwrap())
	})
}

func TestMiddleware_startedAndCompleted(t *testing.T) {
	s, cap := newCapturedServer(t)

	handler := s.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	started := recordByMessage(t, cap, "request started")
	completed := recordByMessage(t, cap, "request completed")

	assert.Equal(t, slog.LevelDebug, started.Level)
	assert.Equal(t, slog.LevelInfo, completed.Level)

	assert.Equal(t, "req-0000001", attrValue(t, started, "request_id").String())
	assert.Equal(t, "req-0000001", attrValue(t, completed, "request_id").String())
	assert.Equal(t, int64(http.StatusOK), attrValue(t, completed, "status").Int64())
	assert.Equal(t, int64(5), attrValue(t, completed, "bytes").Int64())
	assert.True(t, attrValue(t, completed, "duration").Duration() > 0)
}

func TestMiddleware_requestID(t *testing.T) {
	s, cap := newCapturedServer(t)

	handler := s.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	for _, wantID := range []string{"req-0000001", "req-0000002"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		assert.Equal(t, wantID, rec.Header().Get("X-Request-ID"))
		assert.Equal(t, wantID, attrValue(t, recordByMessage(t, cap, "request completed"), "request_id").String())
	}
}

func TestMiddleware_writeError(t *testing.T) {
	s, cap := newCapturedServer(t)

	handler := s.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))

	handler.ServeHTTP(newFailWriter(), httptest.NewRequest(http.MethodGet, "/", nil))

	completed := recordByMessage(t, cap, "request completed")
	writeErr := attrValue(t, completed, "write_error").Any()

	err, ok := writeErr.(error)
	require.True(t, ok)
	assert.Same(t, errSimulatedWrite, err)
}

func TestMiddleware_serveContentRequests(t *testing.T) {
	s, cap := newCapturedServer(t)

	handler := s.loggingMiddleware(http.HandlerFunc(s.homeHandler))

	t.Run("full GET", func(t *testing.T) {
		cap.reset()

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		completed := recordByMessage(t, cap, "request completed")
		assert.Equal(t, int64(http.StatusOK), attrValue(t, completed, "status").Int64())
		assert.Equal(t, int64(1024), attrValue(t, completed, "bytes").Int64())
		assert.Equal(t, "", attrValue(t, completed, "served_range").String())
	})

	t.Run("range GET", func(t *testing.T) {
		cap.reset()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Range", "bytes=100-199")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		completed := recordByMessage(t, cap, "request completed")
		assert.Equal(t, int64(http.StatusPartialContent), attrValue(t, completed, "status").Int64())
		assert.Equal(t, int64(100), attrValue(t, completed, "bytes").Int64())
		assert.Equal(t, "bytes 100-199/1024", attrValue(t, completed, "served_range").String())
	})

	t.Run("unsatisfiable range", func(t *testing.T) {
		cap.reset()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Range", "bytes=99999-")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		completed := recordByMessage(t, cap, "request completed")
		assert.Equal(t, int64(http.StatusRequestedRangeNotSatisfiable), attrValue(t, completed, "status").Int64())
		assert.Equal(t, int64(len(rec.Body.Bytes())), attrValue(t, completed, "bytes").Int64())
	})

	t.Run("HEAD", func(t *testing.T) {
		cap.reset()

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/", nil))

		completed := recordByMessage(t, cap, "request completed")
		assert.Equal(t, int64(http.StatusOK), attrValue(t, completed, "status").Int64())
		assert.Equal(t, int64(0), attrValue(t, completed, "bytes").Int64())
	})

	t.Run("method not allowed", func(t *testing.T) {
		cap.reset()

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

		completed := recordByMessage(t, cap, "request completed")
		assert.Equal(t, int64(http.StatusMethodNotAllowed), attrValue(t, completed, "status").Int64())
	})
}
