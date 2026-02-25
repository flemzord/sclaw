package memory

import (
	"context"
	"strings"

	ctxengine "github.com/flemzord/sclaw/internal/context"
)

// InjectMemory retrieves the top-K relevant facts from the store and formats
// them as a list of strings suitable for inclusion in the system prompt.
//
// Returns nil if the store is nil or no relevant facts are found.
// Token budget is enforced: facts are added until maxTokens is reached.
func InjectMemory(
	ctx context.Context,
	store Store,
	query string,
	maxFacts int,
	maxTokens int,
	estimator ctxengine.TokenEstimator,
) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	if maxFacts <= 0 || maxTokens <= 0 {
		return nil, nil
	}

	facts, err := store.Search(ctx, query, maxFacts)
	if err != nil {
		return nil, err
	}

	if len(facts) == 0 {
		return nil, nil
	}

	var result []string
	usedTokens := 0

	for i := range facts {
		tokens := estimator.Estimate(facts[i].Content)
		if usedTokens+tokens > maxTokens {
			break
		}
		result = append(result, facts[i].Content)
		usedTokens += tokens
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
