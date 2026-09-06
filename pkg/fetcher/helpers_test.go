package fetcher_test

import (
	"context"
	"sync"
)

type writeRecord struct {
	offset int64
	data   []byte
}

type mockWriter struct {
	mu          sync.Mutex
	writes      []writeRecord
	buf         []byte
	errToReturn error
}

func newMockWriter(size int64) *mockWriter {
	return &mockWriter{
		buf: make([]byte, size),
	}
}

func (m *mockWriter) WriteChunk(offset int64, data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.errToReturn != nil {
		return 0, m.errToReturn
	}

	record := writeRecord{
		offset: offset,
		data:   append([]byte(nil), data...),
	}
	m.writes = append(m.writes, record)

	if offset+int64(len(data)) <= int64(len(m.buf)) {
		copy(m.buf[offset:], data)
	}

	return len(data), nil
}

func (m *mockWriter) TotalBytesWritten() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	var total int64
	for _, w := range m.writes {
		total += int64(len(w.data))
	}
	return total
}

type cancelOnWriteWriter struct {
	*mockWriter
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelOnWriteWriter) WriteChunk(offset int64, data []byte) (int, error) {
	n, err := c.mockWriter.WriteChunk(offset, data)
	if err == nil {
		c.once.Do(c.cancel)
	}
	return n, err
}
