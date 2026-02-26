package anthropic

import (
	"context"

	"github.com/flemzord/sclaw/internal/provider"
)

// Complete sends a synchronous completion request to the Anthropic Messages API.
func (a *Anthropic) Complete(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	params := convertRequest(req, &a.config, a.logger)

	msg, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return provider.CompletionResponse{}, mapError(err)
	}

	return convertResponse(msg), nil
}
