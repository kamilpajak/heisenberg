// Package patterns provides failure pattern recognition for CI test failures.
// Static patterns are bundled in the CLI binary; dynamic patterns (pgvector)
// are planned for the SaaS tier.
package patterns

import (
	"context"

	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// PatternMatcher finds known failure patterns that match an RCA.
type PatternMatcher interface {
	Match(ctx context.Context, rca *llm.RootCauseAnalysis) []llm.MatchedPattern
}
