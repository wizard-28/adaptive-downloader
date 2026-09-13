package server

import (
	"fmt"
	"net/http"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	// first status written, 0 = never written
	statusCode int
	// total body bytes forwarded
	bytes int64

	// the requestID associated with this request
	requestID string

	// first write error encountered
	writeError error
}

func (rw *responseRecorder) WriteHeader(code int) {
	if rw.statusCode == 0 {
		rw.statusCode = code
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseRecorder) Write(p []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(p)
	rw.bytes += int64(n)

	if err != nil && rw.writeError == nil {
		rw.writeError = err
	}

	return n, err
}

// Convenience method for other stuff to get direct access to the unimplemetned
// methods.
func (rw *responseRecorder) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &responseRecorder{ResponseWriter: w}
		rw.requestID = fmt.Sprintf("req-%07d", s.requestSeq.Add(1))

		rw.Header().Set("X-Request-ID", rw.requestID)

		s.logger.Debug("request started",
			"request_id", rw.requestID,
			"method", r.Method,
			"range", r.Header.Get("Range"),
			"remote_addr", r.RemoteAddr,
			"proto", r.Proto,
		)

		next.ServeHTTP(rw, r)

		status := rw.statusCode
		if status == 0 {
			status = http.StatusOK
		}

		s.logger.Info("request completed",
			"request_id", rw.requestID,
			"status", status,
			"bytes", rw.bytes,
			"duration", time.Since(start),
			"range", r.Header.Get("Range"),
			"served_range", rw.Header().Get("Content-Range"),
			"remote_addr", r.RemoteAddr,
			"write_error", rw.writeError,
		)
	})
}

var _ http.ResponseWriter = (*responseRecorder)(nil)
