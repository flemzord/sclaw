package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
)

// maxDefSize is the maximum allowed size for a prompt cron definition file.
const maxDefSize = 256 << 10 // 256 KiB

// PromptCronDef is the on-disk JSON representation of a scheduled prompt.
type PromptCronDef struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Schedule    string            `json:"schedule"`
	Enabled     bool              `json:"enabled"`
	Prompt      string            `json:"prompt"`
	Tools       []string          `json:"tools,omitempty"`
	Loop        PromptCronLoop    `json:"loop,omitempty"`
	Output      *PromptCronOutput `json:"output,omitempty"`
}

// PromptCronLoop configures the agent loop for a prompt cron.
type PromptCronLoop struct {
	MaxIterations int    `json:"max_iterations,omitempty"`
	Timeout       string `json:"timeout,omitempty"`
}

// PromptCronOutput configures where to deliver the cron result.
type PromptCronOutput struct {
	Channel string `json:"channel"` // e.g. "channel.telegram"
	ChatID  string `json:"chat_id"` // e.g. "123456789"
}

// PromptCronResult is the last-run result stored on disk.
type PromptCronResult struct {
	Name        string `json:"name"`
	RanAt       string `json:"ran_at"`
	DurationMs  int64  `json:"duration_ms"`
	StopReason  string `json:"stop_reason"`
	Iterations  int    `json:"iterations"`
	ToolCalls   int    `json:"tool_calls"`
	TotalTokens int    `json:"total_tokens"`
	Content     string `json:"content"`
	Error       string `json:"error,omitempty"`
}

// LoopBuilder creates an agent.Loop for cron execution.
// Defined here to avoid a circular dependency on the multiagent package.
type LoopBuilder interface {
	BuildCronLoop(agentID string, toolFilter []string, loopOverrides agent.LoopConfig) (*agent.Loop, string, error)
}

// OutputSender sends cron results to a channel.
// Defined here to avoid a circular dependency on the channel package.
type OutputSender interface {
	SendCronOutput(ctx context.Context, channel, chatID, text string) error
}

// PromptJob executes a scheduled prompt through the agent loop.
type PromptJob struct {
	Def     PromptCronDef
	AgentID string
	Builder LoopBuilder
	Sender  OutputSender // nil = no channel output
	DataDir string
	Logger  *slog.Logger
}

// Compile-time interface check.
var _ Job = (*PromptJob)(nil)

// Name implements Job.
func (j *PromptJob) Name() string {
	return "prompt_cron:" + j.AgentID + ":" + j.Def.Name
}

// Schedule implements Job.
func (j *PromptJob) Schedule() string {
	return j.Def.Schedule
}

// Run implements Job.
func (j *PromptJob) Run(ctx context.Context) error {
	if !j.Def.Enabled {
		return nil
	}

	logger := j.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("prompt_cron: starting", "name", j.Def.Name, "agent", j.AgentID)

	// Build loop config from cron definition.
	var loopCfg agent.LoopConfig
	if j.Def.Loop.MaxIterations > 0 {
		loopCfg.MaxIterations = j.Def.Loop.MaxIterations
	}
	if j.Def.Loop.Timeout != "" {
		if d, err := time.ParseDuration(j.Def.Loop.Timeout); err == nil {
			loopCfg.Timeout = d
		}
	}

	loop, systemPrompt, err := j.Builder.BuildCronLoop(j.AgentID, j.Def.Tools, loopCfg)
	if err != nil {
		return fmt.Errorf("prompt_cron: building loop for %q: %w", j.Def.Name, err)
	}

	// Build request with the prompt as a user message.
	req := agent.Request{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: j.Def.Prompt},
		},
		SystemPrompt: systemPrompt,
		Tools:        loop.ToolDefinitions(),
	}

	startTime := time.Now()
	resp, runErr := loop.Run(ctx, req)
	duration := time.Since(startTime)

	// Build result.
	result := PromptCronResult{
		Name:        j.Def.Name,
		RanAt:       startTime.UTC().Format(time.RFC3339),
		DurationMs:  duration.Milliseconds(),
		StopReason:  string(resp.StopReason),
		Iterations:  resp.Iterations,
		ToolCalls:   len(resp.ToolCalls),
		TotalTokens: resp.TotalUsage.TotalTokens,
		Content:     resp.Content,
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}

	// Write result to disk.
	if saveErr := SaveResult(j.DataDir, result); saveErr != nil {
		logger.Error("prompt_cron: failed to save result", "name", j.Def.Name, "error", saveErr)
	}

	// Send output to channel if configured.
	if j.Def.Output != nil && j.Sender != nil && resp.Content != "" {
		if sendErr := j.Sender.SendCronOutput(ctx, j.Def.Output.Channel, j.Def.Output.ChatID, resp.Content); sendErr != nil {
			logger.Error("prompt_cron: failed to send output", "name", j.Def.Name, "error", sendErr)
		}
	}

	logger.Info("prompt_cron: completed",
		"name", j.Def.Name,
		"agent", j.AgentID,
		"stop_reason", result.StopReason,
		"iterations", result.Iterations,
		"tool_calls", result.ToolCalls,
		"duration_ms", result.DurationMs,
	)

	return runErr
}

// Validate checks that the definition has all required fields and valid values.
func (d *PromptCronDef) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("prompt cron: name is required")
	}
	if d.Schedule == "" {
		return fmt.Errorf("prompt cron %q: schedule is required", d.Name)
	}
	if d.Prompt == "" {
		return fmt.Errorf("prompt cron %q: prompt is required", d.Name)
	}
	if d.Loop.Timeout != "" {
		if _, err := time.ParseDuration(d.Loop.Timeout); err != nil {
			return fmt.Errorf("prompt cron %q: invalid timeout %q: %w", d.Name, d.Loop.Timeout, err)
		}
	}
	if d.Output != nil {
		if d.Output.Channel == "" || d.Output.ChatID == "" {
			return fmt.Errorf("prompt cron %q: output requires both channel and chat_id", d.Name)
		}
	}
	return nil
}

// LoadPromptCronDef reads and parses a prompt cron definition from a JSON file.
func LoadPromptCronDef(path string) (*PromptCronDef, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > maxDefSize {
		return nil, fmt.Errorf("prompt cron file %s exceeds max size (%d bytes)", path, maxDefSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var def PromptCronDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if err := def.Validate(); err != nil {
		return nil, err
	}

	return &def, nil
}

// SaveResult writes the last-run result to disk, overwriting any previous result.
func SaveResult(dataDir string, result PromptCronResult) error {
	dir := ResultsDir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating results dir: %w", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling result: %w", err)
	}

	path := filepath.Join(dir, result.Name+".json")
	return os.WriteFile(path, data, 0o644)
}

// CronsDir returns the prompt crons directory under the given data directory.
func CronsDir(dataDir string) string {
	return filepath.Join(dataDir, "crons")
}

// ResultsDir returns the cron results directory under the given data directory.
func ResultsDir(dataDir string) string {
	return filepath.Join(dataDir, "crons", "results")
}
