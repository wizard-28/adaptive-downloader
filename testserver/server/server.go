package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"adaptive-downloader/testserver/readseeker"

	"golang.org/x/time/rate"
)

// 256 KiB, large enough to handle typical [io.ReadSeeker] requests
const limiterBucketSize = 256 * 1024

type Server struct {
	server  http.Server
	config  Config
	limiter rate.Limiter

	activeMu          sync.Mutex
	activeGetRequests int
}

func New(config Config) Server {
	return Server{
		config:  config,
		limiter: *rate.NewLimiter(rate.Limit(config.BelowThresholdBandwidth), limiterBucketSize),
	}
}

func (s *Server) requestStarted() {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	s.activeGetRequests++
	slog.Debug("Increased active get requests count", "activeGetRequests", s.activeGetRequests)

	if s.activeGetRequests > s.config.ConcurrencyThreshold {
		s.limiter.SetLimit(rate.Limit(s.config.AboveThresholdBandwidth))
	} else {
		s.limiter.SetLimit(rate.Limit(s.config.BelowThresholdBandwidth))
	}
}

func (s *Server) requestFinished() {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	s.activeGetRequests--
	slog.Debug("Decreased active get requests count", "activeGetRequests", s.activeGetRequests)

	if s.activeGetRequests > s.config.ConcurrencyThreshold {
		s.limiter.SetLimit(rate.Limit(s.config.AboveThresholdBandwidth))
	} else {
		s.limiter.SetLimit(rate.Limit(s.config.BelowThresholdBandwidth))
	}
}

func (s *Server) homeHandler(w http.ResponseWriter, r *http.Request) {
	slog.Info("handled request", "method", r.Method, "path", r.URL.Path)

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Explicitly set it to octet-stream otherwise ServeContent might misjudge it
	w.Header().Set("Content-Type", "application/octet-stream")

	configuredSize := s.config.FileSize

	virtualReadSeeker := &readseeker.Virtual{
		Size: configuredSize,
	}

	throttledVirtualReadSeeker := readseeker.NewThrottledReadSeeker(
		r.Context(),
		virtualReadSeeker,
		&s.limiter,
	)

	slog.Info("Generating dynamic virtual file",
		"size_bytes", configuredSize,
		"range_header", r.Header.Get("Range"),
	)

	if r.Method == http.MethodGet {
		s.requestStarted()
		defer s.requestFinished()
	}

	http.ServeContent(w, r, "dummy_data.bin", time.Time{}, throttledVirtualReadSeeker)
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.homeHandler)

	s.server.Addr = fmt.Sprintf(":%d", s.config.Port)
	s.server.Handler = mux

	shutdownDone := make(chan struct{})

	go func() {
		defer close(shutdownDone)

		<-ctx.Done()

		slog.Info("Shutdown signal received, closing server...")

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			slog.Error(
				"failed to shut down server gracefully",
				"error", err,
			)
		}
	}()

	slog.Info("Server is starting", "port", s.config.Port)

	err := s.server.ListenAndServe()

	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}

	return fmt.Errorf("server failed: %w", err)
}
