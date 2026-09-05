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
func NewProgressUI(totalBytes int64, opts ...mpb.ContainerOption) *ProgressUI {
	defaultOpts := []mpb.ContainerOption{
		mpb.WithWidth(50),
		mpb.WithAutoRefresh(),
	}
	defaultOpts = append(defaultOpts, opts...)

	p := mpb.New(defaultOpts...)

	// Master Bar: Pinned strictly at priority 0
	totalBar := p.AddBar(totalBytes,
		mpb.BarPriority(0),
		mpb.PrependDecorators(
			decor.Name("Overall Progress: ", decor.WC{W: 18}),
			decor.CountersKibiByte("% .2f / % .2f", decor.WC{W: 22}),
			decor.Percentage(decor.WC{W: 6}),
		),
		mpb.AppendDecorators(
			decor.AverageSpeed(decor.SizeB1024(0), "% .2f", decor.WC{W: 12}),
			decor.Name(" ", decor.WC{W: 2}),
			decor.OnComplete(decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 8}), "Done"),
		),
	)

	return &ProgressUI{
		p:          p,
		totalBar:   totalBar,
		workerBars: make(map[int]*mpb.Bar),
	}
}

// GetOrAddWorkerBar reuses an existing worker slot's bar row or dynamically creates one if slot doesn't exist yet.
func (ui *ProgressUI) GetOrAddWorkerBar(workerSlotID int, chunkSize int64) *mpb.Bar {
	if ui == nil || ui.p == nil {
		return nil
	}

	ui.mu.Lock()
	defer ui.mu.Unlock()

	bar, ok := ui.workerBars[workerSlotID]
	if ok {
		// Reset current slot bar in-place for the new chunk without spawning a new line
		bar.SetTotal(chunkSize, false)
		bar.SetCurrent(0)
		return bar
	}

	// Dynamically create a bar locked to priority (workerSlotID + 1)
	bar = ui.p.AddBar(chunkSize,
		mpb.BarPriority(workerSlotID+1),
		mpb.PrependDecorators(
			decor.Name(fmt.Sprintf("Worker #%d: ", workerSlotID), decor.WC{W: 18}),
			decor.CountersKibiByte("% .2f / % .2f", decor.WC{W: 22}),
			decor.Percentage(decor.WC{W: 6}),
		),
		mpb.AppendDecorators(
			decor.AverageSpeed(decor.SizeB1024(0), "% .2f", decor.WC{W: 12}),
		),
	)

	ui.workerBars[workerSlotID] = bar
	return bar
}

// RemoveWorkerBar cleanly removes a worker slot's bar when dynamic concurrency scales down or exits.
func (ui *ProgressUI) RemoveWorkerBar(workerSlotID int) {
	if ui == nil {
		return
	}

	ui.mu.Lock()
	defer ui.mu.Unlock()

	if bar, ok := ui.workerBars[workerSlotID]; ok {
		bar.Abort(true) // Removes line from terminal viewport without layout shifting
		delete(ui.workerBars, workerSlotID)
	}
}

// UpdateOverall adds n bytes to the master total progress bar in a thread-safe manner.
func (ui *ProgressUI) UpdateOverall(n int) {
	if ui == nil || ui.totalBar == nil || n <= 0 {
		return
	}
	ui.totalBar.IncrBy(n)
}

// ProxyWriter returns a fetcher.ChunkWriter wrapper that updates both worker and overall bars.
func (ui *ProgressUI) ProxyWriter(w fetcher.ChunkWriter, workerBar *mpb.Bar) fetcher.ChunkWriter {
	return &progressChunkWriter{
		w:         w,
		workerBar: workerBar,
		ui:        ui,
	}
}

// Wait blocks until all progress rendering finishes cleanly.
func (ui *ProgressUI) Wait() {
	if ui == nil || ui.p == nil {
		return
	}

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

func (pw *progressChunkWriter) WriteChunk(offset int64, data []byte) (int, error) {
	n, err := pw.w.WriteChunk(offset, data)
	if n > 0 {
		if pw.workerBar != nil {
			pw.workerBar.IncrBy(n)
		}
		if pw.ui != nil {
			pw.ui.UpdateOverall(n)
		}
	}
	return n, err
}
