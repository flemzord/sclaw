package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// opPattern matches op://vault/item/field and op://vault/item/section/field references.
var opPattern = regexp.MustCompile(`op://[^\s"'\x60,\]\}]+`)

// opLookPathFunc checks whether the 1Password CLI is available.
// It is a package variable so tests can replace it.
var opLookPathFunc = func() error {
	_, err := exec.LookPath("op")
	return err
}

// opRunnerFunc executes "op read" for a single reference and returns the secret value.
// It is a package variable so tests can replace it without touching the real CLI.
var opRunnerFunc = opCLIRunner

// opCLIRunner is the production implementation that shells out to the 1Password CLI.
func opCLIRunner(ref string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "op", "read", "--no-newline", ref)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("op read %s: %s", ref, bytes.TrimSpace(exitErr.Stderr))
		}
		return "", fmt.Errorf("op read %s: %w", ref, err)
	}
	return string(out), nil
}

// resolveOnePassword replaces all op:// references in raw YAML bytes with
// their resolved secret values obtained via the 1Password CLI.
// If no op:// references are found, the input is returned unchanged without
// checking for the op binary.
func resolveOnePassword(raw []byte) ([]byte, error) {
	matches := opPattern.FindAll(raw, -1)
	if len(matches) == 0 {
		return raw, nil
	}

	// Verify the op CLI is available.
	if err := opLookPathFunc(); err != nil {
		return nil, fmt.Errorf("config contains op:// references but 1Password CLI is not installed: %w", err)
	}

	// Deduplicate references.
	unique := make(map[string]struct{})
	for _, m := range matches {
		unique[string(m)] = struct{}{}
	}

	// Validate and resolve each unique reference.
	resolved := make(map[string]string, len(unique))
	var errs []error
	for ref := range unique {
		// op:// requires at least 3 segments: vault/item/field.
		// Strip the "op://" prefix and count path segments.
		segments := strings.Split(strings.TrimPrefix(ref, "op://"), "/")
		if len(segments) < 3 {
			errs = append(errs, fmt.Errorf(
				"invalid 1password reference %q: expected op://vault/item/field (got %d segments, need at least 3)",
				ref, len(segments)))
			continue
		}

		val, err := opRunnerFunc(ref)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		resolved[ref] = val
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	// Replace all occurrences in the raw bytes.
	result := raw
	for ref, val := range resolved {
		result = bytes.ReplaceAll(result, []byte(ref), []byte(val))
	}

	return result, nil
}
