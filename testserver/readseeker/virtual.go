package readseeker

import (
	"errors"
	"io"
)

type Virtual struct {
	Size   int64 // Total size
	Offset int64 // Current reading cursor position
}

func (d *Virtual) Read(p []byte) (n int, err error) {
	if d.Offset >= d.Size {
		return 0, io.EOF
	}

	remaining := d.Size - d.Offset

	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	for i := range p {
		p[i] = 'A'
	}

	d.Offset += int64(len(p))
	if d.Offset >= d.Size {
		err = io.EOF
	}

	return len(p), err
}

// Seek Useful for HTTP Range requests
func (d *Virtual) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64

	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = d.Offset + offset
	case io.SeekEnd:
		newOffset = d.Size + offset
	default:
		return 0, errors.New("invalid whence value")
	}

	if newOffset < 0 || newOffset > d.Size {
		return 0, errors.New("seek position out of bounds")
	}

	d.Offset = newOffset
	return newOffset, nil
}

var _ io.ReadSeeker = (*Virtual)(nil)
