package analysis

import (
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/cluster"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeClusterResults_TwoClusters(t *testing.T) {
	results := []clusterAnalysis{
		{
			Cluster: cluster.Cluster{ID: 1, Failures: make([]cluster.FailureInfo, 4)},
			Result: &llm.AnalysisResult{
				Text:        "Cluster 1 analysis",
				Category:    llm.CategoryDiagnosis,
				Confidence:  90,
				Sensitivity: "low",
				RCAs: []llm.RootCauseAnalysis{
					{Title: "Connection refused", FailureType: llm.FailureTypeInfra, RootCause: "DB not started"},
				},
			},
		},
		{
			Cluster: cluster.Cluster{ID: 2, Failures: make([]cluster.FailureInfo, 1)},
			Result: &llm.AnalysisResult{
				Text:        "Cluster 2 analysis",
				Category:    llm.CategoryDiagnosis,
				Confidence:  70,
				Sensitivity: "high",
				RCAs: []llm.RootCauseAnalysis{
					{Title: "Assertion failed", FailureType: llm.FailureTypeAssertion, RootCause: "Outdated snapshot"},
				},
			},
		},
	}

	merged := mergeClusterResults(results)

	assert.Equal(t, llm.CategoryDiagnosis, merged.Category)
	require.Len(t, merged.RCAs, 2)
	assert.Equal(t, "Connection refused", merged.RCAs[0].Title)
	assert.Equal(t, "Assertion failed", merged.RCAs[1].Title)
	// Weighted confidence: (90*4 + 70*1) / 5 = 86
	assert.Equal(t, 86, merged.Confidence)
	// Sensitivity: take highest (worst)
	assert.Equal(t, "high", merged.Sensitivity)
	assert.Contains(t, merged.Text, "Cluster 1")
	assert.Contains(t, merged.Text, "Cluster 2")
}

func TestMergeClusterResults_DeduplicateIdenticalRootCauses(t *testing.T) {
	results := []clusterAnalysis{
		{
			Cluster: cluster.Cluster{ID: 1, Failures: make([]cluster.FailureInfo, 3)},
			Result: &llm.AnalysisResult{
				Category:   llm.CategoryDiagnosis,
				Confidence: 85,
				RCAs: []llm.RootCauseAnalysis{
					{Title: "DB connection refused", RootCause: "database not running"},
				},
			},
		},
		{
			Cluster: cluster.Cluster{ID: 2, Failures: make([]cluster.FailureInfo, 2)},
			Result: &llm.AnalysisResult{
				Category:   llm.CategoryDiagnosis,
				Confidence: 80,
				RCAs: []llm.RootCauseAnalysis{
					{Title: "DB connection refused", RootCause: "database not running"},
				},
			},
		},
	}

	merged := mergeClusterResults(results)

	// Same root cause → deduplicated to 1 RCA
	require.Len(t, merged.RCAs, 1)
	assert.Equal(t, "DB connection refused", merged.RCAs[0].Title)
}

func TestMergeClusterResults_SingleCluster(t *testing.T) {
	results := []clusterAnalysis{
		{
			Cluster: cluster.Cluster{ID: 1, Failures: make([]cluster.FailureInfo, 2)},
			Result: &llm.AnalysisResult{
				Text:        "Single cluster analysis",
				Category:    llm.CategoryDiagnosis,
				Confidence:  95,
				Sensitivity: "low",
				RCAs: []llm.RootCauseAnalysis{
					{Title: "Timeout", FailureType: llm.FailureTypeTimeout},
				},
			},
		},
	}

	merged := mergeClusterResults(results)

	require.Len(t, merged.RCAs, 1)
	assert.Equal(t, 95, merged.Confidence)
	assert.Equal(t, "Single cluster analysis", merged.Text)
}

func TestMergeClusterResults_Empty(t *testing.T) {
	merged := mergeClusterResults(nil)
	assert.Empty(t, merged.RCAs)
}

func TestMergeCategory_NoFailures(t *testing.T) {
	results := []clusterAnalysis{
		{Result: &llm.AnalysisResult{Category: llm.CategoryNoFailures}},
	}
	assert.Equal(t, llm.CategoryNoFailures, mergeCategory(results))
}

func TestMergeCategory_NotSupported(t *testing.T) {
	results := []clusterAnalysis{
		{Result: &llm.AnalysisResult{Category: llm.CategoryNotSupported}},
	}
	assert.Equal(t, llm.CategoryNotSupported, mergeCategory(results))
}

func TestMergeConfidence_Empty(t *testing.T) {
	assert.Equal(t, 0, mergeConfidence(nil))
}

func TestMergeSensitivity_Empty(t *testing.T) {
	assert.Equal(t, "", mergeSensitivity(nil))
}

func TestMergeClusterResults_MixedCategories(t *testing.T) {
	results := []clusterAnalysis{
		{
			Cluster: cluster.Cluster{ID: 1, Failures: make([]cluster.FailureInfo, 3)},
			Result: &llm.AnalysisResult{
				Category: llm.CategoryDiagnosis,
				RCAs:     []llm.RootCauseAnalysis{{Title: "Bug"}},
			},
		},
		{
			Cluster: cluster.Cluster{ID: 2, Failures: make([]cluster.FailureInfo, 1)},
			Result: &llm.AnalysisResult{
				Category: llm.CategoryNotSupported,
			},
		},
	}

	merged := mergeClusterResults(results)

	// If any cluster is diagnosis, overall is diagnosis
	assert.Equal(t, llm.CategoryDiagnosis, merged.Category)
	require.Len(t, merged.RCAs, 1)
}
