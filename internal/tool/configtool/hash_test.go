package configtool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := []byte("version: \"1\"\nmodules: {}\n")

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	hash, raw, err := fileHash(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(raw) != string(content) {
		t.Errorf("raw content mismatch: got %q, want %q", raw, content)
	}

	if hash == "" {
		t.Error("hash should not be empty")
	}

	// Hash should be deterministic.
	hash2, _, err := fileHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash != hash2 {
		t.Errorf("hash not deterministic: %s != %s", hash, hash2)
	}
}

func TestFileHashMissing(t *testing.T) {
	_, _, err := fileHash("/nonexistent/path/file.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestBytesHash(t *testing.T) {
	h1 := bytesHash([]byte("hello"))
	h2 := bytesHash([]byte("hello"))
	h3 := bytesHash([]byte("world"))

	if h1 != h2 {
		t.Errorf("same input should produce same hash: %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different input should produce different hash: %s == %s", h1, h3)
	}
	if len(h1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("unexpected hash length: %d", len(h1))
	}
}
