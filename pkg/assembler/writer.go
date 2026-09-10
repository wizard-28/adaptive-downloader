package assembler

import (
	"fmt"
	"os"
)

type Writer struct {
	file *os.File
	path string
}

func NewWriter(path string, totalSize int64) (*Writer, error) {
	return newWriter(path, totalSize, false)
}

func NewResumableWriter(
	path string,
	totalSize int64,
) (*Writer, error) {
	return newWriter(path, totalSize, true)
}

func newWriter(
	path string,
	totalSize int64,
	resume bool,
) (*Writer, error) {
	flags := os.O_CREATE | os.O_RDWR

	if !resume {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(
		path,
		flags,
		0o644,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open output file %q: %w",
			path,
			err,
		)
	}

	if totalSize > 0 {
		if err := file.Truncate(totalSize); err != nil {
			_ = file.Close()

			return nil, fmt.Errorf(
				"pre-allocate file %q (%d bytes): %w",
				path,
				totalSize,
				err,
			)
		}
	}

	return &Writer{
		file: file,
		path: path,
	}, nil
}

func (w *Writer) WriteChunk(
	offset int64,
	data []byte,
) (int, error) {
	bytesWritten, err := w.file.WriteAt(
		data,
		offset,
	)
	if err != nil {
		return bytesWritten, fmt.Errorf(
			"write chunk (offset: %d, len: %d): %w",
			offset,
			len(data),
			err,
		)
	}

	return bytesWritten, nil
}

func (w *Writer) Close() error {
	if w.file == nil {
		return nil
	}

	if err := w.file.Close(); err != nil {
		return fmt.Errorf(
			"close file handle %q: %w",
			w.path,
			err,
		)
	}

	return nil
}
