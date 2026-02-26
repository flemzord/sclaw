package openrouter

// defaultContextWindow is used when a model is not in the lookup table
// and no override is configured.
const defaultContextWindow = 128000

// contextWindows maps popular OpenRouter model identifiers to their
// maximum context window size in tokens.
var contextWindows = map[string]int{
	// OpenAI
	"openai/gpt-4o":      128000,
	"openai/gpt-4o-mini": 128000,
	"openai/gpt-4-turbo": 128000,
	"openai/gpt-4":       8192,
	"openai/o1":          200000,
	"openai/o1-mini":     128000,
	"openai/o3-mini":     200000,

	// Anthropic
	"anthropic/claude-3.5-sonnet": 200000,
	"anthropic/claude-3-opus":     200000,
	"anthropic/claude-3-haiku":    200000,
	"anthropic/claude-3.5-haiku":  200000,
	"anthropic/claude-sonnet-4":   200000,
	"anthropic/claude-opus-4":     200000,

	// Google
	"google/gemini-pro-1.5":   1048576,
	"google/gemini-flash-1.5": 1048576,
	"google/gemini-2.0-flash": 1048576,

	// Meta
	"meta-llama/llama-3.1-405b-instruct": 131072,
	"meta-llama/llama-3.1-70b-instruct":  131072,
	"meta-llama/llama-3.1-8b-instruct":   131072,

	// Mistral
	"mistralai/mistral-large": 128000,
	"mistralai/mixtral-8x7b":  32768,

	// OpenRouter
	"openrouter/auto": 128000,
}

// lookupContextWindow returns the context window size for the given model.
// If the model is not in the table, defaultContextWindow is returned.
func lookupContextWindow(model string) int {
	if size, ok := contextWindows[model]; ok {
		return size
	}
	return defaultContextWindow
}
