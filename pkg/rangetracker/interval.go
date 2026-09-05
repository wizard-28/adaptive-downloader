package rangetracker

import (
	"fmt"
	"log/slog"
)

type Interval struct {
	Start int64
	End   int64
}

func (i Interval) Len() int64 {
	if i.End < i.Start {
		return 0
	}

	return i.End - i.Start + 1
}

func (i Interval) String() string {
	return fmt.Sprintf("bytes=%d-%d", i.Start, i.End)
}

func (i Interval) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("start", i.Start),
		slog.Int64("end", i.End),
		slog.Int64("len", i.Len()),
	)
}
