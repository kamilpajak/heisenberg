package patterns

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"

	"github.com/kamilpajak/heisenberg/ee/database"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

const (
	dynamicMaxMatches     = 3
	dynamicThreshold      = 0.7
	embeddingTextMaxChars = 100
)

// DynamicMatcher finds similar historical RCAs using vector similarity search.
// It implements patterns.PatternMatcher from pkg/patterns.
type DynamicMatcher struct {
	db              *database.DB
	embeddingClient *EmbeddingClient
	orgID           uuid.UUID
}

// NewDynamicMatcher creates a dynamic pattern matcher for the given organization.
func NewDynamicMatcher(db *database.DB, ec *EmbeddingClient, orgID uuid.UUID) *DynamicMatcher {
	return &DynamicMatcher{
		db:              db,
		embeddingClient: ec,
		orgID:           orgID,
	}
}

// Match finds historical RCAs similar to the given RCA using vector similarity.
// Returns nil (degrades gracefully) if embedding or DB query fails.
func (m *DynamicMatcher) Match(ctx context.Context, rca *llm.RootCauseAnalysis) []llm.MatchedPattern {
	text := ComputeEmbeddingText(rca)

	embedding, err := m.embeddingClient.Embed(ctx, text)
	if err != nil {
		log.Printf("dynamic matcher embedding failed: %v", err)
		return nil
	}

	similar, err := m.db.FindSimilarRCAs(ctx, m.orgID, pgvector.NewVector(embedding),
		uuid.Nil, dynamicMaxMatches, dynamicThreshold)
	if err != nil {
		log.Printf("dynamic matcher DB query failed: %v", err)
		return nil
	}

	matches := make([]llm.MatchedPattern, 0, len(similar))
	for _, s := range similar {
		matches = append(matches, llm.MatchedPattern{
			Name:        fmt.Sprintf("historical-%s", s.AnalysisID.String()[:8]),
			Description: truncateText(s.EmbeddingText, embeddingTextMaxChars),
			Similarity:  s.Similarity,
			Frequency:   fmt.Sprintf("Seen in %s run %d", s.RepoFullName, s.RunID),
		})
	}
	return matches
}

func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
