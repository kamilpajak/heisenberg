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
	if len(results) == 1 {
		return results[0].Result
	}

	return &llm.AnalysisResult{
		RCAs:        collectRCAs(results),
		Category:    mergeCategory(results),
		Confidence:  mergeConfidence(results),
		Sensitivity: mergeSensitivity(results),
		Text:        mergeText(results),
	}
}

func collectRCAs(results []clusterAnalysis) []llm.RootCauseAnalysis {
	seen := map[string]bool{}
	var rcas []llm.RootCauseAnalysis
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
			rcas = append(rcas, rca)
		}
	}
	return rcas
}

func mergeCategory(results []clusterAnalysis) string {
	for _, ca := range results {
		if ca.Result != nil && ca.Result.Category == llm.CategoryDiagnosis {
			return llm.CategoryDiagnosis
		}
	}
	for _, ca := range results {
		if ca.Result != nil && ca.Result.Category == llm.CategoryNoFailures {
			return llm.CategoryNoFailures
		}
	}
	return llm.CategoryNotSupported
}

func mergeConfidence(results []clusterAnalysis) int {
	totalWeight, weightedSum := 0, 0
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
	if totalWeight == 0 {
		return 0
	}
	return weightedSum / totalWeight
}

func mergeSensitivity(results []clusterAnalysis) string {
	order := map[string]int{"high": 3, "medium": 2, "low": 1}
	best, bestVal := "", 0
	for _, ca := range results {
		if ca.Result == nil {
			continue
		}
		if v := order[ca.Result.Sensitivity]; v > bestVal {
			bestVal = v
			best = ca.Result.Sensitivity
		}
	}
	return best
}

func mergeText(results []clusterAnalysis) string {
	var sb strings.Builder
	for i, ca := range results {
		if ca.Result == nil {
			continue
		}
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&sb, "## Cluster %d (%d jobs)\n\n", ca.Cluster.ID, len(ca.Cluster.Failures))
		sb.WriteString(ca.Result.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}
