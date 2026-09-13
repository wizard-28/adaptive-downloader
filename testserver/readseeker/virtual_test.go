package readseeker_test

import (
	"io"
	"testing"

	"adaptive-downloader/testserver/readseeker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVirtual_Read(t *testing.T) {
	t.Run("reads requested bytes", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 10}

		buf := make([]byte, 4)

		n, err := rs.Read(buf)

		require.NoError(t, err)
		assert.Equal(t, 4, n)
		assert.Equal(t, []byte("AAAA"), buf)
		assert.Equal(t, int64(4), rs.Offset)
	})

	t.Run("does not read beyond size", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 5}

		buf := make([]byte, 10)

		n, err := rs.Read(buf)

		assert.Equal(t, io.EOF, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, []byte("AAAAA"), buf[:n])
		assert.Equal(t, int64(5), rs.Offset)
	})

	t.Run("returns EOF after reaching end", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 5}

		buf := make([]byte, 5)

		n, err := rs.Read(buf)

		assert.Equal(t, io.EOF, err)
		assert.Equal(t, 5, n)

		n, err = rs.Read(buf)

		assert.Equal(t, io.EOF, err)
		assert.Equal(t, 0, n)
	})
}

func TestVirtual_Seek(t *testing.T) {
	t.Run("seek from start", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 100, Offset: 50}

		offset, err := rs.Seek(20, io.SeekStart)

		require.NoError(t, err)
		assert.Equal(t, int64(20), offset)
		assert.Equal(t, int64(20), rs.Offset)
	})

	t.Run("seek from current", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 100, Offset: 50}

		offset, err := rs.Seek(20, io.SeekCurrent)

		require.NoError(t, err)
		assert.Equal(t, int64(70), offset)
		assert.Equal(t, int64(70), rs.Offset)
	})

	t.Run("seek from end", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 100, Offset: 50}

		offset, err := rs.Seek(-20, io.SeekEnd)

		require.NoError(t, err)
		assert.Equal(t, int64(80), offset)
		assert.Equal(t, int64(80), rs.Offset)
	})

	t.Run("rejects invalid whence", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 100}

		offset, err := rs.Seek(10, 123)

		assert.Error(t, err)
		assert.Equal(t, int64(0), offset)
		assert.Equal(t, int64(0), rs.Offset)
	})

	t.Run("rejects negative offset", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 100}

		offset, err := rs.Seek(-1, io.SeekStart)

		assert.Error(t, err)
		assert.Equal(t, int64(0), offset)
		assert.Equal(t, int64(0), rs.Offset)
	})

	t.Run("rejects offset beyond size", func(t *testing.T) {
		rs := &readseeker.Virtual{Size: 100}

		offset, err := rs.Seek(101, io.SeekStart)

		assert.Error(t, err)
		assert.Equal(t, int64(0), offset)
		assert.Equal(t, int64(0), rs.Offset)
	})
}
