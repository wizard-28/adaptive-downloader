package cmd_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"adaptive-downloader/cmd"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadCmd_Integration(t *testing.T) {
	// 100KB test payload
	payload := make([]byte, 100*1024)
	for i := range payload {
		payload[i] = byte((i * 31) % 256)
	}
	hashSum := sha256.Sum256(payload)
	validHash := hex.EncodeToString(hashSum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodGet {
			rangeHeader := r.Header.Get("Range")
			if rangeHeader == "" {
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(payload)
				return
			}

			// Parse Range: bytes=start-end
			if !strings.HasPrefix(rangeHeader, "bytes=") {
				http.Error(w, "invalid range prefix", http.StatusBadRequest)
				return
			}
			parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
			start, _ := strconv.ParseInt(parts[0], 10, 64)
			end, _ := strconv.ParseInt(parts[1], 10, 64)

			if start > end || end >= int64(len(payload)) {
				http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
				return
			}

			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start : end+1])
			return
		}

		http.Error(w, "not allowed", http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	t.Run("Successful multi-worker download with valid SHA256", func(t *testing.T) {
		tempDir := t.TempDir()
		outPath := filepath.Join(tempDir, "downloaded.bin")

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs([]string{
			"download",
			server.URL,
			"-o", outPath,
			"-n", "3",
			"-s", "16384", // 16KB chunks
			"-h", validHash,
		})

		err := rootCmd.Execute()
		require.NoError(t, err)

		downloadedData, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Equal(t, payload, downloadedData)
	})

	t.Run("SHA256 mismatch returns error", func(t *testing.T) {
		tempDir := t.TempDir()
		outPath := filepath.Join(tempDir, "mismatch.bin")

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs([]string{
			"download",
			server.URL,
			"-o", outPath,
			"-n", "2",
			"-s", "32768",
			"-h", "0000000000000000000000000000000000000000000000000000000000000000",
		})

		err := rootCmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum verification failed")
	})
}

func TestDownloadCmd_NoRangeSupport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server does not support ranges
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 1000))
	}))
	defer server.Close()

	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "no_range.bin")

	rootCmd := cmd.GetRootCmd()
	rootCmd.SetArgs([]string{
		"download",
		server.URL,
		"-o", outPath,
	})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server does not support HTTP byte-range requests")
}
