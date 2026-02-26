package cert

import "crypto/ed25519"

// Sign signs a module identity string (e.g. "github.com/example/plugin@v1.0.0")
// and returns its Ed25519 signature.
func Sign(privateKey ed25519.PrivateKey, moduleIdentity string) []byte {
	digest := identityDigest(moduleIdentity)
	return ed25519.Sign(privateKey, digest)
}
