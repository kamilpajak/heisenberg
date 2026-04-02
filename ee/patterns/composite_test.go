package patterns

import (
	"context"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
)

// mockMatcher returns fixed results for testing.
type mockMatcher struct {
	results []llm.MatchedPattern
}

func (m *mockMatcher) Match(_ context.Context, _ *llm.RootCauseAnalysis) []llm.MatchedPattern {
	return m.results
}

func TestCompositeMatcher_MergesResults(t *testing.T) {
	static := &mockMatcher{results: []llm.MatchedPattern{
		{Name: "playwright-timeout", Similarity: 0.8, Description: "Static pattern"},
	}}
	dynamic := &mockMatcher{results: []llm.MatchedPattern{
		{Name: "historical-abc12345", Similarity: 0.9, Description: "Dynamic pattern"},
	}}

	composite := NewCompositeMatcher(static, dynamic)
	results := composite.Match(context.Background(), &llm.RootCauseAnalysis{})

	assert.Len(t, results, 2)
	// Should be sorted by similarity descending
	assert.Equal(t, "historical-abc12345", results[0].Name)
	assert.Equal(t, "playwright-timeout", results[1].Name)
}

func TestCompositeMatcher_DeduplicatesByName(t *testing.T) {
	m1 := &mockMatcher{results: []llm.MatchedPattern{
		{Name: "same-pattern", Similarity: 0.8},
	}}
	m2 := &mockMatcher{results: []llm.MatchedPattern{
		{Name: "same-pattern", Similarity: 0.9},
	}}

	composite := NewCompositeMatcher(m1, m2)
	results := composite.Match(context.Background(), &llm.RootCauseAnalysis{})

	assert.Len(t, results, 1, "duplicates should be removed")
	assert.Equal(t, "same-pattern", results[0].Name)
	assert.Equal(t, 0.8, results[0].Similarity, "first occurrence wins")
}

func TestCompositeMatcher_CapsAt5(t *testing.T) {
	many := make([]llm.MatchedPattern, 10)
	for i := range many {
		many[i] = llm.MatchedPattern{
			Name:       "pattern-" + string(rune('A'+i)),
			Similarity: float64(10-i) / 10.0,
		}
	}

	matcher := &mockMatcher{results: many}
	composite := NewCompositeMatcher(matcher)
	results := composite.Match(context.Background(), &llm.RootCauseAnalysis{})

	assert.Len(t, results, 5, "should cap at 5")
	assert.Equal(t, 1.0, results[0].Similarity, "highest similarity first")
}

func TestCompositeMatcher_NilMatcher(t *testing.T) {
	static := &mockMatcher{results: []llm.MatchedPattern{
		{Name: "test", Similarity: 0.8},
	}}

	composite := NewCompositeMatcher(static, nil)
	results := composite.Match(context.Background(), &llm.RootCauseAnalysis{})

	assert.Len(t, results, 1, "should skip nil matchers")
}

func TestCompositeMatcher_Empty(t *testing.T) {
	composite := NewCompositeMatcher()
	results := composite.Match(context.Background(), &llm.RootCauseAnalysis{})
	assert.Empty(t, results)
}
