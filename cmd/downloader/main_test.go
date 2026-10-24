package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func content(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

// rangeServer serves data with http.ServeContent, which honours Range.
func rangeServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "f.bin", time.Unix(0, 0), bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeReadsSizeFromContentRange(t *testing.T) {
	srv := rangeServer(t, content(12345))
	n, err := probe(context.Background(), srv.Client(), srv.URL)
	if err != nil || n != 12345 {
		t.Fatalf("probe = %d, %v; want 12345, nil", n, err)
	}
}

func TestProbeRejectsServerWithoutRanges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content(100)) // ignores Range, answers 200
	}))
	defer srv.Close()
	if _, err := probe(context.Background(), srv.Client(), srv.URL); err == nil ||
		!strings.Contains(err.Error(), "does not support range requests") {
		t.Fatalf("probe error = %v, want a no-range-support error", err)
	}
}

func TestRunProbesSizeAndVerifiesChecksum(t *testing.T) {
	data := content(300000)
	srv := rangeServer(t, data)
	sum := sha256.Sum256(data)
	out := filepath.Join(t.TempDir(), "o.bin")
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"-url", srv.URL, "-out", out, "-concurrency", "3",
		"-chunk", "65536", "-sha256", hex.EncodeToString(sum[:])}, &stdout)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, stdout.String())
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, data) {
		t.Fatal("output differs from source")
	}
	if !strings.Contains(stdout.String(), "sha256 OK") || !strings.Contains(stdout.String(), "size 300000 bytes") {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}

func TestRunFailsOnChecksumMismatch(t *testing.T) {
	srv := rangeServer(t, content(5000))
	out := filepath.Join(t.TempDir(), "o.bin")
	err := run(context.Background(), []string{"-url", srv.URL, "-out", out, "-sha256", strings.Repeat("0", 64)}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("run error = %v, want sha256 mismatch", err)
	}
}

func TestRunRejectsMissingURL(t *testing.T) {
	if err := run(context.Background(), []string{}, &bytes.Buffer{}); err == nil {
		t.Fatal("run without -url succeeded")
	}
}
