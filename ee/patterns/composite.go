package patterns

import (
	"context"
	"sort"

	"github.com/kamilpajak/heisenberg/pkg/llm"
	pkgpatterns "github.com/kamilpajak/heisenberg/pkg/patterns"
)

const compositeMaxMatches = 5

// CompositeMatcher combines multiple PatternMatcher implementations and merges
// their results. Static patterns and dynamic patterns are both surfaced.
type CompositeMatcher struct {
	matchers []pkgpatterns.PatternMatcher
}

// NewCompositeMatcher creates a matcher that runs all provided matchers and
// merges their results, deduplicating by name and capping at 5 results.
func NewCompositeMatcher(matchers ...pkgpatterns.PatternMatcher) *CompositeMatcher {
	return &CompositeMatcher{matchers: matchers}
}

// Match runs all underlying matchers, merges results, deduplicates by name,
// sorts by similarity descending, and caps at compositeMaxMatches.
func (c *CompositeMatcher) Match(ctx context.Context, rca *llm.RootCauseAnalysis) []llm.MatchedPattern {
	var all []llm.MatchedPattern
	for _, m := range c.matchers {
		if m == nil {
			continue
		}
		results := m.Match(ctx, rca)
		all = append(all, results...)
	}

	deduped := dedupByName(all)

	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].Similarity > deduped[j].Similarity
	})

	if len(deduped) > compositeMaxMatches {
		deduped = deduped[:compositeMaxMatches]
	}

	return deduped
}

func dedupByName(matches []llm.MatchedPattern) []llm.MatchedPattern {
	seen := make(map[string]struct{}, len(matches))
	result := make([]llm.MatchedPattern, 0, len(matches))
	for _, m := range matches {
		if _, ok := seen[m.Name]; ok {
			continue
		}
		seen[m.Name] = struct{}{}
		result = append(result, m)
	}
	return result
}
