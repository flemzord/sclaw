package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanPromptCrons reads all *.json files from dir and returns parsed defs.
// Invalid files are collected in the errors slice (partial failure).
// If the directory does not exist, both slices are nil.
func ScanPromptCrons(dir string) (defs []PromptCronDef, errs []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("reading crons dir %s: %w", dir, err)}
	}

	for _, entry := range entries {
		// Skip directories (e.g. results/) and non-JSON files.
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		def, err := LoadPromptCronDef(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("file %s: %w", entry.Name(), err))
			continue
		}
		defs = append(defs, *def)
	}

	return defs, errs
}
