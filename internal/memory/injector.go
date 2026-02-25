package memory

import (
	"context"
	"strings"

	ctxengine "github.com/flemzord/sclaw/internal/context"
)

// InjectionRequest holds the parameters for InjectMemory.
type InjectionRequest struct {
	Store     Store
	Query     string
	MaxFacts  int
	MaxTokens int
	Estimator ctxengine.TokenEstimator
}

// InjectMemory retrieves the top-K relevant facts from the store and formats
// them as a list of strings suitable for inclusion in the system prompt.
//
// Returns nil if the store is nil or no relevant facts are found.
// Token budget is enforced: facts are added until maxTokens is reached.
func InjectMemory(ctx context.Context, req InjectionRequest) ([]string, error) {
	if req.Store == nil {
		return nil, nil
	}
	if req.MaxFacts <= 0 || req.MaxTokens <= 0 {
		return nil, nil
	}

	facts, err := req.Store.Search(ctx, req.Query, req.MaxFacts)
	if err != nil {
		return nil, err
	}

	if len(facts) == 0 {
		return nil, nil
	}

	var result []string

	for i := range facts {
		candidate := append(result, facts[i].Content)
		if req.Estimator.Estimate(FormatFacts(candidate)) > req.MaxTokens {
			break
		}
		result = candidate
	}

	return result, nil
}

// FormatFacts formats a list of fact strings into a single prompt section.
// Returns an empty string if no facts are provided.
func FormatFacts(facts []string) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Relevant Memory\n\n")
	for _, fact := range facts {
		b.WriteString("- ")
		b.WriteString(fact)
		b.WriteString("\n")
	}
	return b.String()
}
