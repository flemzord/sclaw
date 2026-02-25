package cert

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestSign_Roundtrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	// Write a temp file to sign.
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.so")
	if err := os.WriteFile(path, []byte("plugin binary content"), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	sig, err := Sign(priv, path)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if !ed25519.Verify(pub, mustDigest(t, path), sig) {
		t.Error("signature verification failed with correct key")
	}
}

func TestSign_FileNotFound(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, err := Sign(priv, "/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func mustDigest(t *testing.T, path string) []byte {
	t.Helper()
	d, err := fileDigest(path)
	if err != nil {
		t.Fatalf("computing digest: %v", err)
	}
	return d
}
