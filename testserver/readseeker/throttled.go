package readseeker

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

type Throttled struct {
	rs      io.ReadSeeker
	limiter *rate.Limiter
	ctx     context.Context
}

func NewThrottledReadSeeker(ctx context.Context, rs io.ReadSeeker, limiter *rate.Limiter) *Throttled {
	return &Throttled{
		rs:      rs,
		limiter: limiter,
		ctx:     ctx,
	}
}

// Read implements [io.ReadSeeker].
func (t *Throttled) Read(p []byte) (int, error) {
	n, err := t.rs.Read(p)

	if n <= 0 || t.limiter == nil {
		return n, err
	}

	if waitErr := t.limiter.WaitN(t.ctx, n); waitErr != nil {
		return 0, waitErr
	}

	return n, err
}

// Seek implements [io.ReadSeeker].
func (t *Throttled) Seek(offset int64, whence int) (int64, error) {
	return t.rs.Seek(offset, whence)
}

var _ io.ReadSeeker = (*Throttled)(nil)
