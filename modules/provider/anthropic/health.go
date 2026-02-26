package anthropic

import (
	"context"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
)

// HealthCheck validates connectivity and authentication by sending a minimal
// completion request. The Anthropic API has no dedicated health endpoint,
// so a 1-token completion is the cheapest probe available.
func (a *Anthropic) HealthCheck(ctx context.Context) error {
	_, err := a.client.Messages.New(ctx, sdkanthropic.MessageNewParams{
		Model:     sdkanthropic.Model(a.config.Model),
		MaxTokens: 1,
		Messages: []sdkanthropic.MessageParam{
			sdkanthropic.NewUserMessage(sdkanthropic.NewTextBlock("hi")),
		},
	})
	return mapError(err)
}
