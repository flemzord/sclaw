package tool

import (
	"fmt"
	"strings"
)

// ApprovalLevel defines how a tool invocation is handled.
type ApprovalLevel string

const (
	// ApprovalAllow permits tool execution without user confirmation.
	ApprovalAllow ApprovalLevel = "allow"

	// ApprovalAsk requires user confirmation before execution.
	ApprovalAsk ApprovalLevel = "ask"

	// ApprovalDeny blocks tool execution entirely.
	ApprovalDeny ApprovalLevel = "deny"
)

// PolicyContext describes the context in which a tool is invoked.
type PolicyContext string

// PolicyContext values for different conversation types.
const (
	PolicyContextDM    PolicyContext = "dm"
	PolicyContextGroup PolicyContext = "group"
)

// Policy defines the approval settings for a context.
type Policy struct {
	// Default is the fallback approval level for tools not explicitly listed.
	Default ApprovalLevel

	// Tools maps tool names to explicit approval levels.
	Tools map[string]ApprovalLevel

	// Allow lists tools that can execute without confirmation.
	Allow []string

	// Ask lists tools that require confirmation before execution.
	Ask []string

	// Deny lists tools that must never execute.
	Deny []string
}

// PolicyConfig holds policies for each context type.
type PolicyConfig struct {
	DM    Policy
	Group Policy
}

// ResolvePolicy determines the effective approval level for a tool.
// Resolution order: explicit tool mapping > context default > tool's DefaultPolicy.
func ResolvePolicy(cfg PolicyConfig, ctx PolicyContext, t Tool) ApprovalLevel {
	var policy Policy
	switch ctx {
	case PolicyContextDM:
		policy = cfg.DM
	case PolicyContextGroup:
		policy = cfg.Group
	default:
		return t.DefaultPolicy()
	}

	// Check explicit tool mapping first.
	toolName := strings.TrimSpace(t.Name())
	if level, ok := resolveExplicitLevel(policy, toolName); ok {
		return level
	}

	// Fall back to context default if set.
	if policy.Default != "" {
		return policy.Default
	}

	// Fall back to the tool's own default.
	return t.DefaultPolicy()
}

// ValidatePolicyConfig checks that no tool appears with conflicting assignments
// within the same context (e.g., listed in both allow and deny).
func ValidatePolicyConfig(cfg PolicyConfig) error {
	if err := validatePolicy(cfg.DM, "dm"); err != nil {
		return err
	}
	return validatePolicy(cfg.Group, "group")
}

func validatePolicy(policy Policy, ctx string) error {
	// Check that the default level is valid if set.
	if policy.Default != "" {
		if !isValidApprovalLevel(policy.Default) {
			return fmt.Errorf("policy %s: invalid default level %q", ctx, policy.Default)
		}
	}

	// Check that all tool-level assignments are valid.
	explicit := make(map[string]ApprovalLevel)
	for name, level := range policy.Tools {
		toolName := strings.TrimSpace(name)
		if toolName == "" {
			return fmt.Errorf("policy %s: tool mapping has empty name", ctx)
		}
		if !isValidApprovalLevel(level) {
			return fmt.Errorf("policy %s: tool %q has invalid level %q", ctx, toolName, level)
		}
		explicit[toolName] = level
	}

	if err := validatePolicyList(policy.Allow, ApprovalAllow, "allow", explicit, ctx); err != nil {
		return err
	}
	if err := validatePolicyList(policy.Ask, ApprovalAsk, "ask", explicit, ctx); err != nil {
		return err
	}
	if err := validatePolicyList(policy.Deny, ApprovalDeny, "deny", explicit, ctx); err != nil {
		return err
	}

	return nil
}

func resolveExplicitLevel(policy Policy, toolName string) (ApprovalLevel, bool) {
	for name, level := range policy.Tools {
		if strings.TrimSpace(name) == toolName {
			return level, true
		}
	}
	if toolInList(policy.Allow, toolName) {
		return ApprovalAllow, true
	}
	if toolInList(policy.Ask, toolName) {
		return ApprovalAsk, true
	}
	if toolInList(policy.Deny, toolName) {
		return ApprovalDeny, true
	}
	return "", false
}

func validatePolicyList(
	names []string,
	level ApprovalLevel,
	listName string,
	explicit map[string]ApprovalLevel,
	ctx string,
) error {
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return fmt.Errorf("policy %s: %s list contains empty tool name", ctx, listName)
		}
		if existing, ok := explicit[name]; ok && existing != level {
			return fmt.Errorf("%w: policy %s: tool %q appears in both %q and %q", ErrToolInMultipleLists, ctx, name, existing, level)
		}
		explicit[name] = level
	}
	return nil
}

func toolInList(list []string, name string) bool {
	for _, candidate := range list {
		if strings.TrimSpace(candidate) == name {
			return true
		}
	}
	return false
}

func isValidApprovalLevel(level ApprovalLevel) bool {
	switch level {
	case ApprovalAllow, ApprovalAsk, ApprovalDeny:
		return true
	default:
		return false
	}
}
