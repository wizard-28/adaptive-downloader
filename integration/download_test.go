package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testServerPort = 18080
	testFileSize   = 10 * 1024 * 1024
)

// TestDownload verifies the complete downloader flow against the real test
// server process.
func TestDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	projectRoot := projectRoot(t)
	outputPath := filepath.Join(t.TempDir(), "download.bin")

	t.Logf("project root: %s", projectRoot)
	t.Logf("output path: %s", outputPath)

	testServerBinary := buildTestServer(t, projectRoot)

	serverCmd := startTestServer(t, testServerBinary)

	waitForServer(t, testServerPort)

	t.Log("test server is ready")

	runDownloader(t, projectRoot, outputPath)

	t.Log("downloader completed successfully")

	verifyDownloadedFile(t, outputPath, testFileSize)

	t.Log("downloaded file verified successfully")

	stopProcess(t, serverCmd)

	t.Log("integration test completed successfully")
}

func buildTestServer(t *testing.T, projectRoot string) string {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), "testserver")

	t.Logf("building test server: %s", binaryPath)

	cmd := exec.Command(
		"go",
		"build",
		"-o",
		binaryPath,
		"./testserver",
	)

	cmd.Dir = projectRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"failed to build test server: %v\noutput:\n%s",
			err,
			output,
		)
	}

	t.Logf("test server built successfully")

	return binaryPath
}

func startTestServer(t *testing.T, binaryPath string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(
		binaryPath,
		"-v",
		"serve",
		"--port",
		strconv.Itoa(testServerPort),
		"--size",
		"10MiB",
	)

	t.Logf("starting test server: %s", strings.Join(cmd.Args, " "))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to create test server stdout pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("failed to create test server stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	t.Logf("test server process started with PID %d", cmd.Process.Pid)

	// Keep draining the server's output so the child process cannot block
	// because its stdout or stderr pipe becomes full.
	go drainOutput(t, stdout, "server stdout")
	go drainOutput(t, stderr, "server stderr")

	t.Cleanup(func() {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return
		}

		t.Log("test cleanup: stopping test server")

		stopProcess(t, cmd)
	})

	return cmd
}

func runDownloader(t *testing.T, projectRoot, outputPath string) {
	t.Helper()

	cmd := exec.Command(
		"go",
		"run",
		".",
		"-v",
		"download",
		"http://127.0.0.1:"+strconv.Itoa(testServerPort),
		"--output",
		outputPath,
		"--concurrency",
		"4",
		"--chunk-size",
		strconv.Itoa(1*1024*1024),
		"--no-progress",
	)

	cmd.Dir = projectRoot

	t.Logf("starting downloader: %s", strings.Join(cmd.Args, " "))

	start := time.Now()

	output, err := cmd.CombinedOutput()

	t.Logf("downloader exited after %s", time.Since(start))

	if len(output) > 0 {
		t.Logf("downloader output:\n%s", output)
	}

	if err != nil {
		t.Fatalf("downloader failed: %v", err)
	}
}

func verifyDownloadedFile(t *testing.T, path string, expectedSize int) {
	t.Helper()

	t.Logf("verifying downloaded file: %s", path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat downloaded file: %v", err)
	}

	t.Logf("downloaded file size: %d bytes", info.Size())

	if info.Size() != int64(expectedSize) {
		t.Fatalf(
			"downloaded file has incorrect size: got %d, want %d",
			info.Size(),
			expectedSize,
		)
	}

	t.Log("calculating expected SHA-256")

	expectedHash := expectedVirtualFileSHA256(expectedSize)

	t.Log("calculating actual SHA-256")

	actualHash := fileSHA256(t, path)

	t.Logf("expected SHA-256: %s", expectedHash)
	t.Logf("actual SHA-256:   %s", actualHash)

	if actualHash != expectedHash {
		t.Fatalf(
			"downloaded file has incorrect SHA-256: got %s, want %s",
			actualHash,
			expectedHash,
		)
	}
}

func expectedVirtualFileSHA256(size int) string {
	hash := sha256.New()

	const chunkSize = 1024 * 1024
	chunk := make([]byte, chunkSize)

	for i := range chunk {
		chunk[i] = 'A'
	}

	remaining := size

	for remaining > 0 {
		n := min(remaining, len(chunk))

		if _, err := hash.Write(chunk[:n]); err != nil {
			panic(fmt.Sprintf("failed to hash expected file: %v", err))
		}

		remaining -= n
	}

	return hex.EncodeToString(hash.Sum(nil))
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open downloaded file: %v", err)
	}
	defer file.Close()

	hash := sha256.New()

	if _, err := io.Copy(hash, file); err != nil {
		t.Fatalf("failed to hash downloaded file: %v", err)
	}

	return hex.EncodeToString(hash.Sum(nil))
}

func waitForServer(t *testing.T, port int) {
	t.Helper()

	address := "127.0.0.1:" + strconv.Itoa(port)

	t.Logf("waiting for test server on %s", address)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	attempt := 0

	for {
		attempt++

		if ctx.Err() != nil {
			t.Fatalf(
				"timed out waiting for test server on %s after %d attempts",
				address,
				attempt,
			)
		}

		conn, err := net.DialTimeout(
			"tcp",
			address,
			100*time.Millisecond,
		)

		if err == nil {
			conn.Close()

			t.Logf(
				"test server accepted TCP connection after %d attempts",
				attempt,
			)

			return
		}

		if attempt == 1 || attempt%20 == 0 {
			t.Logf(
				"test server not ready yet (attempt %d): %v",
				attempt,
				err,
			)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func stopProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	if cmd == nil || cmd.Process == nil {
		t.Log("stopProcess: process is nil")
		return
	}

	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		t.Logf(
			"stopProcess: process already exited with status %d",
			cmd.ProcessState.ExitCode(),
		)
		return
	}

	t.Logf("sending SIGTERM to test server PID %d", cmd.Process.Pid)

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Log("test server exited while sending signal")
			return
		}

		t.Fatalf("failed to signal test server: %v", err)
	}

	t.Log("waiting for test server process to exit")

	done := make(chan error, 1)

	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("test server exited with error: %v", err)
		}

		t.Log("test server exited cleanly")

	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for test server to stop")
	}
}

func drainOutput(t *testing.T, r io.Reader, source string) {
	t.Helper()

	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		t.Logf("[%s] %s", source, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		t.Logf(
			"failed to read %s: %v",
			source,
			err,
		)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	return filepath.Dir(wd)
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
