package ui

import (
	"fmt"
	"sync"

	"adaptive-downloader/pkg/fetcher"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

type ProgressUI struct {
	p          *mpb.Progress
	totalBar   *mpb.Bar
	workerBars map[int]*mpb.Bar
	mu         sync.Mutex
}

// NewProgressUI initializes ProgressUI with an overall master bar.
func NewProgressUI(
	totalBytes int64,
	opts ...mpb.ContainerOption,
) *ProgressUI {
	defaultOpts := []mpb.ContainerOption{
		mpb.WithWidth(50),
		mpb.WithAutoRefresh(),
	}

	defaultOpts = append(defaultOpts, opts...)

	progress := mpb.New(defaultOpts...)

	totalBar := progress.AddBar(
		totalBytes,
		mpb.BarPriority(0),
		mpb.PrependDecorators(
			decor.Name(
				"Overall Progress: ",
				decor.WC{W: 18},
			),
			decor.CountersKibiByte(
				"% .2f / % .2f",
				decor.WC{W: 22},
			),
			decor.Percentage(
				decor.WC{W: 6},
			),
		),
		mpb.AppendDecorators(
			decor.AverageSpeed(
				decor.SizeB1024(0),
				"% .2f",
				decor.WC{W: 12},
			),
			decor.Name(
				" ",
				decor.WC{W: 2},
			),
			decor.OnComplete(
				decor.AverageETA(
					decor.ET_STYLE_GO,
					decor.WC{W: 8},
				),
				"Done",
			),
		),
	)

	return &ProgressUI{
		p:          progress,
		totalBar:   totalBar,
		workerBars: make(map[int]*mpb.Bar),
	}
}

// Writer returns a writer that forwards writes to w while updating the
// corresponding worker and overall progress bars.
func (ui *ProgressUI) Writer(
	w fetcher.ChunkWriter,
	workerID int,
	chunkSize int64,
) fetcher.ChunkWriter {
	bar := ui.GetOrAddWorkerBar(
		workerID,
		chunkSize,
	)

	return &progressChunkWriter{
		w:         w,
		workerBar: bar,
		ui:        ui,
	}
}

// ResetWorker resets the current progress of a worker's progress bar.
func (ui *ProgressUI) ResetWorker(workerID int) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if bar, ok := ui.workerBars[workerID]; ok {
		bar.SetCurrent(0)
	}
}

// GetOrAddWorkerBar reuses an existing worker slot's bar or creates one.
func (ui *ProgressUI) GetOrAddWorkerBar(
	workerID int,
	chunkSize int64,
) *mpb.Bar {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if bar, ok := ui.workerBars[workerID]; ok {
		bar.SetTotal(chunkSize, false)
		bar.SetCurrent(0)

		return bar
	}

	bar := ui.p.AddBar(
		chunkSize,
		mpb.BarPriority(workerID+1),
		mpb.PrependDecorators(
			decor.Name(
				fmt.Sprintf("Worker #%d: ", workerID),
				decor.WC{W: 18},
			),
			decor.CountersKibiByte(
				"% .2f / % .2f",
				decor.WC{W: 22},
			),
			decor.Percentage(
				decor.WC{W: 6},
			),
		),
		mpb.AppendDecorators(
			decor.AverageSpeed(
				decor.SizeB1024(0),
				"% .2f",
				decor.WC{W: 12},
			),
		),
	)

	ui.workerBars[workerID] = bar

	return bar
}

// RemoveWorkerBar removes a worker's progress bar.
func (ui *ProgressUI) RemoveWorkerBar(workerID int) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if bar, ok := ui.workerBars[workerID]; ok {
		bar.Abort(true)
		delete(ui.workerBars, workerID)
	}
}

// UpdateOverall adds n bytes to the overall progress bar.
func (ui *ProgressUI) UpdateOverall(n int) {
	if n <= 0 || ui.totalBar == nil {
		return
	}

	ui.totalBar.IncrBy(n)
}

// Wait blocks until all progress rendering has finished.
func (ui *ProgressUI) Wait() {
	ui.mu.Lock()

	for id, bar := range ui.workerBars {
		if !bar.Completed() {
			bar.Abort(true)
		}

		delete(ui.workerBars, id)
	}

	ui.mu.Unlock()

	if ui.totalBar != nil && !ui.totalBar.Completed() {
		ui.totalBar.SetTotal(-1, true)
	}

	ui.p.Wait()
}

type progressChunkWriter struct {
	w         fetcher.ChunkWriter
	workerBar *mpb.Bar
	ui        *ProgressUI
}

func (pw *progressChunkWriter) WriteChunk(
	offset int64,
	data []byte,
) (int, error) {
	n, err := pw.w.WriteChunk(offset, data)

	if n <= 0 {
		return n, err
	}

	if pw.workerBar != nil {
		pw.workerBar.IncrBy(n)
	}

	if pw.ui != nil {
		pw.ui.UpdateOverall(n)
	}

	return n, err
}

// Compile time check for implementing ChunkWriter interface
var _ fetcher.ChunkWriter = (*progressChunkWriter)(nil)
