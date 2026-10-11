package assembler

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// pattern returns n deterministic bytes.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*7 + 3) % 256)
	}
	return b
}

// newAssembler creates an Assembler over a temp file and registers cleanup.
func newAssembler(t *testing.T, size int64) (*Assembler, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.bin")
	a, err := New(path, size)
	if err != nil {
		t.Fatalf("New(%q, %d) = %v", path, size, err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", path, err)
	}
	return got
}

func TestSingleRangeWrite(t *testing.T) {
	content := pattern(100)
	a, path := newAssembler(t, 100)

	if err := a.WriteRange(0, 99, content); err != nil {
		t.Fatalf("WriteRange = %v, want nil", err)
	}
	if got := readFile(t, path); !bytes.Equal(got, content) {
		t.Fatalf("file contents do not match")
	}
}

func TestMultipleRangesInOrder(t *testing.T) {
	content := pattern(300)
	a, path := newAssembler(t, 300)

	for _, s := range []struct{ start, end int64 }{
		{0, 99}, {100, 199}, {200, 299},
	} {
		if err := a.WriteRange(s.start, s.end, content[s.start:s.end+1]); err != nil {
			t.Fatalf("WriteRange(%d,%d) = %v", s.start, s.end, err)
		}
	}

	if got := readFile(t, path); !bytes.Equal(got, content) {
		t.Fatalf("file contents do not match")
	}
}

func TestMultipleRangesOutOfOrder(t *testing.T) {
	content := pattern(300)
	a, path := newAssembler(t, 300)

	// Range 2 completes before range 1.
	for _, s := range []struct{ start, end int64 }{
		{200, 299}, {0, 99}, {100, 199},
	} {
		if err := a.WriteRange(s.start, s.end, content[s.start:s.end+1]); err != nil {
			t.Fatalf("WriteRange(%d,%d) = %v", s.start, s.end, err)
		}
	}

	if got := readFile(t, path); !bytes.Equal(got, content) {
		t.Fatalf("file contents do not match")
	}
}

func TestFirstMiddleFinalRanges(t *testing.T) {
	content := pattern(1000)
	a, path := newAssembler(t, 1000)

	for _, s := range []struct{ start, end int64 }{
		{400, 599}, // middle
		{0, 199},   // first
		{800, 999}, // final
	} {
		if err := a.WriteRange(s.start, s.end, content[s.start:s.end+1]); err != nil {
			t.Fatalf("WriteRange(%d,%d) = %v", s.start, s.end, err)
		}
	}

	got := readFile(t, path)
	if len(got) != 1000 {
		t.Fatalf("len(file) = %d, want 1000", len(got))
	}

	want := make([]byte, 1000)
	copy(want[0:200], content[0:200])
	copy(want[400:600], content[400:600])
	copy(want[800:1000], content[800:1000])

	if !bytes.Equal(got, want) {
		t.Fatalf("file contents do not match: written ranges wrong or gaps not zero")
	}
}

func TestInvalidRange(t *testing.T) {
	a, _ := newAssembler(t, 100)

	for _, tc := range []struct {
		name       string
		start, end int64
	}{
		{"negative start", -1, 5},
		{"end before start", 10, 5},
		{"end equals size", 0, 100},
		{"end beyond size", 50, 150},
		{"start beyond size", 100, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := a.WriteRange(tc.start, tc.end, nil); !errors.Is(err, ErrInvalidRange) {
				t.Fatalf("WriteRange(%d,%d) = %v, want ErrInvalidRange", tc.start, tc.end, err)
			}
		})
	}
}

func TestDataLengthMismatch(t *testing.T) {
	a, _ := newAssembler(t, 100)

	for _, tc := range []struct {
		name       string
		start, end int64
		data       []byte
	}{
		{"too short", 0, 9, make([]byte, 9)},
		{"too long", 0, 9, make([]byte, 11)},
		{"empty", 10, 19, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := a.WriteRange(tc.start, tc.end, tc.data); !errors.Is(err, ErrLengthMismatch) {
				t.Fatalf("WriteRange(%d,%d, %d bytes) = %v, want ErrLengthMismatch", tc.start, tc.end, len(tc.data), err)
			}
		})
	}
}

func TestCompleteFileReconstruction(t *testing.T) {
	const size = 1000
	content := pattern(size)
	a, path := newAssembler(t, size)

	segments := []struct{ start, end int64 }{
		{0, 49}, {50, 149}, {150, 150}, {151, 499}, {500, 999},
	}
	// Deliberately scrambled completion order.
	for _, i := range []int{3, 0, 4, 1, 2} {
		s := segments[i]
		if err := a.WriteRange(s.start, s.end, content[s.start:s.end+1]); err != nil {
			t.Fatalf("WriteRange(%d,%d) = %v", s.start, s.end, err)
		}
	}

	if got := readFile(t, path); !bytes.Equal(got, content) {
		t.Fatalf("reconstructed file does not match original bytes")
	}
}

func TestFileSizeEnsured(t *testing.T) {
	const size = 512
	a, path := newAssembler(t, size)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat = %v", err)
	}
	if info.Size() != size {
		t.Fatalf("file size after New = %d, want %d", info.Size(), size)
	}

	if err := a.WriteRange(100, 199, pattern(100)); err != nil {
		t.Fatalf("WriteRange = %v", err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat = %v", err)
	}
	if info.Size() != size {
		t.Fatalf("file size after partial write = %d, want %d", info.Size(), size)
	}
}

func TestClosedAssembler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.bin")
	a, err := New(path, 10)
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
	if err := a.WriteRange(0, 9, make([]byte, 10)); !errors.Is(err, ErrClosed) {
		t.Fatalf("WriteRange after Close = %v, want ErrClosed", err)
	}
}

func TestNegativeSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.bin")
	if _, err := New(path, -1); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("New(path, -1) = %v, want ErrInvalidSize", err)
	}
}

func TestOpenError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "out.bin")
	if _, err := New(path, 10); err == nil {
		t.Fatal("New in missing directory = nil, want error")
	}
}
