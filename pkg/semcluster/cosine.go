// Package semcluster provides semantic failure clustering on top of the
// pkg/cluster pipeline. Near-duplicate failures (same root cause, slightly
// different error text) can be collapsed into one cluster using embedding
// similarity with an antonym guardrail against false merges.
package semcluster

import "math"

// cosineSimilarity returns the cosine similarity of two equal-length vectors.
// Returns 0 for length mismatch or zero-magnitude inputs.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		magA += x * x
		magB += y * y
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
