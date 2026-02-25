// Package cert provides Ed25519 plugin certification: signing and verification.
package cert

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// VerifyConfig controls plugin certification behaviour.
type VerifyConfig struct {
	// RequireCertified rejects unsigned plugins when true.
	RequireCertified bool

	// TrustedKeys is a list of hex-encoded Ed25519 public keys.
	TrustedKeys []string
}

// Verifier checks Ed25519 signatures for plugin files.
type Verifier struct {
	required bool
	keys     []ed25519.PublicKey
}

// NewVerifier creates a Verifier from the given config.
// Returns an error if RequireCertified is true but no valid keys are provided.
func NewVerifier(cfg VerifyConfig) (*Verifier, error) {
	if !cfg.RequireCertified {
		return &Verifier{required: false}, nil
	}

	keys := make([]ed25519.PublicKey, 0, len(cfg.TrustedKeys))
	for _, hexKey := range cfg.TrustedKeys {
		raw, err := hex.DecodeString(hexKey)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted key %q: %w", hexKey, err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid key size for %q: got %d, want %d", hexKey, len(raw), ed25519.PublicKeySize)
		}
		keys = append(keys, ed25519.PublicKey(raw))
	}

	if len(keys) == 0 {
		return nil, errors.New("require_certified is true but no trusted keys provided")
	}

	return &Verifier{required: true, keys: keys}, nil
}

// Verify checks that the file at pluginPath has a valid Ed25519 signature
// from one of the trusted keys. Returns nil if certification is not required.
func (v *Verifier) Verify(pluginPath string, signature []byte) error {
	if !v.required {
		return nil
	}

	digest, err := fileDigest(pluginPath)
	if err != nil {
		return fmt.Errorf("computing digest: %w", err)
	}

	for _, key := range v.keys {
		if ed25519.Verify(key, digest, signature) {
			return nil
		}
	}

	return fmt.Errorf("no trusted key verified signature for %s", pluginPath)
}

// fileDigest returns the SHA-256 hash of the file at the given path.
func fileDigest(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return hash[:], nil
}
