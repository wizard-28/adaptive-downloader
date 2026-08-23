package assembler

import (
	"fmt"
	"log/slog"
	"os"
)

type Writer struct {
	file *os.File
	path string
}

func NewWriter(path string, totalSize int64) (*Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		slog.Error("failed to open output file", "path", path, "error", err)
		return nil, err
	}

	if totalSize > 0 {
		if err := file.Truncate(totalSize); err != nil {
			file.Close()
			slog.Error("failed to pre-allocate file size", "path", path, "size", totalSize)
			return nil, fmt.Errorf("failed to pre-allocate file size: %w", err)
		}
	}

	slog.Info("initialized file writer", "path", path, "size", totalSize)

	return &Writer{
		file: file,
		path: path,
	}, nil
}

func (w *Writer) WriteChunk(offset int64, data []byte) (int, error) {
	bytesWritten, err := w.file.WriteAt(data, offset)
	if err != nil {
		slog.Error("error writing chunk", "path", w.path, "offset", offset, "requested_bytes", len(data), "error", err)
		return bytesWritten, fmt.Errorf("error while writing chunk: %w", err)
	}

	slog.Debug("chunk written successfully", "path", w.path, "offset", offset, "bytes_written", bytesWritten)

	return bytesWritten, nil
}

func (w *Writer) Close() error {
	if w.file != nil {
		err := w.file.Close()
		if err != nil {
			slog.Error("failed to close file handle", "path", w.path, "error", err)
			return err
		}
		slog.Info("closed file handle", "path", w.path)
	}

	return nil
}
