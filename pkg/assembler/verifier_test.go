package assembler_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"adaptive-downloader/pkg/assembler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifySHA256(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "sample.txt")
	content := []byte("The quick brown fox jumps over the lazy dog")

	// Calculate known SHA-256 hash for sample content
	sum := sha256.Sum256(content)
	validHash := hex.EncodeToString(sum[:])

	// Write test file to disk
	err := os.WriteFile(filePath, content, 0o644)
	require.NoError(t, err, "failed to create test file")

	t.Run("Valid Hash Match", func(t *testing.T) {
		match, err := assembler.VerifySHA256Sum(filePath, validHash)
		require.NoError(t, err)
		assert.True(t, match, "expected valid SHA-256 hash to match")
	})

	t.Run("Hash Mismatch", func(t *testing.T) {
		invalidHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		match, err := assembler.VerifySHA256Sum(filePath, invalidHash)
		require.NoError(t, err)
		assert.False(t, match, "expected mismatched hash to return false")
	})

	t.Run("File Not Found", func(t *testing.T) {
		missingPath := filepath.Join(tempDir, "missing.txt")
		_, err := assembler.VerifySHA256Sum(missingPath, validHash)
		assert.Error(t, err, "expected error when file does not exist")
	})
}
