package cert

import (
	"crypto/ed25519"
	"fmt"
)

// Sign computes the SHA-256 digest of the file at pluginPath and returns
// its Ed25519 signature.
func Sign(privateKey ed25519.PrivateKey, pluginPath string) ([]byte, error) {
	digest, err := fileDigest(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("computing digest: %w", err)
	}
	return ed25519.Sign(privateKey, digest), nil
}
