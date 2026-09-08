package fetcher_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"adaptive-downloader/pkg/fetcher"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbe_HeadSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "50000")
			w.WriteHeader(http.StatusOK)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := fetcher.New(2)

	res, err := client.Probe(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, int64(50000), res.ContentLength)

	resPkg, err := fetcher.Probe(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, int64(50000), resPkg.ContentLength)
}

func TestProbe_HeadFails_FallbackGet206(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "HEAD forbidden", http.StatusMethodNotAllowed)
			return
		}

		if r.Method == http.MethodGet {
			assert.Equal(t, "bytes=0-0", r.Header.Get("Range"))

			w.Header().Set("Content-Range", "bytes 0-0/123456")
			w.Header().Set("Content-Length", "1")
			w.WriteHeader(http.StatusPartialContent)

			_, _ = w.Write([]byte("X"))
			return
		}

		http.Error(w, "unexpected", http.StatusBadRequest)
	}))
	defer server.Close()

	client := fetcher.New(2)

	res, err := client.Probe(context.Background(), server.URL)

	require.NoError(t, err)
	assert.Equal(t, int64(123456), res.ContentLength)
}

func TestProbe_NoRangeSupport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "8000")
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodGet {
			w.Header().Set("Content-Length", "8000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 8000))
			return
		}

		http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := fetcher.New(1)

	_, err := client.Probe(context.Background(), server.URL)

	require.Error(t, err)
	assert.ErrorIs(t, err, fetcher.ErrRangeUnsupported)
}

func TestProbe_Errors(t *testing.T) {
	t.Run("server 500 error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(
				w,
				"internal server error",
				http.StatusInternalServerError,
			)
		}))
		defer server.Close()

		client := fetcher.New(1)

		_, err := client.Probe(
			context.Background(),
			server.URL,
		)

		require.Error(t, err)
		assert.ErrorIs(t, err, fetcher.ErrProbeFailed)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("malformed content-range header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				http.Error(
					w,
					"no head",
					http.StatusMethodNotAllowed,
				)
				return
			}

			w.Header().Set(
				"Content-Range",
				"bytes invalid",
			)
			w.WriteHeader(http.StatusPartialContent)
		}))
		defer server.Close()

		client := fetcher.New(1)

		_, err := client.Probe(
			context.Background(),
			server.URL,
		)

		require.Error(t, err)
		assert.ErrorIs(t, err, fetcher.ErrProbeFailed)
		assert.Contains(t, err.Error(), "parse Content-Range")
	})
}
