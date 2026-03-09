package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/flemzord/sclaw/internal/tool"
	gomcp "github.com/mark3labs/mcp-go/mcp"
)

// nonAlphanumeric matches any character that is not a letter, digit, or underscore.
var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// ToolName returns the namespaced tool name: mcp_{server}_{tool}.
// Non-alphanumeric characters are replaced with underscores.
func ToolName(serverName, toolName string) string {
	s := nonAlphanumeric.ReplaceAllString(serverName, "_")
	t := nonAlphanumeric.ReplaceAllString(toolName, "_")
	return "mcp_" + s + "_" + t
}

// mcpTool adapts an MCP tool to the tool.Tool interface.
type mcpTool struct {
	namespacedName string
	mcpToolName    string
	description    string
	schema         json.RawMessage
	client         *Client
}

// NewTool creates a tool.Tool adapter for the given MCP tool.
func NewTool(serverName string, mt gomcp.Tool, client *Client) tool.Tool {
	schema := buildSchema(mt)

	desc := mt.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", mt.Name, serverName)
	}

	return &mcpTool{
		namespacedName: ToolName(serverName, mt.Name),
		mcpToolName:    mt.Name,
		description:    desc,
		schema:         schema,
		client:         client,
	}
}

func (t *mcpTool) Name() string                      { return t.namespacedName }
func (t *mcpTool) Description() string               { return t.description }
func (t *mcpTool) Schema() json.RawMessage           { return t.schema }
func (t *mcpTool) Scopes() []tool.Scope              { return []tool.Scope{tool.ScopeNetwork} }
func (t *mcpTool) DefaultPolicy() tool.ApprovalLevel { return tool.ApprovalAllow }

// Execute calls the MCP tool and formats the result as tool.Output.
func (t *mcpTool) Execute(ctx context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var argsMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return tool.Output{}, fmt.Errorf("mcp: invalid arguments for %s: %w", t.namespacedName, err)
		}
	}

	result, err := t.client.CallTool(ctx, t.mcpToolName, argsMap)
	if err != nil {
		return tool.Output{}, fmt.Errorf("mcp: calling %s: %w", t.namespacedName, err)
	}

	text := formatResult(result)
	return tool.Output{
		Content: text,
		IsError: result.IsError,
	}, nil
}

// formatResult extracts text content from a CallToolResult.
func formatResult(result *gomcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(gomcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// buildSchema converts the MCP tool's InputSchema to a JSON Schema byte slice.
func buildSchema(t gomcp.Tool) json.RawMessage {
	// Prefer raw schema if available.
	if len(t.RawInputSchema) > 0 {
		return json.RawMessage(t.RawInputSchema)
	}

	// Fall back to the structured InputSchema.
	data, err := json.Marshal(t.InputSchema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return data
}

// Compile-time check that mcpTool implements tool.Tool.
var _ tool.Tool = (*mcpTool)(nil)
