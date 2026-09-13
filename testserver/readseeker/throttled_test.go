package readseeker_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"adaptive-downloader/testserver/readseeker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestThrottled_Read(t *testing.T) {
	t.Run("reads through underlying reader", func(t *testing.T) {
		rs := strings.NewReader("hello")

		limiter := rate.NewLimiter(rate.Inf, 5)

		throttled := readseeker.NewThrottledReadSeeker(
			context.Background(),
			rs,
			limiter,
		)

		buf := make([]byte, 5)

		n, err := throttled.Read(buf)

		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(buf))
	})

	t.Run("nil limiter does not throttle", func(t *testing.T) {
		rs := strings.NewReader("hello")

		throttled := readseeker.NewThrottledReadSeeker(
			context.Background(),
			rs,
			nil,
		)

		buf := make([]byte, 5)

		n, err := throttled.Read(buf)

		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(buf))
	})

	t.Run("propagates EOF", func(t *testing.T) {
		rs := strings.NewReader("hello")

		limiter := rate.NewLimiter(rate.Inf, 5)

		throttled := readseeker.NewThrottledReadSeeker(
			context.Background(),
			rs,
			limiter,
		)

		buf := make([]byte, 5)

		n, err := throttled.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 5, n)

		n, err = throttled.Read(buf)

		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("canceled context stops waiting", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rs := strings.NewReader("hello")

		limiter := rate.NewLimiter(rate.Limit(1), 1)

		throttled := readseeker.NewThrottledReadSeeker(
			ctx,
			rs,
			limiter,
		)

		cancel()

		buf := make([]byte, 5)

		n, err := throttled.Read(buf)

		assert.Error(t, err)
		assert.Equal(t, 0, n)
	})
}

func TestThrottled_Seek(t *testing.T) {
	rs := strings.NewReader("hello")

	throttled := readseeker.NewThrottledReadSeeker(
		context.Background(),
		rs,
		rate.NewLimiter(rate.Inf, 5),
	)

	offset, err := throttled.Seek(2, io.SeekStart)

	require.NoError(t, err)
	assert.Equal(t, int64(2), offset)

	buf := make([]byte, 3)

	n, err := throttled.Read(buf)

	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "llo", string(buf))
}
