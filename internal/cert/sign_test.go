package cert

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestSign_Roundtrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	identity := "github.com/example/plugin@v1.0.0"
	sig := Sign(priv, identity)

	digest := identityDigest(identity)
	if !ed25519.Verify(pub, digest, sig) {
		t.Error("signature verification failed with correct key")
	}
}

func TestSign_DifferentIdentities(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	sig1 := Sign(priv, "github.com/a/b@v1.0.0")
	sig2 := Sign(priv, "github.com/a/b@v2.0.0")

	if string(sig1) == string(sig2) {
		t.Error("different identities should produce different signatures")
	}
}
