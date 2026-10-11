// Package assembler writes downloaded byte ranges into their exact positions
// in an output file. Ranges may arrive in any order; each is written
// independently with random-access I/O, so the whole file is never held in
// memory.
//
// Ranges use inclusive HTTP semantics [start, end], matching fetcher.Result.
package assembler

import (
	"errors"
	"fmt"
	"os"
)

var (
	// ErrInvalidSize indicates a negative expected file size.
	ErrInvalidSize = errors.New("assembler: invalid file size")
	// ErrInvalidRange indicates a range that is negative, reversed, or beyond
	// the expected file size.
	ErrInvalidRange = errors.New("assembler: invalid byte range")
	// ErrLengthMismatch indicates that the supplied data does not cover the
	// range exactly.
	ErrLengthMismatch = errors.New("assembler: data length does not match range")
	// ErrClosed indicates use of an Assembler after it was closed.
	ErrClosed = errors.New("assembler: closed")
)

// Assembler writes byte ranges into a single output file.
type Assembler struct {
	file   *os.File
	size   int64
	closed bool
}

// New creates or truncates the file at path and prepares it to hold size
// bytes. The file is extended to its final size immediately, so ranges may be
// written at any offset in any order.
func New(path string, size int64) (*Assembler, error) {
	if size < 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidSize, size)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("assembler: open %q: %w", path, err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("assembler: truncate %q to %d: %w", path, size, err)
	}

	return &Assembler{file: file, size: size}, nil
}

// Size returns the expected total size of the output file.
func (a *Assembler) Size() int64 { return a.size }

// WriteRange writes data covering the inclusive byte range [start, end] at the
// matching offset in the output file. Ranges may be written in any order and
// may overwrite earlier writes.
//
// It returns ErrInvalidRange for a range outside [0, Size), ErrLengthMismatch
// unless len(data) == end-start+1, and ErrClosed after Close.
func (a *Assembler) WriteRange(start, end int64, data []byte) error {
	if a.closed {
		return ErrClosed
	}
	if start < 0 || end < start || end >= a.size {
		return fmt.Errorf("%w: [%d,%d] for file size %d", ErrInvalidRange, start, end, a.size)
	}
	if want := end - start + 1; int64(len(data)) != want {
		return fmt.Errorf("%w: got %d bytes for [%d,%d] (want %d)", ErrLengthMismatch, len(data), start, end, want)
	}

	n, err := a.file.WriteAt(data, start)
	if err != nil {
		return fmt.Errorf("assembler: write [%d,%d]: %w", start, end, err)
	}
	if n != len(data) {
		return fmt.Errorf("assembler: short write [%d,%d]: wrote %d of %d bytes", start, end, n, len(data))
	}
	return nil
}

// Close flushes and closes the output file. It is safe to call more than once.
func (a *Assembler) Close() error {
	if a.closed {
		return nil
	}
	a.closed = true
	if err := a.file.Close(); err != nil {
		return fmt.Errorf("assembler: close: %w", err)
	}
	return nil
}
