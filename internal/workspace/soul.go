package workspace

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultSoulPrompt is used when no SOUL.md file is found or the file is empty.
const DefaultSoulPrompt = "You are a helpful assistant."

// SoulProvider is the interface for loading the agent personality prompt.
// Extracted for testability (mock in pipeline tests).
type SoulProvider interface {
	Load() (string, error)
}

// SoulLoader implements SoulProvider with stat-based cache invalidation.
// On every Load() call it stats the file (~1µs, negligible vs LLM call).
// If the file changed, the content is re-read; otherwise the cached value
// is returned via the RLock fast path.
type SoulLoader struct {
	path string

	mu       sync.RWMutex
	content  string
	modTime  time.Time
	notFound bool
}

// NewSoulLoader creates a SoulLoader for the given SOUL.md path.
func NewSoulLoader(soulPath string) *SoulLoader {
	return &SoulLoader{path: soulPath}
}

// Load returns the current SOUL.md content, hot-reloading on file changes.
//
// Behavior:
//   - File missing → DefaultSoulPrompt, no error.
//   - File empty   → DefaultSoulPrompt, no error.
//   - ModTime unchanged → cached content (RLock fast path).
//   - ModTime changed   → re-read file + update cache (Lock).
func (s *SoulLoader) Load() (string, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.markNotFound()
			return DefaultSoulPrompt, nil
		}
		return "", err
	}

	modTime := info.ModTime()

	// Fast path: check if cached content is still valid.
	s.mu.RLock()
	if !s.notFound && s.modTime.Equal(modTime) && s.content != "" {
		cached := s.content
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	// Slow path: re-read the file.
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.markNotFound()
			return DefaultSoulPrompt, nil
		}
		return "", err
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		s.markNotFound()
		return DefaultSoulPrompt, nil
	}

	s.mu.Lock()
	s.content = content
	s.modTime = modTime
	s.notFound = false
	s.mu.Unlock()

	return content, nil
}

// markNotFound updates the cache to reflect a missing or empty file.
func (s *SoulLoader) markNotFound() {
	s.mu.Lock()
	s.notFound = true
	s.content = ""
	s.modTime = time.Time{}
	s.mu.Unlock()
}
