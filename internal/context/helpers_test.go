package ctxengine_test

import (
	"context"
	"fmt"

	"github.com/flemzord/sclaw/internal/provider"
)

// mockSummarizer implements ctxengine.Summarizer for tests.
type mockSummarizer struct {
	result string
	err    error
	called int
}

func (m *mockSummarizer) Summarize(_ context.Context, _ []provider.LLMMessage) (string, error) {
	m.called++
	return m.result, m.err
}

// mockEstimator implements ctxengine.TokenEstimator for tests.
type mockEstimator struct{}

func (m *mockEstimator) Estimate(text string) int { return len(text) }

// makeTestMessages creates n alternating user/assistant messages.
func makeTestMessages(n int) []provider.LLMMessage {
	msgs := make([]provider.LLMMessage, n)
	for i := range msgs {
		role := provider.MessageRoleUser
		if i%2 == 1 {
			role = provider.MessageRoleAssistant
		}
		msgs[i] = provider.LLMMessage{Role: role, Content: fmt.Sprintf("msg-%d", i)}
	}
	return msgs
}
