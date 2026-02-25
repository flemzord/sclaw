// Package bootstrap provides build-hash comparison and process re-execution
// for hot plugin reload.
package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

// BuildHash returns a deterministic SHA-256 hex digest for the given plugin
// list. The list is sorted before hashing so the result is order-independent.
func BuildHash(plugins []string) string {
	sorted := make([]string, len(plugins))
	copy(sorted, plugins)
	slices.Sort(sorted)

	h := sha256.New()
	for _, p := range sorted {
		h.Write([]byte(p))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
