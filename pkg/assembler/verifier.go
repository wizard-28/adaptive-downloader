package assembler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

func VerifySHA256Sum(path string, expectedHash string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("failed to open file for hash verification: %w	", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false, fmt.Errorf("failed to calculate hash: %w", err)
	}

	computedHash := hex.EncodeToString(hasher.Sum(nil))
	return computedHash == expectedHash, nil
}
