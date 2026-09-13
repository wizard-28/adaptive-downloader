package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func testServer() *Server {
	return &Server{
		config: Config{
			Port:                    8080,
			FileSize:                1024,
			ConcurrencyThreshold:    4,
			BelowThresholdBandwidth: 30 * 1024 * 1024,
			AboveThresholdBandwidth: 10 * 1024 * 1024,
		},
		limiter: *rate.NewLimiter(
			rate.Limit(30*1024*1024),
			limiterBucketSize,
		),
	}
}

func TestServer_RequestConcurrency(t *testing.T) {
	s := testServer()

	assert.Equal(t, 0, s.activeGetRequests)
	assert.Equal(
		t,
		rate.Limit(s.config.BelowThresholdBandwidth),
		s.limiter.Limit(),
	)

	s.requestStarted()
	assert.Equal(t, 1, s.activeGetRequests)
	assert.Equal(
		t,
		rate.Limit(s.config.BelowThresholdBandwidth),
		s.limiter.Limit(),
	)

	s.requestStarted()
	s.requestStarted()
	s.requestStarted()

	assert.Equal(t, 4, s.activeGetRequests)
	assert.Equal(
		t,
		rate.Limit(s.config.BelowThresholdBandwidth),
		s.limiter.Limit(),
	)

	s.requestStarted()

	assert.Equal(t, 5, s.activeGetRequests)
	assert.Equal(
		t,
		rate.Limit(s.config.AboveThresholdBandwidth),
		s.limiter.Limit(),
	)

	s.requestFinished()

	assert.Equal(t, 4, s.activeGetRequests)
	assert.Equal(
		t,
		rate.Limit(s.config.BelowThresholdBandwidth),
		s.limiter.Limit(),
	)
}

func TestServer_HomeHandler(t *testing.T) {
	s := testServer()

	t.Run("GET returns configured file", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		s.homeHandler(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "application/octet-stream", res.Header.Get("Content-Type"))
		assert.Equal(t, "1024", res.Header.Get("Content-Length"))
		assert.Equal(t, 0, s.activeGetRequests)
	})

	t.Run("HEAD returns headers without body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/", nil)
		rec := httptest.NewRecorder()

		s.homeHandler(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "application/octet-stream", res.Header.Get("Content-Type"))
		assert.Equal(t, "1024", res.Header.Get("Content-Length"))
		assert.Empty(t, rec.Body.Bytes())
		assert.Equal(t, 0, s.activeGetRequests)
	})

	t.Run("GET range returns partial content", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Range", "bytes=100-199")

		rec := httptest.NewRecorder()

		s.homeHandler(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		assert.Equal(t, http.StatusPartialContent, res.StatusCode)
		assert.Equal(t, "bytes 100-199/1024", res.Header.Get("Content-Range"))
		assert.Equal(t, "100", res.Header.Get("Content-Length"))

		body := rec.Body.Bytes()

		require.Len(t, body, 100)
		assert.Equal(t, bytes.Repeat([]byte{'A'}, 100), body)
	})
}

func TestServer_HomeHandler_MethodNotAllowed(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()

	s.homeHandler(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)
	assert.Equal(t, "GET, HEAD", res.Header.Get("Allow"))
}
