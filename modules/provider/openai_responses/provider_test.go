package openairesponses

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flemzord/sclaw/internal/provider"
)

// mockWSServer creates a test WebSocket server that accepts connections and
// calls the handler function for each connection. It returns the server
// and a wss:// URL that can be used as ws_endpoint.
func mockWSServer(t *testing.T, handler func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			t.Logf("websocket accept error: %v", err)
			return
		}
		defer conn.CloseNow() //nolint:errcheck
		handler(conn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsURL converts an httptest server URL to a ws:// URL.
func wsURL(srv *httptest.Server) string {
	return strings.Replace(srv.URL, "http://", "ws://", 1) + "/v1/responses"
}

func testConfig(wsEndpoint string) Config {
	return Config{
		APIKey:        "test-key",
		Model:         "gpt-4o",
		ContextWindow: 128_000,
		MaxTokens:     4096,
		WSEndpoint:    wsEndpoint,
		DialTimeout:   5 * time.Second,
		ConnMaxAge:    55 * time.Minute,
	}
}

func TestCompleteSimpleText(t *testing.T) {
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		// Read the response.create event.
		_, data, err := conn.Read(context.Background())
		if err != nil {
			t.Errorf("read: %v", err)
			return
		}

		var event clientEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}

		if event.Type != "response.create" {
			t.Errorf("expected response.create, got %s", event.Type)
		}

		// Send text deltas.
		writeJSON(t, conn, serverEvent{
			Type:  "response.output_text.delta",
			Delta: "Hello, ",
		})
		writeJSON(t, conn, serverEvent{
			Type:  "response.output_text.delta",
			Delta: "world!",
		})

		// Send completed.
		writeJSON(t, conn, serverEvent{
			Type: "response.completed",
			Response: &responseCompleted{
				ID:     "resp_001",
				Status: "completed",
				Output: []outputItem{
					{
						Type: "message",
						Role: "assistant",
						Content: []outputContentPart{
							{Type: "output_text", Text: "Hello, world!"},
						},
					},
				},
				Usage: &wireUsage{
					InputTokens:  10,
					OutputTokens: 5,
					TotalTokens:  15,
				},
			},
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg}
	p.conn = newConnManager(cfg, testLogger())

	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if resp.Content != "Hello, world!" {
		t.Errorf("content = %q, want %q", resp.Content, "Hello, world!")
	}
	if resp.FinishReason != provider.FinishReasonStop {
		t.Errorf("finish_reason = %q, want %q", resp.FinishReason, provider.FinishReasonStop)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want %d", resp.Usage.TotalTokens, 15)
	}
}

func TestCompleteWithToolCalls(t *testing.T) {
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		// Read the response.create event.
		_, _, err := conn.Read(context.Background())
		if err != nil {
			t.Errorf("read: %v", err)
			return
		}

		// Send function call argument deltas.
		writeJSON(t, conn, serverEvent{
			Type:        "response.function_call_arguments.delta",
			OutputIndex: 0,
			Delta:       `{"query":`,
		})
		writeJSON(t, conn, serverEvent{
			Type:        "response.function_call_arguments.delta",
			OutputIndex: 0,
			Delta:       ` "weather"}`,
		})

		// Send completed output item.
		writeJSON(t, conn, serverEvent{
			Type:        "response.output_item.done",
			OutputIndex: 0,
			Item: &outputItem{
				Type:      "function_call",
				CallID:    "call_001",
				Name:      "get_weather",
				Arguments: `{"query": "weather"}`,
			},
		})

		// Send completed response.
		writeJSON(t, conn, serverEvent{
			Type: "response.completed",
			Response: &responseCompleted{
				ID:     "resp_002",
				Status: "completed",
				Output: []outputItem{
					{
						Type:      "function_call",
						CallID:    "call_001",
						Name:      "get_weather",
						Arguments: `{"query": "weather"}`,
					},
				},
				Usage: &wireUsage{
					InputTokens:  20,
					OutputTokens: 10,
					TotalTokens:  30,
				},
			},
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg}
	p.conn = newConnManager(cfg, testLogger())

	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "What's the weather?"},
		},
		Tools: []provider.ToolDefinition{
			{Name: "get_weather", Description: "Get weather info"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("tool call name = %q, want %q", resp.ToolCalls[0].Name, "get_weather")
	}
	if resp.ToolCalls[0].ID != "call_001" {
		t.Errorf("tool call id = %q, want %q", resp.ToolCalls[0].ID, "call_001")
	}
	if resp.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("finish_reason = %q, want %q", resp.FinishReason, provider.FinishReasonToolUse)
	}
}

func TestStreamText(t *testing.T) {
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		_, _, err := conn.Read(context.Background())
		if err != nil {
			t.Errorf("read: %v", err)
			return
		}

		writeJSON(t, conn, serverEvent{
			Type:  "response.output_text.delta",
			Delta: "chunk1",
		})
		writeJSON(t, conn, serverEvent{
			Type:  "response.output_text.delta",
			Delta: "chunk2",
		})
		writeJSON(t, conn, serverEvent{
			Type: "response.completed",
			Response: &responseCompleted{
				ID:     "resp_003",
				Status: "completed",
				Output: []outputItem{
					{
						Type: "message",
						Role: "assistant",
						Content: []outputContentPart{
							{Type: "output_text", Text: "chunk1chunk2"},
						},
					},
				},
				Usage: &wireUsage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
			},
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg}
	p.conn = newConnManager(cfg, testLogger())

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Accumulate streamed text content.
	var content string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		content += chunk.Content
	}
	if content != "chunk1chunk2" {
		t.Errorf("streamed content = %q, want %q", content, "chunk1chunk2")
	}
}

func TestStreamToolCalls(t *testing.T) {
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		_, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}

		writeJSON(t, conn, serverEvent{
			Type:        "response.function_call_arguments.delta",
			OutputIndex: 0,
			Delta:       `{"x":1}`,
		})
		writeJSON(t, conn, serverEvent{
			Type:        "response.output_item.done",
			OutputIndex: 0,
			Item: &outputItem{
				Type:      "function_call",
				CallID:    "call_100",
				Name:      "do_thing",
				Arguments: `{"x":1}`,
			},
		})
		writeJSON(t, conn, serverEvent{
			Type: "response.completed",
			Response: &responseCompleted{
				ID:     "resp_004",
				Status: "completed",
				Output: []outputItem{
					{Type: "function_call", CallID: "call_100", Name: "do_thing", Arguments: `{"x":1}`},
				},
				Usage: &wireUsage{InputTokens: 5, OutputTokens: 5, TotalTokens: 10},
			},
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg}
	p.conn = newConnManager(cfg, testLogger())

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "do it"},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var lastToolCall provider.ToolCall
	var toolCount int
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		for _, tc := range chunk.ToolCalls {
			lastToolCall = tc
			toolCount++
		}
	}

	if toolCount != 1 {
		t.Fatalf("tool_calls = %d, want 1", toolCount)
	}
	if lastToolCall.Name != "do_thing" {
		t.Errorf("tool name = %q, want %q", lastToolCall.Name, "do_thing")
	}
}

func TestServerError(t *testing.T) {
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		_, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}

		writeJSON(t, conn, serverEvent{
			Type: "error",
			Error: &serverError{
				Code:    "rate_limit_exceeded",
				Message: "too many requests",
			},
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg}
	p.conn = newConnManager(cfg, testLogger())

	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %v, want rate limit error", err)
	}
}

func TestContextCancellation(t *testing.T) {
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		_, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}

		// Delay response to give time for cancellation.
		time.Sleep(200 * time.Millisecond)
		writeJSON(t, conn, serverEvent{
			Type:  "response.output_text.delta",
			Delta: "too late",
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg}
	p.conn = newConnManager(cfg, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Complete(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}

func TestReconnectionAfterInvalidate(t *testing.T) {
	connectCount := 0
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		connectCount++

		_, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}

		if connectCount == 1 {
			// First connection: send an error to trigger invalidation.
			writeJSON(t, conn, serverEvent{
				Type: "error",
				Error: &serverError{
					Code:    "server_error",
					Message: "temporary failure",
				},
			})
			return
		}

		// Second connection: respond normally.
		writeJSON(t, conn, serverEvent{
			Type:  "response.output_text.delta",
			Delta: "ok",
		})
		writeJSON(t, conn, serverEvent{
			Type: "response.completed",
			Response: &responseCompleted{
				ID:     "resp_005",
				Status: "completed",
				Output: []outputItem{
					{
						Type: "message", Role: "assistant",
						Content: []outputContentPart{{Type: "output_text", Text: "ok"}},
					},
				},
				Usage: &wireUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
			},
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg}
	p.conn = newConnManager(cfg, testLogger())

	// First call: should fail.
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected first call to fail")
	}

	// Second call: should succeed with a new connection.
	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want %q", resp.Content, "ok")
	}
	if connectCount < 2 {
		t.Errorf("connectCount = %d, want >= 2", connectCount)
	}
}

func TestFreshConnectionPerRequest(t *testing.T) {
	connectCount := 0
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		connectCount++
		_, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		writeJSON(t, conn, serverEvent{
			Type:  "response.output_text.delta",
			Delta: "ok",
		})
		writeJSON(t, conn, serverEvent{
			Type: "response.completed",
			Response: &responseCompleted{
				ID:     "resp_fresh",
				Status: "completed",
				Output: []outputItem{
					{
						Type: "message", Role: "assistant",
						Content: []outputContentPart{{Type: "output_text", Text: "ok"}},
					},
				},
				Usage: &wireUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
			},
		})
	})

	cfg := testConfig(wsURL(srv))
	p := &Provider{config: cfg, logger: testLogger()}
	p.conn = newConnManager(cfg, testLogger())

	// Two consecutive requests should each get a fresh connection.
	for i := 0; i < 2; i++ {
		resp, err := p.Complete(context.Background(), provider.CompletionRequest{
			Messages: []provider.LLMMessage{
				{Role: provider.MessageRoleUser, Content: "Hi"},
			},
		})
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		if resp.Content != "ok" {
			t.Errorf("request %d: content = %q, want %q", i+1, resp.Content, "ok")
		}
	}

	if connectCount < 2 {
		t.Errorf("connectCount = %d, want >= 2 (fresh conn per request)", connectCount)
	}
}

func TestConvertMessages(t *testing.T) {
	cfg := Config{Model: "gpt-4o", MaxTokens: 1024}
	req := provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleSystem, Content: "You are helpful."},
			{Role: provider.MessageRoleUser, Content: "Hello"},
			{Role: provider.MessageRoleAssistant, Content: "Hi there!"},
			{
				Role: provider.MessageRoleAssistant,
				ToolCalls: []provider.ToolCall{
					{ID: "tc_1", Name: "search", Arguments: json.RawMessage(`{"q":"Go"}`)},
				},
			},
			{Role: provider.MessageRoleTool, ToolID: "tc_1", Content: "results..."},
			{Role: provider.MessageRoleUser, Content: "Thanks"},
		},
		Tools: []provider.ToolDefinition{
			{Name: "search", Description: "Search the web"},
		},
	}

	event := buildClientEvent(cfg, req)

	if event.Type != "response.create" {
		t.Fatalf("type = %q, want response.create", event.Type)
	}
	if event.Instructions != "You are helpful." {
		t.Errorf("instructions = %q, want %q", event.Instructions, "You are helpful.")
	}
	if event.Model != "gpt-4o" {
		t.Errorf("model = %q, want %q", event.Model, "gpt-4o")
	}

	// System messages are extracted, so Input should have:
	// user "Hello", assistant "Hi there!", function_call, function_call_output, user "Thanks"
	inputs := event.Input
	if len(inputs) != 5 {
		t.Fatalf("input count = %d, want 5", len(inputs))
	}

	// First input: user message.
	if inputs[0].Type != "message" || inputs[0].Role != "user" {
		t.Errorf("input[0] = %s/%s, want message/user", inputs[0].Type, inputs[0].Role)
	}

	// Second input: assistant message.
	if inputs[1].Type != "message" || inputs[1].Role != "assistant" {
		t.Errorf("input[1] = %s/%s, want message/assistant", inputs[1].Type, inputs[1].Role)
	}

	// Third input: function_call.
	if inputs[2].Type != "function_call" || inputs[2].Name != "search" {
		t.Errorf("input[2] = %s/%s, want function_call/search", inputs[2].Type, inputs[2].Name)
	}

	// Fourth input: function_call_output.
	if inputs[3].Type != "function_call_output" || inputs[3].CallID != "tc_1" {
		t.Errorf("input[3] = %s/%s, want function_call_output/tc_1", inputs[3].Type, inputs[3].CallID)
	}

	// Fifth input: user message.
	if inputs[4].Type != "message" || inputs[4].Role != "user" {
		t.Errorf("input[4] = %s/%s, want message/user", inputs[4].Type, inputs[4].Role)
	}

	// Tools.
	if len(event.Tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(event.Tools))
	}
	if event.Tools[0].Name != "search" {
		t.Errorf("tool name = %q, want %q", event.Tools[0].Name, "search")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	cfg.defaults()

	if cfg.WSEndpoint != "wss://api.openai.com/v1/responses" {
		t.Errorf("ws_endpoint = %q", cfg.WSEndpoint)
	}
	if cfg.ContextWindow != 128_000 {
		t.Errorf("context_window = %d", cfg.ContextWindow)
	}
	if cfg.DialTimeout != 10*time.Second {
		t.Errorf("dial_timeout = %v", cfg.DialTimeout)
	}
	if cfg.ConnMaxAge != 55*time.Minute {
		t.Errorf("conn_max_age = %v", cfg.ConnMaxAge)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "missing api key",
			cfg:     Config{Model: "gpt-4o", WSEndpoint: "wss://api.openai.com/v1/responses"},
			wantErr: "one of api_key or api_key_env is required",
		},
		{
			name:    "missing model",
			cfg:     Config{APIKey: "key", WSEndpoint: "wss://api.openai.com/v1/responses"},
			wantErr: "model is required",
		},
		{
			name:    "bad ws scheme",
			cfg:     Config{APIKey: "key", Model: "gpt-4o", WSEndpoint: "http://example.com"},
			wantErr: "ws or wss",
		},
		{
			name:    "negative context window",
			cfg:     Config{APIKey: "key", Model: "gpt-4o", WSEndpoint: "wss://x.com", ContextWindow: -1},
			wantErr: "must not be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestWsToHTTP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"wss://api.openai.com/v1/responses", "https://api.openai.com/v1"},
		{"ws://localhost:8080/v1/responses", "http://localhost:8080/v1"},
		{"wss://custom.api.com/v2/responses", "https://custom.api.com/v2"},
	}
	for _, tt := range tests {
		got := wsToHTTP(tt.input)
		if got != tt.want {
			t.Errorf("wsToHTTP(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestModelName(t *testing.T) {
	p := &Provider{config: Config{Model: "gpt-4o"}}
	if p.ModelName() != "gpt-4o" {
		t.Errorf("ModelName = %q", p.ModelName())
	}
}

func TestContextWindowSize(t *testing.T) {
	p := &Provider{config: Config{ContextWindow: 200_000}}
	if p.ContextWindowSize() != 200_000 {
		t.Errorf("ContextWindowSize = %d", p.ContextWindowSize())
	}
}

// writeJSON is a test helper that sends a JSON-encoded server event.
func writeJSON(t *testing.T, conn *websocket.Conn, event serverEvent) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal server event: %v", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Logf("write server event: %v", err)
	}
}

// testLogger returns a no-op logger for tests.
func testLogger() *slog.Logger {
	return slog.Default()
}
