// Package cert provides Ed25519 plugin certification: signing and verification.
//
// Plugins in sclaw are Go modules composed at compile time (not loaded at
// runtime), so certification operates on module identity strings
// (e.g. "github.com/example/plugin@v1.0.0") rather than on binary files.
package cert

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// VerifyConfig controls plugin certification behaviour.
type VerifyConfig struct {
	// RequireCertified rejects unsigned plugins when true.
	RequireCertified bool

	// TrustedKeys is a list of hex-encoded Ed25519 public keys.
	TrustedKeys []string
}

// Verifier checks Ed25519 signatures for plugin module identities.
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

// Verify checks that the module identity has a valid Ed25519 signature from
// one of the trusted keys. Returns nil if certification is not required.
func (v *Verifier) Verify(moduleIdentity string, signature []byte) error {
	if !v.required {
		return nil
	}

	if len(signature) == 0 {
		return fmt.Errorf("plugin %s: signature required but not provided", moduleIdentity)
	}

	digest := identityDigest(moduleIdentity)

	for _, key := range v.keys {
		if ed25519.Verify(key, digest, signature) {
			return nil
		}
	}

	return fmt.Errorf("no trusted key verified signature for %s", moduleIdentity)
}

// identityDigest returns the SHA-256 hash of a module identity string.
func identityDigest(identity string) []byte {
	hash := sha256.Sum256([]byte(identity))
	return hash[:]
}
