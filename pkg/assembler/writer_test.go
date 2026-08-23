package assembler_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"adaptive-downloader/pkg/assembler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriter_ConcurrentOutOfOrderWrites(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "download_test.bin")

	expectedContent := []byte("Go-Native LinUCB Adaptive File Downloader")
	totalSize := int64(len(expectedContent))

	w, err := assembler.NewWriter(outPath, totalSize)
	require.NoError(t, err, "NewWriter should create file without errors")

	info, err := os.Stat(outPath)
	require.NoError(t, err, "os.Stat failed on created test file")
	assert.Equal(t, totalSize, info.Size(), "pre-allocated file size mismatch")

	type Chunk struct {
		offset int64
		data   []byte
	}

	chunks := []Chunk{
		{offset: 17, data: []byte("Adaptive File Downloader")},
		{offset: 0, data: []byte("Go-Native ")},
		{offset: 10, data: []byte("LinUCB ")},
	}

	var wg sync.WaitGroup
	for _, chunk := range chunks {
		wg.Add(1)
		go func(c Chunk) {
			defer wg.Done()
			n, err := w.WriteChunk(c.offset, c.data)
			assert.NoError(t, err, "WriteChunk failed at offset %d", c.offset)
			assert.Equal(t, len(c.data), n, "bytes written mismatch at offset %d", c.offset)
		}(chunk)
	}
	wg.Wait()

	require.NoError(t, w.Close(), "Close should finish without error")

	actualContent, err := os.ReadFile(outPath)
	require.NoError(t, err, "failed reading assembled file from disk")
	assert.Equal(t, string(expectedContent), string(actualContent), "file payload does not match expected output")
}

func TestWriter_ErrorHandling(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "non_existent_folder/test.bin")

	_, err := assembler.NewWriter(outPath, 1024)
	assert.Error(t, err, "expected error when initializing writer in non-existent directory")
}
