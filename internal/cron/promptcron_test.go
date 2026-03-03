package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
)

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}
}

func TestPromptCronDef_Validate(t *testing.T) {
	tests := []struct {
		name    string
		def     PromptCronDef
		wantErr bool
	}{
		{
			name:    "valid minimal",
			def:     PromptCronDef{Name: "test", Schedule: "* * * * *", Prompt: "hello"},
			wantErr: false,
		},
		{
			name:    "missing name",
			def:     PromptCronDef{Schedule: "* * * * *", Prompt: "hello"},
			wantErr: true,
		},
		{
			name:    "missing schedule",
			def:     PromptCronDef{Name: "test", Prompt: "hello"},
			wantErr: true,
		},
		{
			name:    "missing prompt",
			def:     PromptCronDef{Name: "test", Schedule: "* * * * *"},
			wantErr: true,
		},
		{
			name:    "invalid timeout",
			def:     PromptCronDef{Name: "test", Schedule: "* * * * *", Prompt: "hello", Loop: PromptCronLoop{Timeout: "not-a-duration"}},
			wantErr: true,
		},
		{
			name:    "valid timeout",
			def:     PromptCronDef{Name: "test", Schedule: "* * * * *", Prompt: "hello", Loop: PromptCronLoop{Timeout: "2m"}},
			wantErr: false,
		},
		{
			name:    "output missing channel",
			def:     PromptCronDef{Name: "test", Schedule: "* * * * *", Prompt: "hello", Output: &PromptCronOutput{ChatID: "123"}},
			wantErr: true,
		},
		{
			name:    "output missing chat_id",
			def:     PromptCronDef{Name: "test", Schedule: "* * * * *", Prompt: "hello", Output: &PromptCronOutput{Channel: "channel.telegram"}},
			wantErr: true,
		},
		{
			name:    "valid output",
			def:     PromptCronDef{Name: "test", Schedule: "* * * * *", Prompt: "hello", Output: &PromptCronOutput{Channel: "channel.telegram", ChatID: "123"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.def.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadPromptCronDef(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		def := PromptCronDef{
			Name:     "test",
			Schedule: "0 9 * * *",
			Enabled:  true,
			Prompt:   "Analyze tools",
		}
		data, _ := json.MarshalIndent(def, "", "  ")
		writeTestFile(t, path, data)

		loaded, err := LoadPromptCronDef(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if loaded.Name != "test" {
			t.Errorf("got name %q, want %q", loaded.Name, "test")
		}
	})

	t.Run("file too large", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "big.json")
		data := make([]byte, maxDefSize+1)
		writeTestFile(t, path, data)

		_, err := LoadPromptCronDef(path)
		if err == nil {
			t.Fatal("expected error for oversized file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.json")
		writeTestFile(t, path, []byte("{invalid"))

		_, err := LoadPromptCronDef(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.json")
		writeTestFile(t, path, []byte(`{"name":"test"}`))

		_, err := LoadPromptCronDef(path)
		if err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func TestPromptJob_Name(t *testing.T) {
	j := &PromptJob{
		Def:     PromptCronDef{Name: "tools-analyzer"},
		AgentID: "main",
	}
	got := j.Name()
	want := "prompt_cron:main:tools-analyzer"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestPromptJob_Run_Disabled(t *testing.T) {
	j := &PromptJob{
		Def: PromptCronDef{Name: "disabled", Enabled: false},
	}
	err := j.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error for disabled job: %v", err)
	}
}

// mockLoopBuilder is a test double for cron.LoopBuilder.
type mockLoopBuilder struct {
	resp agent.Response
	err  error
}

func (m *mockLoopBuilder) BuildCronLoop(_ string, _ []string, _ agent.LoopConfig) (*agent.Loop, string, error) {
	if m.err != nil {
		return nil, "", m.err
	}
	// Create a minimal loop with a mock provider that returns our canned response.
	p := &mockProvider{resp: m.resp}
	loop := agent.NewLoop(p, nil, agent.LoopConfig{
		MaxIterations: 1,
		Timeout:       10 * time.Second,
	})
	return loop, "You are a test agent.", nil
}

// mockProvider returns a canned response from Complete.
type mockProvider struct {
	resp agent.Response
}

func (p *mockProvider) Complete(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{
		Content: p.resp.Content,
		Usage:   provider.TokenUsage{TotalTokens: p.resp.TotalUsage.TotalTokens},
	}, nil
}

func (p *mockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Content: p.resp.Content}
	close(ch)
	return ch, nil
}

func (p *mockProvider) ContextWindowSize() int              { return 4096 }
func (p *mockProvider) ModelName() string                   { return "test-model" }
func (p *mockProvider) HealthCheck(_ context.Context) error { return nil }

// mockOutputSender records calls to SendCronOutput.
type mockOutputSender struct {
	calls []sendCall
}

type sendCall struct {
	channel, chatID, text string
}

func (m *mockOutputSender) SendCronOutput(_ context.Context, ch, chatID, text string) error {
	m.calls = append(m.calls, sendCall{channel: ch, chatID: chatID, text: text})
	return nil
}

func TestPromptJob_Run_Success(t *testing.T) {
	dir := t.TempDir()

	sender := &mockOutputSender{}
	builder := &mockLoopBuilder{
		resp: agent.Response{
			Content:    "Analysis complete.",
			Iterations: 1,
			StopReason: agent.StopReasonComplete,
			TotalUsage: provider.TokenUsage{TotalTokens: 100},
		},
	}

	j := &PromptJob{
		Def: PromptCronDef{
			Name:     "test-cron",
			Schedule: "* * * * *",
			Enabled:  true,
			Prompt:   "Analyze tools",
			Output:   &PromptCronOutput{Channel: "channel.telegram", ChatID: "12345"},
		},
		AgentID: "main",
		Builder: builder,
		Sender:  sender,
		DataDir: dir,
	}

	err := j.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify result file was written.
	resultPath := filepath.Join(ResultsDir(dir), "test-cron.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("result file not found: %v", err)
	}

	var result PromptCronResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}
	if result.Name != "test-cron" {
		t.Errorf("result name = %q, want %q", result.Name, "test-cron")
	}
	if result.Content != "Analysis complete." {
		t.Errorf("result content = %q, want %q", result.Content, "Analysis complete.")
	}
	if result.StopReason != "complete" {
		t.Errorf("result stop_reason = %q, want %q", result.StopReason, "complete")
	}

	// Verify output was sent.
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(sender.calls))
	}
	if sender.calls[0].chatID != "12345" {
		t.Errorf("sent to chatID %q, want %q", sender.calls[0].chatID, "12345")
	}
}

func TestSaveResult(t *testing.T) {
	dir := t.TempDir()
	result := PromptCronResult{
		Name:       "test",
		RanAt:      "2026-03-03T09:00:00Z",
		DurationMs: 1234,
		StopReason: "complete",
		Content:    "done",
	}

	if err := SaveResult(dir, result); err != nil {
		t.Fatalf("SaveResult: %v", err)
	}

	path := filepath.Join(ResultsDir(dir), "test.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	var loaded PromptCronResult
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parsing result: %v", err)
	}
	if loaded.Name != "test" {
		t.Errorf("name = %q, want %q", loaded.Name, "test")
	}
}
