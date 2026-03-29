package analysis

import (
	"fmt"
	"strings"

	"github.com/kamilpajak/heisenberg/pkg/cluster"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// clusterAnalysis pairs a cluster with its LLM analysis result.
type clusterAnalysis struct {
	Cluster cluster.Cluster
	Result  *llm.AnalysisResult
}

// mergeClusterResults combines per-cluster AnalysisResults into a single
// run-level result. It deduplicates RCAs with identical root causes,
// computes weighted confidence, and generates a summary text.
func mergeClusterResults(results []clusterAnalysis) *llm.AnalysisResult {
	if len(results) == 0 {
		return &llm.AnalysisResult{}
	}

	// Single cluster — return as-is
	if len(results) == 1 {
		return results[0].Result
	}

	merged := &llm.AnalysisResult{}

	// Collect RCAs, deduplicating identical root causes
	seen := map[string]bool{}
	for _, ca := range results {
		if ca.Result == nil {
			continue
		}
		for _, rca := range ca.Result.RCAs {
			key := strings.ToLower(strings.TrimSpace(rca.Title + "|" + rca.RootCause))
			if seen[key] {
				continue
			}
			seen[key] = true
			merged.RCAs = append(merged.RCAs, rca)
		}
	}

	// Category: diagnosis if any cluster has diagnosis
	merged.Category = llm.CategoryNotSupported
	for _, ca := range results {
		if ca.Result != nil && ca.Result.Category == llm.CategoryDiagnosis {
			merged.Category = llm.CategoryDiagnosis
			break
		}
	}
	if merged.Category != llm.CategoryDiagnosis {
		for _, ca := range results {
			if ca.Result != nil && ca.Result.Category == llm.CategoryNoFailures {
				merged.Category = llm.CategoryNoFailures
				break
			}
		}
	}

	// Confidence: weighted average by cluster size
	totalWeight := 0
	weightedSum := 0
	for _, ca := range results {
		if ca.Result == nil {
			continue
		}
		w := len(ca.Cluster.Failures)
		if w == 0 {
			w = 1
		}
		weightedSum += ca.Result.Confidence * w
		totalWeight += w
	}
	if totalWeight > 0 {
		merged.Confidence = weightedSum / totalWeight
	}

	// Sensitivity: take highest (worst)
	sensOrder := map[string]int{"high": 3, "medium": 2, "low": 1}
	maxSens := 0
	for _, ca := range results {
		if ca.Result != nil {
			if s, ok := sensOrder[ca.Result.Sensitivity]; ok && s > maxSens {
				maxSens = s
				merged.Sensitivity = ca.Result.Sensitivity
			}
		}
	}

	// Text: structured summary
	var sb strings.Builder
	for i, ca := range results {
		if ca.Result == nil {
			continue
		}
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString(fmt.Sprintf("## Cluster %d (%d jobs)\n\n", ca.Cluster.ID, len(ca.Cluster.Failures)))
		sb.WriteString(ca.Result.Text)
		sb.WriteString("\n")
	}
	merged.Text = sb.String()

	return merged
}
