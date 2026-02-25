package cert

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestNewVerifier_NotRequired(t *testing.T) {
	v, err := NewVerifier(VerifyConfig{RequireCertified: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should accept anything when not required.
	if err := v.Verify("github.com/any/module@v1.0.0", nil); err != nil {
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

	identity := "github.com/example/plugin@v1.0.0"
	sig := Sign(priv, identity)

	v, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString(pub)},
	})
	if err != nil {
		t.Fatalf("creating verifier: %v", err)
	}

	if err := v.Verify(identity, sig); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

func TestVerifier_MissingSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	v, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString(pub)},
	})
	if err != nil {
		t.Fatalf("creating verifier: %v", err)
	}

	if err := v.Verify("github.com/example/plugin@v1.0.0", nil); err == nil {
		t.Error("expected error for missing signature")
	}
}

func TestVerifier_InvalidSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	v, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString(pub)},
	})
	if err != nil {
		t.Fatalf("creating verifier: %v", err)
	}

	if err := v.Verify("github.com/example/plugin@v1.0.0", []byte("bad-signature")); err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestVerifier_UntrustedKey(t *testing.T) {
	_, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)

	identity := "github.com/example/plugin@v1.0.0"
	sig := Sign(privA, identity)

	v, err := NewVerifier(VerifyConfig{
		RequireCertified: true,
		TrustedKeys:      []string{hex.EncodeToString(pubB)},
	})
	if err != nil {
		t.Fatalf("creating verifier: %v", err)
	}

	if err := v.Verify(identity, sig); err == nil {
		t.Error("expected error for untrusted key")
	}
}
