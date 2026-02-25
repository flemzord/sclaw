package cert

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestNewVerifier_NotRequired(t *testing.T) {
	v, err := NewVerifier(VerifyConfig{RequireCertified: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should accept anything when not required.
	if err := v.Verify("/any/path", nil); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestNewVerifier_RequiredNoKeys(t *testing.T) {
	_, err := NewVerifier(VerifyConfig{RequireCertified: true})
	if err == nil {
		t.Error("expected error when require_certified=true with no keys")
	}
}

func TestNewVerifier_InvalidKeyHex(t *testing.T) {
	_, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{"not-hex"},
	})
	if err == nil {
		t.Error("expected error for invalid hex key")
	}
}

func TestNewVerifier_InvalidKeySize(t *testing.T) {
	_, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString([]byte("short"))},
	})
	if err == nil {
		t.Error("expected error for wrong key size")
	}
}

func TestVerifier_ValidSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.so")
	if err := os.WriteFile(path, []byte("plugin content"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	sig, err := Sign(priv, path)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	v, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString(pub)},
	})
	if err != nil {
		t.Fatalf("creating verifier: %v", err)
	}

	if err := v.Verify(path, sig); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

func TestVerifier_InvalidSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.so")
	if err := os.WriteFile(path, []byte("plugin content"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	v, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString(pub)},
	})
	if err != nil {
		t.Fatalf("creating verifier: %v", err)
	}

	// Use garbage signature.
	if err := v.Verify(path, []byte("bad-signature")); err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestVerifier_UntrustedKey(t *testing.T) {
	// Sign with key A, verify with key B.
	_, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)

	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.so")
	if err := os.WriteFile(path, []byte("plugin content"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	sig, _ := Sign(privA, path)

	v, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString(pubB)},
	})
	if err != nil {
		t.Fatalf("creating verifier: %v", err)
	}

	if err := v.Verify(path, sig); err == nil {
		t.Error("expected error for untrusted key")
	}
}
