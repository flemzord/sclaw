package workspace

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSoulLoader_FileAbsent(t *testing.T) {
	t.Parallel()

	loader := NewSoulLoader(filepath.Join(t.TempDir(), "SOUL.md"))

	content, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if content != DefaultSoulPrompt {
		t.Errorf("content = %q, want %q", content, DefaultSoulPrompt)
	}
}

func TestSoulLoader_FilePresent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(path, []byte("You are a pirate."), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewSoulLoader(path)

	content, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if content != "You are a pirate." {
		t.Errorf("content = %q, want %q", content, "You are a pirate.")
	}
}

func TestSoulLoader_EmptyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewSoulLoader(path)

	content, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if content != DefaultSoulPrompt {
		t.Errorf("content = %q, want %q", content, DefaultSoulPrompt)
	}
}

func TestSoulLoader_WhitespaceOnlyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(path, []byte("   \n\t  \n  "), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewSoulLoader(path)

	content, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if content != DefaultSoulPrompt {
		t.Errorf("content = %q, want %q (whitespace-only file)", content, DefaultSoulPrompt)
	}
}

func TestSoulLoader_ContentChangeDetected(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(path, []byte("Version 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewSoulLoader(path)

	content, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if content != "Version 1" {
		t.Fatalf("content = %q, want %q", content, "Version 1")
	}

	// Overwrite with new content.
	if err := os.WriteFile(path, []byte("Version 2"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, err = loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if content != "Version 2" {
		t.Errorf("content = %q, want %q after update", content, "Version 2")
	}
}

func TestSoulLoader_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(path, []byte("Concurrent soul"), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewSoulLoader(path)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			content, err := loader.Load()
			if err != nil {
				t.Errorf("Load() error: %v", err)
				return
			}
			if content != "Concurrent soul" {
				t.Errorf("content = %q, want %q", content, "Concurrent soul")
			}
		}()
	}
	wg.Wait()
}
