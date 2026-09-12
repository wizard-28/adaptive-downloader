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

const (
	testConcurrency = "2"
	testChunkSize   = "16384"
)

// defaultDownloadArgs returns a complete set of download arguments so that
// each test is independent of flag values left behind by previous tests.
func defaultDownloadArgs(
	serverURL string,
	outputPath string,
) []string {
	return []string{
		"download",
		serverURL,
		"-o", outputPath,
		"-p", "fixed",
		"-n", testConcurrency,
		"-s", testChunkSize,
		"-h", "",
		"--no-progress",
	}
}

func TestDownloadCmd_Integration(t *testing.T) {
	payload := makeTestPayload(100 * 1024)

	hash := sha256.Sum256(payload)
	validHash := hex.EncodeToString(hash[:])

	server := newRangeServer(t, payload)
	defer server.Close()

	t.Run("successful download", func(t *testing.T) {
		outPath := filepath.Join(
			t.TempDir(),
			"downloaded.bin",
		)

		args := defaultDownloadArgs(
			server.URL,
			outPath,
		)

		args = append(
			args,
			"-n", "3",
			"-s", "16384",
			"-h", validHash,
		)

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs(args)

		err := rootCmd.Execute()
		require.NoError(t, err)

		downloadedData, err := os.ReadFile(outPath)
		require.NoError(t, err)

		assert.Equal(t, payload, downloadedData)
	})

	t.Run("checksum mismatch returns error", func(t *testing.T) {
		outPath := filepath.Join(
			t.TempDir(),
			"mismatch.bin",
		)

		args := defaultDownloadArgs(
			server.URL,
			outPath,
		)

		args = append(
			args,
			"-h", strings.Repeat("0", 64),
		)

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs(args)

		err := rootCmd.Execute()

		require.Error(t, err)
		assert.Contains(
			t,
			err.Error(),
			"checksum verification failed",
		)
	})
}

func TestDownloadCmd_NoRangeSupport(t *testing.T) {
	payload := make([]byte, 1000)

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Server does not support ranges.
			w.Header().Set(
				"Content-Length",
				strconv.Itoa(len(payload)),
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		}),
	)
	defer server.Close()

	outPath := filepath.Join(
		t.TempDir(),
		"no-range.bin",
	)

	rootCmd := cmd.GetRootCmd()
	rootCmd.SetArgs(
		defaultDownloadArgs(
			server.URL,
			outPath,
		),
	)

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		"server does not support HTTP byte-range requests",
	)
}

func TestDownloadCmd_InvalidFlags(t *testing.T) {
	serverURL := "https://example.com/file"

	tests := []struct {
		name        string
		override    []string
		errContains string
	}{
		{
			name: "zero concurrency",
			override: []string{
				"-n", "0",
			},
			errContains: "concurrency must be greater than 0",
		},
		{
			name: "negative concurrency",
			override: []string{
				"-n", "-1",
			},
			errContains: "concurrency must be greater than 0",
		},
		{
			name: "zero chunk size",
			override: []string{
				"-s", "0",
			},
			errContains: "chunk size must be greater than 0",
		},
		{
			name: "negative chunk size",
			override: []string{
				"-s", "-1",
			},
			errContains: "chunk size must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := defaultDownloadArgs(
				serverURL,
				filepath.Join(t.TempDir(), "unused.bin"),
			)

			args = append(args, tt.override...)

			rootCmd := cmd.GetRootCmd()
			rootCmd.SetArgs(args)

			err := rootCmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestDownloadCmd_PolicySelection(t *testing.T) {
	payload := makeTestPayload(32 * 1024)

	server := newRangeServer(t, payload)
	defer server.Close()

	t.Run("fixed policy succeeds", func(t *testing.T) {
		outPath := filepath.Join(
			t.TempDir(),
			"fixed.bin",
		)

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs(
			defaultDownloadArgs(
				server.URL,
				outPath,
			),
		)

		err := rootCmd.Execute()

		require.NoError(t, err)

		actual, err := os.ReadFile(outPath)
		require.NoError(t, err)

		assert.Equal(t, payload, actual)
	})

	t.Run("bandit policy is not implemented", func(t *testing.T) {
		args := defaultDownloadArgs(
			server.URL,
			filepath.Join(t.TempDir(), "bandit.bin"),
		)

		args = append(
			args,
			"--policy", "bandit",
		)

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs(args)

		err := rootCmd.Execute()

		require.Error(t, err)
		assert.Contains(
			t,
			err.Error(),
			"bandit policy is not implemented yet",
		)
	})

	t.Run("rl policy is not implemented", func(t *testing.T) {
		args := defaultDownloadArgs(
			server.URL,
			filepath.Join(t.TempDir(), "rl.bin"),
		)

		args = append(
			args,
			"--policy", "rl",
		)

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs(args)

		err := rootCmd.Execute()

		require.Error(t, err)
		assert.Contains(
			t,
			err.Error(),
			"rl policy is not implemented yet",
		)
	})

	t.Run("unknown policy", func(t *testing.T) {
		args := defaultDownloadArgs(
			server.URL,
			filepath.Join(t.TempDir(), "unknown.bin"),
		)

		args = append(
			args,
			"--policy", "unknown",
		)

		rootCmd := cmd.GetRootCmd()
		rootCmd.SetArgs(args)

		err := rootCmd.Execute()

		require.Error(t, err)
		assert.Contains(
			t,
			err.Error(),
			`unknown policy "unknown"`,
		)
	})
}

func makeTestPayload(size int) []byte {
	payload := make([]byte, size)

	for i := range payload {
		payload[i] = byte((i * 31) % 251)
	}

	return payload
}

func newRangeServer(
	t *testing.T,
	payload []byte,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set(
					"Content-Length",
					strconv.Itoa(len(payload)),
				)
				w.WriteHeader(http.StatusOK)
				return
			}

			if r.Method != http.MethodGet {
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
				return
			}

			rangeHeader := r.Header.Get("Range")

			if rangeHeader == "" {
				w.Header().Set(
					"Content-Length",
					strconv.Itoa(len(payload)),
				)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(payload)
				return
			}

			start, end, ok := parseRange(
				rangeHeader,
				int64(len(payload)),
			)
			if !ok {
				http.Error(
					w,
					"invalid range",
					http.StatusRequestedRangeNotSatisfiable,
				)
				return
			}

			body := payload[start : end+1]

			w.Header().Set(
				"Content-Range",
				fmt.Sprintf(
					"bytes %d-%d/%d",
					start,
					end,
					len(payload),
				),
			)
			w.Header().Set(
				"Content-Length",
				strconv.Itoa(len(body)),
			)

			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body)
		}),
	)
}

func parseRange(
	header string,
	total int64,
) (int, int, bool) {
	const prefix = "bytes="

	if !strings.HasPrefix(header, prefix) {
		return 0, 0, false
	}

	parts := strings.Split(
		strings.TrimPrefix(header, prefix),
		"-",
	)

	if len(parts) != 2 {
		return 0, 0, false
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}

	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}

	if start < 0 || start > end || end >= total {
		return 0, 0, false
	}

	return int(start), int(end), true
}
