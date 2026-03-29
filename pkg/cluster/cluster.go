package cluster

import "sort"

const (
	// jaccardThreshold is the minimum similarity to merge two singleton clusters.
	// 0.8 is conservative — short error messages share too much boilerplate at lower thresholds.
	jaccardThreshold = 0.8

	// maxClusters caps the number of clusters. Excess failures go to Unclustered.
	maxClusters = 10
)

// ClusterFailures groups failures by error signature.
// Phase A: exact match on normalized signature.
// Phase B: Jaccard merge for similar singletons.
// Returns a single-cluster result as fallback.
func ClusterFailures(failures []FailureInfo) Result {
	if len(failures) == 0 {
		return Result{Method: "single"}
	}
	if len(failures) == 1 {
		c := buildCluster(0, failures)
		return Result{
			Clusters:    []Cluster{c},
			TotalFailed: 1,
			Method:      "single",
		}
	}

	// Separate failures with/without signatures
	var withSig, withoutSig []FailureInfo
	for _, f := range failures {
		if f.Signature.Category == "" {
			withoutSig = append(withoutSig, f)
		} else {
			withSig = append(withSig, f)
		}
	}

	// Phase A: exact match on Normalized string
	groups := map[string][]FailureInfo{}
	for _, f := range withSig {
		key := f.Signature.Normalized
		groups[key] = append(groups[key], f)
	}

	// Convert to cluster list
	var clusters []Cluster
	for _, members := range groups {
		clusters = append(clusters, buildCluster(len(clusters), members))
	}

	method := "exact"

	// Phase B: Jaccard merge for singletons
	merged := jaccardMerge(clusters)
	if len(merged) < len(clusters) {
		method = "jaccard"
	}
	clusters = merged

	// Sort by size descending (largest cluster first)
	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].Failures) > len(clusters[j].Failures)
	})

	// Cap at maxClusters — excess goes to unclustered
	if len(clusters) > maxClusters {
		for _, c := range clusters[maxClusters:] {
			withoutSig = append(withoutSig, c.Failures...)
		}
		clusters = clusters[:maxClusters]
	}

	// Re-number clusters
	for i := range clusters {
		clusters[i].ID = i + 1
	}

	return Result{
		Clusters:    clusters,
		Unclustered: withoutSig,
		TotalFailed: len(failures),
		Method:      method,
	}
}

// buildCluster creates a Cluster from a group of failures.
// The representative is the failure with the longest LogTail.
func buildCluster(id int, failures []FailureInfo) Cluster {
	rep := failures[0]
	for _, f := range failures[1:] {
		if len(f.LogTail) > len(rep.LogTail) {
			rep = f
		}
	}
	return Cluster{
		ID:             id + 1,
		Signature:      failures[0].Signature,
		Failures:       failures,
		Representative: rep,
	}
}

// jaccardMerge merges singleton clusters with high Jaccard similarity.
func jaccardMerge(clusters []Cluster) []Cluster {
	if len(clusters) <= 1 {
		return clusters
	}

	// Find singletons and multi-member clusters
	var singletons []int
	for i, c := range clusters {
		if len(c.Failures) == 1 {
			singletons = append(singletons, i)
		}
	}

	if len(singletons) <= 1 {
		return clusters
	}

	// Union-find for merging
	parent := make([]int, len(clusters))
	for i := range parent {
		parent[i] = i
	}

	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}

	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	// Compare all pairs of singletons
	for i := 0; i < len(singletons); i++ {
		for j := i + 1; j < len(singletons); j++ {
			ci, cj := singletons[i], singletons[j]
			if find(ci) == find(cj) {
				continue // already merged
			}
			sim := jaccard(
				clusters[ci].Signature.Tokens,
				clusters[cj].Signature.Tokens,
			)
			if sim >= jaccardThreshold {
				union(ci, cj)
			}
		}
	}

	// Rebuild clusters based on union-find
	merged := map[int][]FailureInfo{}
	mergedSig := map[int]ErrorSignature{}
	for i, c := range clusters {
		root := find(i)
		merged[root] = append(merged[root], c.Failures...)
		if _, ok := mergedSig[root]; !ok {
			mergedSig[root] = c.Signature
		}
	}

	var result []Cluster
	for root, failures := range merged {
		c := buildCluster(len(result), failures)
		c.Signature = mergedSig[root]
		result = append(result, c)
	}
	return result
}

// jaccard computes the Jaccard similarity between two token sets.
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	setA := toSet(a)
	setB := toSet(b)

	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}

	union := len(setA)
	for k := range setB {
		if !setA[k] {
			union++
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func toSet(tokens []string) map[string]bool {
	s := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		s[t] = true
	}
	return s
}
