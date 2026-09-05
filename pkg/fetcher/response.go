package fetcher

import (
	"fmt"
	"io"
	"time"
)

func streamResponse(
	start time.Time,
	ttfb time.Duration,
	offset int64,
	body io.Reader,
	w ChunkWriter,
) (Metrics, error) {
	buf := make([]byte, 32*1024)

	var bytesWritten int64

	for {
		n, readErr := body.Read(buf)

		if n > 0 {
			written, writeErr := w.WriteChunk(
				offset,
				buf[:n],
			)

			if writeErr != nil {
				return buildMetrics(
						start,
						ttfb,
						bytesWritten,
					), fmt.Errorf(
						"write chunk at offset %d: %w",
						offset,
						writeErr,
					)
			}

			bytesWritten += int64(written)
			offset += int64(written)
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}

			return buildMetrics(
					start,
					ttfb,
					bytesWritten,
				), fmt.Errorf(
					"read response body: %w",
					readErr,
				)
		}
	}

	return buildMetrics(start, ttfb, bytesWritten), nil
}

func buildMetrics(
	start time.Time,
	ttfb time.Duration,
	bytesWritten int64,
) Metrics {
	duration := time.Since(start)

	var throughput float64
	if duration > 0 {
		throughput = float64(bytesWritten) / duration.Seconds()
	}

	return Metrics{
		BytesWritten: bytesWritten,
		TTFB:         ttfb,
		Duration:     duration,
		Throughput:   throughput,
	}
}
