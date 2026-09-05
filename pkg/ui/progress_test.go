package ui_test

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"adaptive-downloader/pkg/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vbauerster/mpb/v8"
)

type dummyWriter struct {
	mu          sync.Mutex
	bytesStored []byte
}

func (d *dummyWriter) WriteChunk(offset int64, data []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if int(offset)+len(data) > len(d.bytesStored) {
		newSlice := make([]byte, int(offset)+len(data))
		copy(newSlice, d.bytesStored)
		d.bytesStored = newSlice
	}
	copy(d.bytesStored[offset:], data)
	return len(data), nil
}

func TestProgressUI_Lifecycle(t *testing.T) {
	buf := &bytes.Buffer{}
	totalBytes := int64(1000)

	progressUI := ui.NewProgressUI(totalBytes, mpb.WithOutput(buf), mpb.WithAutoRefresh())
	require.NotNil(t, progressUI)

	// Dynamically acquire/allocate worker bar
	workerBar := progressUI.GetOrAddWorkerBar(1, 500)
	require.NotNil(t, workerBar)

	// Proxy writer setup
	storage := &dummyWriter{}
	pw := progressUI.ProxyWriter(storage, workerBar)
	require.NotNil(t, pw)

	n, err := pw.WriteChunk(0, make([]byte, 250))
	require.NoError(t, err)
	assert.Equal(t, 250, n)

	// Test overall progress update
	progressUI.UpdateOverall(250)

	// Test scale-down cleanup
	progressUI.RemoveWorkerBar(1)

	progressUI.Wait()
	assert.True(t, buf.Len() > 0, "expected progress output to be written to buffer")
}

func TestProgressUI_ConcurrentProxyWrites(t *testing.T) {
	totalBytes := int64(10_000)
	progressUI := ui.NewProgressUI(totalBytes, mpb.WithOutput(io.Discard))

	const numWorkers = 10
	const writesPerWorker = 20
	const chunkSize = 50

	storage := &dummyWriter{}
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer progressUI.RemoveWorkerBar(workerID)

			wBar := progressUI.GetOrAddWorkerBar(workerID, int64(writesPerWorker*chunkSize))
			pw := progressUI.ProxyWriter(storage, wBar)

			for j := 0; j < writesPerWorker; j++ {
				offset := int64(workerID*writesPerWorker*chunkSize + j*chunkSize)
				data := make([]byte, chunkSize)
				_, _ = pw.WriteChunk(offset, data)
			}
			wBar.SetTotal(int64(writesPerWorker*chunkSize), true)
		}(i)
	}

	wg.Wait()
	progressUI.Wait()

	assert.Equal(t, int(totalBytes), len(storage.bytesStored))
}

func TestProgressUI_DynamicScaling(t *testing.T) {
	totalBytes := int64(2000)
	progressUI := ui.NewProgressUI(totalBytes, mpb.WithOutput(io.Discard))

	// Initial worker slot 0
	bar0 := progressUI.GetOrAddWorkerBar(0, 500)
	require.NotNil(t, bar0)

	// Scale up: Add worker slot 1
	bar1 := progressUI.GetOrAddWorkerBar(1, 500)
	require.NotNil(t, bar1)

	// Verify worker slot 0 and slot 1 have distinct bars
	assert.True(t, bar0 != bar1, "different worker slots should have distinct bar instances")

	// Reuse worker slot 0: should retain exact same bar instance across chunks
	bar0Reused := progressUI.GetOrAddWorkerBar(0, 1000)
	assert.True(t, bar0 == bar0Reused, "worker slot should reuse the same bar instance across chunks")

	// Scale down: Remove worker slot 1 and slot 0
	progressUI.RemoveWorkerBar(1)
	progressUI.RemoveWorkerBar(0)

	progressUI.Wait()
}
