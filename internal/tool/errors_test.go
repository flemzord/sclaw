package tool

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors_Distinct(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		ErrToolNotFound,
		ErrDenied,
		ErrApprovalTimeout,
		ErrNoScopes,
		ErrEmptyToolName,
		ErrDuplicateTool,
		ErrToolInMultipleLists,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors should be distinct: %v == %v", a, b)
			}
		}
	}
}

func TestSentinelErrors_Wrappable(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("context: %w", ErrToolNotFound)
	if !errors.Is(wrapped, ErrToolNotFound) {
		t.Error("wrapped error should match ErrToolNotFound")
	}
}

func TestSentinelErrors_Messages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want string
	}{
		{ErrToolNotFound, "tool not found"},
		{ErrDenied, "tool execution denied by policy"},
		{ErrApprovalTimeout, "approval request timed out"},
		{ErrNoScopes, "tool must declare at least one scope"},
		{ErrEmptyToolName, "tool name must not be empty"},
		{ErrDuplicateTool, "tool already registered"},
		{ErrToolInMultipleLists, "tool appears in conflicting policy lists"},
	}

	for _, tt := range tests {
		if tt.err.Error() != tt.want {
			t.Errorf("got %q, want %q", tt.err.Error(), tt.want)
		}
	}
}
