package configtool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// fileHash reads the file at path and returns its SHA-256 hex digest
// along with the raw bytes. The hash is computed on raw bytes (before
// any env expansion) so it is deterministic and environment-independent.
func fileHash(path string) (hash string, raw []byte, err error) {
	raw, err = os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading config file: %w", err)
	}
	return bytesHash(raw), raw, nil
}

// bytesHash returns the SHA-256 hex digest of the given bytes.
func bytesHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
