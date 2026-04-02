package patterns

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/kamilpajak/heisenberg/pkg/llm"
)

const (
	matchThreshold = 0.6
	maxMatches     = 3

	// Scoring weights (sum to 1.0)
	weightFailureType = 0.5
	weightError       = 0.3
	weightFilePattern = 0.2
)

// StaticMatcher matches RCAs against a bundled pattern catalog.
type StaticMatcher struct {
	catalog []CatalogEntry
}

// NewStaticMatcher creates a matcher from the embedded catalog.
func NewStaticMatcher() (*StaticMatcher, error) {
	catalog, err := LoadCatalog()
	if err != nil {
		return nil, err
	}
	return &StaticMatcher{catalog: catalog}, nil
}

// Match finds catalog patterns that match the given RCA.
func (m *StaticMatcher) Match(_ context.Context, rca *llm.RootCauseAnalysis) []llm.MatchedPattern {
	fp := ComputeFingerprint(rca)

	type scored struct {
		entry CatalogEntry
		score float64
	}
	var candidates []scored

	for _, entry := range m.catalog {
		score := computeScore(fp, entry)
		if score >= matchThreshold {
			candidates = append(candidates, scored{entry, score})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	limit := maxMatches
	if len(candidates) < limit {
		limit = len(candidates)
	}

	matches := make([]llm.MatchedPattern, limit)
	for i := 0; i < limit; i++ {
		matches[i] = llm.MatchedPattern{
			Name:        candidates[i].entry.Name,
			Description: candidates[i].entry.Description,
			Similarity:  candidates[i].score,
			Frequency:   candidates[i].entry.Frequency,
		}
	}
	return matches
}

func computeScore(fp Fingerprint, entry CatalogEntry) float64 {
	var score float64

	// Failure type: exact match
	if fp.FailureType == entry.FailureType {
		score += weightFailureType
	}

	// Error tokens: Jaccard similarity
	if len(fp.ErrorTokens) > 0 && len(entry.ErrorTokens) > 0 {
		score += weightError * jaccard(fp.ErrorTokens, entry.ErrorTokens)
	}

	// File pattern: glob match
	if fp.FilePattern != "" && len(entry.FilePatterns) > 0 {
		for _, pattern := range entry.FilePatterns {
			if matched, _ := filepath.Match(pattern, fp.FilePattern); matched {
				score += weightFilePattern
				break
			}
		}
	} else if len(entry.FilePatterns) == 0 {
		// Pattern is framework-agnostic (no file constraint) — award partial credit
		score += weightFilePattern * 0.5
	}

	return score
}

func jaccard(a, b []string) float64 {
	setA := make(map[string]struct{}, len(a))
	for _, s := range a {
		setA[s] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, s := range b {
		setB[s] = struct{}{}
	}

	var intersection int
	for s := range setA {
		if _, ok := setB[s]; ok {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
