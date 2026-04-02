package patterns

import (
	"context"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCatalog(t *testing.T) {
	catalog, err := LoadCatalog()
	require.NoError(t, err)
	assert.True(t, len(catalog) >= 10, "catalog should have at least 10 patterns")

	// Verify first entry has all required fields
	first := catalog[0]
	assert.NotEmpty(t, first.Name)
	assert.NotEmpty(t, first.Description)
	assert.NotEmpty(t, first.FailureType)
	assert.NotEmpty(t, first.ErrorTokens)
	assert.NotEmpty(t, first.Frequency)
}

func TestNewStaticMatcher(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)
	assert.NotNil(t, matcher)
}

func TestStaticMatcher_PlaywrightTimeout(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)

	rca := &llm.RootCauseAnalysis{
		FailureType: "timeout",
		Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts"},
		RootCause:   "waitForSelector timed out in beforeEach hook — CSS selector no longer matches",
	}

	matches := matcher.Match(context.Background(), rca)

	require.NotEmpty(t, matches, "should match Playwright timeout pattern")
	assert.Equal(t, "playwright-beforeeach-timeout", matches[0].Name)
	assert.GreaterOrEqual(t, matches[0].Similarity, 0.6)
}

func TestStaticMatcher_DatabaseConnectionRefused(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)

	rca := &llm.RootCauseAnalysis{
		FailureType: "network",
		Location:    &llm.CodeLocation{FilePath: "tests/db_test.go"},
		RootCause:   "connection refused to postgres on port 5432 — database service not running",
	}

	matches := matcher.Match(context.Background(), rca)

	require.NotEmpty(t, matches)
	assert.Equal(t, "database-connection-refused", matches[0].Name)
}

func TestStaticMatcher_NoMatch(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)

	rca := &llm.RootCauseAnalysis{
		FailureType: "assertion",
		Location:    &llm.CodeLocation{FilePath: "tests/math_test.go"},
		RootCause:   "calculated value 42 does not equal 43 — off by one in fibonacci",
	}

	matches := matcher.Match(context.Background(), rca)

	// Generic math assertion — shouldn't match specific patterns strongly
	for _, m := range matches {
		assert.Less(t, m.Similarity, 0.9, "generic assertion should not be a strong match to any specific pattern")
	}
}

func TestStaticMatcher_MaxThreeResults(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)

	// Broad failure type that could match many patterns
	rca := &llm.RootCauseAnalysis{
		FailureType: "timeout",
		Location:    &llm.CodeLocation{FilePath: "tests/app.spec.ts"},
		RootCause:   "timeout exceeded waiting for navigation page load selector beforeEach",
	}

	matches := matcher.Match(context.Background(), rca)
	assert.LessOrEqual(t, len(matches), 3, "should return at most 3 matches")
}

func TestStaticMatcher_SortedByScore(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)

	rca := &llm.RootCauseAnalysis{
		FailureType: "timeout",
		Location:    &llm.CodeLocation{FilePath: "tests/app.spec.ts"},
		RootCause:   "timeout exceeded waiting for navigation page load",
	}

	matches := matcher.Match(context.Background(), rca)
	if len(matches) >= 2 {
		assert.GreaterOrEqual(t, matches[0].Similarity, matches[1].Similarity,
			"matches should be sorted by similarity descending")
	}
}

func TestStaticMatcher_GoRaceDetector(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)

	rca := &llm.RootCauseAnalysis{
		FailureType: "flake",
		Location:    &llm.CodeLocation{FilePath: "pkg/cache/cache_test.go"},
		RootCause:   "DATA RACE: previous write by goroutine 42, concurrent read by goroutine 18",
	}

	matches := matcher.Match(context.Background(), rca)

	require.NotEmpty(t, matches)
	assert.Equal(t, "go-test-race-detector", matches[0].Name)
}

func TestStaticMatcher_NoFalsePositiveFromStructuralOnly(t *testing.T) {
	matcher, err := NewStaticMatcher()
	require.NoError(t, err)

	// Assertion in *.ts — matches failure_type + file_pattern for many patterns
	// but root cause has zero token overlap with auth-token or visual-regression.
	rca := &llm.RootCauseAnalysis{
		FailureType: "assertion",
		Location:    &llm.CodeLocation{FilePath: "tests/perf.spec.ts"},
		RootCause:   "module loading behavior changed after dependency update, utilsBundle not found in output",
	}

	matches := matcher.Match(context.Background(), rca)

	// Should NOT match auth-token-expired or screenshot-visual-regression
	for _, m := range matches {
		assert.NotEqual(t, "auth-token-expired", m.Name,
			"auth-token should not match — zero error token overlap")
		assert.NotEqual(t, "screenshot-visual-regression", m.Name,
			"visual-regression should not match — zero error token overlap")
	}
}

func TestJaccard(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"a", "b", "c"}, []string{"a", "b", "c"}, 1.0},
		{"disjoint", []string{"a", "b"}, []string{"c", "d"}, 0.0},
		{"partial", []string{"a", "b", "c"}, []string{"b", "c", "d"}, 0.5},
		{"empty a", nil, []string{"a"}, 0.0},
		{"both empty", nil, nil, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.want, jaccard(tt.a, tt.b), 0.01)
		})
	}
}
