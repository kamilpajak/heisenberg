package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sig(category, normalized string) ErrorSignature {
	return ErrorSignature{
		Category:   category,
		Normalized: normalized,
		RawExcerpt: normalized,
		Tokens:     tokenize(normalized),
	}
}

func fail(id int64, name string, s ErrorSignature) FailureInfo {
	return FailureInfo{
		JobID:      id,
		JobName:    name,
		Conclusion: "failure",
		Signature:  s,
		LogTail:    "log for " + name,
	}
}

func TestClusterFailures_ExactMatch(t *testing.T) {
	failures := []FailureInfo{
		fail(1, "E2E 1/4", sig("error_message", "connection refused")),
		fail(2, "E2E 2/4", sig("error_message", "connection refused")),
		fail(3, "E2E 3/4", sig("error_message", "connection refused")),
		fail(4, "E2E 4/4", sig("error_message", "connection refused")),
	}

	result := ClusterFailures(failures)

	assert.Equal(t, 4, result.TotalFailed)
	require.Len(t, result.Clusters, 1)
	assert.Len(t, result.Clusters[0].Failures, 4)
	assert.Equal(t, "exact", result.Method)
	assert.Empty(t, result.Unclustered)
}

func TestClusterFailures_DistinctErrors(t *testing.T) {
	failures := []FailureInfo{
		fail(1, "E2E 1/4", sig("error_message", "connection refused")),
		fail(2, "E2E 2/4", sig("error_message", "connection refused")),
		fail(3, "Lint", sig("exit_code", "exit code 1")),
		fail(4, "Type Check", sig("error_message", "type error in module foo")),
	}

	result := ClusterFailures(failures)

	assert.Equal(t, 4, result.TotalFailed)
	require.Len(t, result.Clusters, 3)

	// Largest cluster first
	assert.Len(t, result.Clusters[0].Failures, 2)
	assert.Contains(t, result.Clusters[0].Signature.Normalized, "connection refused")
}

func TestClusterFailures_JaccardMerge(t *testing.T) {
	// Two nearly identical errors — only one word differs out of many
	// Jaccard: 8/9 ≈ 0.89 > 0.8 threshold
	failures := []FailureInfo{
		fail(1, "Test A", sig("error_message", "expected submit button to be visible on checkout page after login complete")),
		fail(2, "Test B", sig("error_message", "expected submit button to be visible on payment page after login complete")),
	}

	result := ClusterFailures(failures)

	require.Len(t, result.Clusters, 1, "similar errors should be merged by Jaccard")
	assert.Len(t, result.Clusters[0].Failures, 2)
}

func TestClusterFailures_JaccardNoMerge(t *testing.T) {
	// Two very different errors that should NOT merge
	failures := []FailureInfo{
		fail(1, "Test A", sig("error_message", "connection refused at localhost port database")),
		fail(2, "Test B", sig("error_message", "assertion failed expected value to equal target string")),
	}

	result := ClusterFailures(failures)

	require.Len(t, result.Clusters, 2, "distinct errors should not merge")
}

func TestClusterFailures_Empty(t *testing.T) {
	result := ClusterFailures(nil)
	assert.Equal(t, 0, result.TotalFailed)
	assert.Empty(t, result.Clusters)
	assert.Equal(t, "single", result.Method)
}

func TestClusterFailures_SingleFailure(t *testing.T) {
	failures := []FailureInfo{
		fail(1, "Test", sig("error_message", "something broke")),
	}

	result := ClusterFailures(failures)

	require.Len(t, result.Clusters, 1)
	assert.Len(t, result.Clusters[0].Failures, 1)
	assert.Equal(t, "single", result.Method)
}

func TestClusterFailures_NoSignature_GoToUnclustered(t *testing.T) {
	failures := []FailureInfo{
		fail(1, "Test A", sig("error_message", "connection refused")),
		fail(2, "Test B", ErrorSignature{}), // no signature
	}

	result := ClusterFailures(failures)

	require.Len(t, result.Clusters, 1)
	assert.Len(t, result.Unclustered, 1)
	assert.Equal(t, "Test B", result.Unclustered[0].JobName)
}

func TestClusterFailures_Cap10(t *testing.T) {
	// Create 15 distinct failures
	var failures []FailureInfo
	for i := range 15 {
		failures = append(failures, fail(int64(i), "Job", sig("error_message", repeatWord(i))))
	}

	result := ClusterFailures(failures)

	// Should cap at 10 clusters, rest go to unclustered
	assert.LessOrEqual(t, len(result.Clusters), 10)
	totalAccountedFor := len(result.Unclustered)
	for _, c := range result.Clusters {
		totalAccountedFor += len(c.Failures)
	}
	assert.Equal(t, 15, totalAccountedFor, "all failures must be accounted for")
}

func TestClusterFailures_RepresentativeIsLongestLog(t *testing.T) {
	s := sig("error_message", "connection refused")
	failures := []FailureInfo{
		{JobID: 1, JobName: "Short", Signature: s, LogTail: "short"},
		{JobID: 2, JobName: "Long", Signature: s, LogTail: "this is a much longer log with more detail"},
		{JobID: 3, JobName: "Medium", Signature: s, LogTail: "medium length log"},
	}

	result := ClusterFailures(failures)

	require.Len(t, result.Clusters, 1)
	assert.Equal(t, "Long", result.Clusters[0].Representative.JobName)
}

func TestJaccard(t *testing.T) {
	a := []string{"the", "quick", "brown", "fox"}
	b := []string{"the", "quick", "red", "fox"}
	// Intersection: the, quick, fox (3) / Union: the, quick, brown, red, fox (5)
	assert.InDelta(t, 0.6, jaccard(a, b), 0.01)
}

func TestJaccard_Identical(t *testing.T) {
	a := []string{"error", "connection", "refused"}
	assert.Equal(t, 1.0, jaccard(a, a))
}

func TestJaccard_Disjoint(t *testing.T) {
	a := []string{"alpha", "beta"}
	b := []string{"gamma", "delta"}
	assert.Equal(t, 0.0, jaccard(a, b))
}

func TestJaccard_Empty(t *testing.T) {
	assert.Equal(t, 0.0, jaccard(nil, nil))
}

// repeatWord returns a unique word for index i.
func repeatWord(i int) string {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta", "iota", "kappa", "lambda", "mu", "nu", "xi", "omicron"}
	return words[i%len(words)] + " unique error message number"
}
