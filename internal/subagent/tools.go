package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flemzord/sclaw/internal/tool"
)

// RegisterTools registers sub-agent tools on the given registry.
// If isSubAgent is true, only read-only tools are registered (preventing recursive spawning).
func RegisterTools(registry *tool.Registry, mgr *Manager, parentID string, isSubAgent bool) error {
	// Always register read-only tools.
	readOnly := []tool.Tool{
		newSessionsListTool(mgr, parentID),
		newSessionsHistoryTool(mgr),
	}
	for _, t := range readOnly {
		if err := registry.Register(t); err != nil {
			return err
		}
	}

	if isSubAgent {
		return nil
	}

	// Register exec tools only for parent agents.
	exec := []tool.Tool{
		newSessionsSpawnTool(mgr, parentID),
		newSessionsSendTool(mgr),
		newSessionsKillTool(mgr),
	}
	for _, t := range exec {
		if err := registry.Register(t); err != nil {
			return err
		}
	}

	return nil
}

// --- sessions_spawn ---

type sessionsSpawnTool struct {
	mgr      *Manager
	parentID string
}

func newSessionsSpawnTool(mgr *Manager, parentID string) *sessionsSpawnTool {
	return &sessionsSpawnTool{mgr: mgr, parentID: parentID}
}

func (t *sessionsSpawnTool) Name() string         { return "sessions_spawn" }
func (t *sessionsSpawnTool) Description() string  { return "Spawn a new sub-agent session." }
func (t *sessionsSpawnTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeExec} }
func (t *sessionsSpawnTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAsk
}

func (t *sessionsSpawnTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"system_prompt": {"type": "string", "description": "System prompt for the sub-agent."},
			"initial_message": {"type": "string", "description": "First user message to send to the sub-agent."},
			"timeout_seconds": {"type": "integer", "description": "Optional timeout in seconds. Uses default if omitted."}
		},
		"required": ["system_prompt", "initial_message"]
	}`)
}

type spawnArgs struct {
	SystemPrompt   string `json:"system_prompt"`
	InitialMessage string `json:"initial_message"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func (t *sessionsSpawnTool) Execute(ctx context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var a spawnArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	var timeout time.Duration
	if a.TimeoutSeconds > 0 {
		timeout = time.Duration(a.TimeoutSeconds) * time.Second
	}

	id, err := t.mgr.Spawn(ctx, SpawnRequest{
		ParentID:       t.parentID,
		SystemPrompt:   a.SystemPrompt,
		InitialMessage: a.InitialMessage,
		Timeout:        timeout,
	})
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("spawn failed: %v", err), IsError: true}, nil
	}

	result, _ := json.Marshal(map[string]string{
		"id":     id,
		"status": string(StatusRunning),
	})
	return tool.Output{Content: string(result)}, nil
}

// --- sessions_send ---

type sessionsSendTool struct {
	mgr *Manager
}

func newSessionsSendTool(mgr *Manager) *sessionsSendTool {
	return &sessionsSendTool{mgr: mgr}
}

func (t *sessionsSendTool) Name() string         { return "sessions_send" }
func (t *sessionsSendTool) Description() string  { return "Send a message to a running sub-agent." }
func (t *sessionsSendTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeExec} }
func (t *sessionsSendTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
}

func (t *sessionsSendTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "Sub-agent ID."},
			"message": {"type": "string", "description": "Message to send."}
		},
		"required": ["id", "message"]
	}`)
}

type sendArgs struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

func (t *sessionsSendTool) Execute(ctx context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var a sendArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if err := t.mgr.Send(ctx, a.ID, a.Message); err != nil {
		return tool.Output{Content: fmt.Sprintf("send failed: %v", err), IsError: true}, nil
	}

	result, _ := json.Marshal(map[string]bool{"ok": true})
	return tool.Output{Content: string(result)}, nil
}

// --- sessions_list ---

type sessionsListTool struct {
	mgr      *Manager
	parentID string
}

func newSessionsListTool(mgr *Manager, parentID string) *sessionsListTool {
	return &sessionsListTool{mgr: mgr, parentID: parentID}
}

func (t *sessionsListTool) Name() string { return "sessions_list" }
func (t *sessionsListTool) Description() string {
	return "List all sub-agent sessions for the current parent."
}
func (t *sessionsListTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeReadOnly} }
func (t *sessionsListTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
}

func (t *sessionsListTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`)
}

func (t *sessionsListTool) Execute(_ context.Context, _ json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	snapshots := t.mgr.List(t.parentID)
	data, err := json.Marshal(snapshots)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("marshal failed: %v", err), IsError: true}, nil
	}
	return tool.Output{Content: string(data)}, nil
}

// --- sessions_history ---

type sessionsHistoryTool struct {
	mgr *Manager
}

func newSessionsHistoryTool(mgr *Manager) *sessionsHistoryTool {
	return &sessionsHistoryTool{mgr: mgr}
}

func (t *sessionsHistoryTool) Name() string { return "sessions_history" }
func (t *sessionsHistoryTool) Description() string {
	return "Get the full history and state of a sub-agent."
}
func (t *sessionsHistoryTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeReadOnly} }
func (t *sessionsHistoryTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
}

func (t *sessionsHistoryTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "Sub-agent ID."}
		},
		"required": ["id"]
	}`)
}

type historyArgs struct {
	ID string `json:"id"`
}

func (t *sessionsHistoryTool) Execute(_ context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var a historyArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	snap, err := t.mgr.History(a.ID)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("history failed: %v", err), IsError: true}, nil
	}

	data, marshalErr := json.Marshal(snap)
	if marshalErr != nil {
		return tool.Output{Content: fmt.Sprintf("marshal failed: %v", marshalErr), IsError: true}, nil
	}
	return tool.Output{Content: string(data)}, nil
}

// --- sessions_kill ---

type sessionsKillTool struct {
	mgr *Manager
}

func newSessionsKillTool(mgr *Manager) *sessionsKillTool {
	return &sessionsKillTool{mgr: mgr}
}

func (t *sessionsKillTool) Name() string         { return "sessions_kill" }
func (t *sessionsKillTool) Description() string  { return "Kill a running sub-agent session." }
func (t *sessionsKillTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeExec} }
func (t *sessionsKillTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAsk
}

func (t *sessionsKillTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "Sub-agent ID to kill."}
		},
		"required": ["id"]
	}`)
}

type killArgs struct {
	ID string `json:"id"`
}

func (t *sessionsKillTool) Execute(_ context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var a killArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if err := t.mgr.Kill(a.ID); err != nil {
		return tool.Output{Content: fmt.Sprintf("kill failed: %v", err), IsError: true}, nil
	}

	result, _ := json.Marshal(map[string]bool{"ok": true})
	return tool.Output{Content: string(result)}, nil
}
