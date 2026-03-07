package mcp

import (
	"encoding/json"
	"testing"

	"github.com/flemzord/sclaw/internal/tool"
	gomcp "github.com/mark3labs/mcp-go/mcp"
)

func TestToolName(t *testing.T) {
	tests := []struct {
		server string
		tool   string
		want   string
	}{
		{"filesystem", "read_file", "mcp_filesystem_read_file"},
		{"my-server", "do.something", "mcp_my_server_do_something"},
		{"simple", "list", "mcp_simple_list"},
		{"a/b", "c:d", "mcp_a_b_c_d"},
	}

	for _, tt := range tests {
		got := ToolName(tt.server, tt.tool)
		if got != tt.want {
			t.Errorf("ToolName(%q, %q) = %q, want %q", tt.server, tt.tool, got, tt.want)
		}
	}
}

func TestNewTool_Interface(t *testing.T) {
	mt := gomcp.Tool{
		Name:        "read_file",
		Description: "Read a file from disk",
		InputSchema: gomcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
			Required:   []string{"path"},
		},
	}

	adapted := NewTool("fs", mt, nil)

	// Check interface compliance.
	var _ tool.Tool = adapted

	if adapted.Name() != "mcp_fs_read_file" {
		t.Errorf("Name() = %q, want %q", adapted.Name(), "mcp_fs_read_file")
	}

	if adapted.Description() != "Read a file from disk" {
		t.Errorf("Description() = %q", adapted.Description())
	}

	scopes := adapted.Scopes()
	if len(scopes) != 1 || scopes[0] != tool.ScopeNetwork {
		t.Errorf("Scopes() = %v, want [network]", scopes)
	}

	if adapted.DefaultPolicy() != tool.ApprovalAllow {
		t.Errorf("DefaultPolicy() = %v, want allow", adapted.DefaultPolicy())
	}

	schema := adapted.Schema()
	if len(schema) == 0 {
		t.Fatal("Schema() returned empty")
	}

	// Verify schema is valid JSON.
	var m map[string]interface{}
	if err := json.Unmarshal(schema, &m); err != nil {
		t.Fatalf("Schema() is not valid JSON: %v", err)
	}
	if m["type"] != "object" {
		t.Errorf("schema type = %v, want 'object'", m["type"])
	}
}

func TestNewTool_DefaultDescription(t *testing.T) {
	mt := gomcp.Tool{
		Name: "analyze",
	}

	adapted := NewTool("posthog", mt, nil)
	if adapted.Description() == "" {
		t.Fatal("expected non-empty default description")
	}
}

func TestBuildSchema_RawInputSchema(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}}}`)
	mt := gomcp.Tool{
		Name:           "test",
		RawInputSchema: raw,
	}

	schema := buildSchema(mt)
	if string(schema) != string(raw) {
		t.Errorf("buildSchema should prefer RawInputSchema, got %s", schema)
	}
}

func TestBuildSchema_FallbackToInputSchema(t *testing.T) {
	mt := gomcp.Tool{
		Name: "test",
		InputSchema: gomcp.ToolInputSchema{
			Type: "object",
		},
	}

	schema := buildSchema(mt)
	if len(schema) == 0 {
		t.Fatal("buildSchema returned empty for InputSchema fallback")
	}

	var m map[string]interface{}
	if err := json.Unmarshal(schema, &m); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
}

func TestFormatResult_Nil(t *testing.T) {
	if got := formatResult(nil); got != "" {
		t.Errorf("formatResult(nil) = %q, want empty", got)
	}
}

func TestFormatResult_TextContent(t *testing.T) {
	result := &gomcp.CallToolResult{
		Content: []gomcp.Content{
			gomcp.TextContent{Text: "line1"},
			gomcp.TextContent{Text: "line2"},
		},
	}
	got := formatResult(result)
	if got != "line1\nline2" {
		t.Errorf("formatResult = %q, want 'line1\\nline2'", got)
	}
}
