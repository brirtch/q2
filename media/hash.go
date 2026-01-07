package media

import (
	"fmt"
	"io"
	"os"

	"github.com/cespare/xxhash/v2"
)

const (
	// HashBufferSize is the buffer size for reading files during hashing.
	// 1MB chunks for memory efficiency with large files.
	HashBufferSize = 1024 * 1024
)

// HashFile computes the xxhash (XXH64) of a file's contents.
// Returns the hash as a hex string.
func HashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := xxhash.New()
	buf := make([]byte, HashBufferSize)

	for {
		n, err := file.Read(buf)
		if n > 0 {
			if _, writeErr := hash.Write(buf[:n]); writeErr != nil {
				return "", fmt.Errorf("failed to hash data: %w", writeErr)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
	}

	return fmt.Sprintf("%016x", hash.Sum64()), nil
}

// HashString computes the xxhash (XXH64) of a string.
// Returns the hash as a hex string.
func HashString(s string) string {
	return fmt.Sprintf("%016x", xxhash.Sum64String(s))
}
