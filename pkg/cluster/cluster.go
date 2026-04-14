package cluster

import (
	"context"
	"sort"
)

const (
	// jaccardThreshold is the minimum similarity to merge two singleton clusters.
	// 0.8 is conservative — short error messages share too much boilerplate at lower thresholds.
	jaccardThreshold = 0.8

	// maxClusters caps the number of clusters. Excess failures go to Unclustered.
	maxClusters = 10
)

// ClusterFailures groups failures by error signature using the default
// OSS pipeline: exact match → Jaccard merge.
func ClusterFailures(failures []FailureInfo) Result {
	result, _ := ClusterFailuresWithStages(context.Background(), failures, JaccardStage{})
	return result
}

// ClusterFailuresWithStages runs the clustering pipeline with a custom
// ordered list of refinement stages. Stages run after exact-match grouping
// and before the cluster cap. An error from any stage causes that stage's
// output to be discarded; prior stage output is preserved.
//
// The returned error is always nil today; reserved for future stages that
// need to surface fatal conditions to callers.
func ClusterFailuresWithStages(ctx context.Context, failures []FailureInfo, stages ...Stage) (Result, error) {
	if len(failures) == 0 {
		return Result{Method: "single"}, nil
	}
	if len(failures) == 1 {
		c := buildCluster(0, failures)
		return Result{
			Clusters:    []Cluster{c},
			TotalFailed: 1,
			Method:      "single",
		}, nil
	}

	withSig, withoutSig := partitionBySignature(failures)
	clusters := exactMatchClusters(withSig)
	method := "exact"

	for _, stage := range stages {
		refined, err := stage.Refine(ctx, clusters)
		if err != nil {
			continue
		}
		if len(refined) < len(clusters) {
			method = stage.Name()
		}
		clusters = refined
	}

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

	for i := range clusters {
		clusters[i].ID = i + 1
	}

	return Result{
		Clusters:    clusters,
		Unclustered: withoutSig,
		TotalFailed: len(failures),
		Method:      method,
	}, nil
}

func partitionBySignature(failures []FailureInfo) (withSig, withoutSig []FailureInfo) {
	for _, f := range failures {
		if f.Signature.Category == "" {
			withoutSig = append(withoutSig, f)
		} else {
			withSig = append(withSig, f)
		}
	}
	return
}

func exactMatchClusters(withSig []FailureInfo) []Cluster {
	groups := map[string][]FailureInfo{}
	for _, f := range withSig {
		key := f.Signature.Normalized
		groups[key] = append(groups[key], f)
	}
	var clusters []Cluster
	for _, members := range groups {
		clusters = append(clusters, buildCluster(len(clusters), members))
	}
	return clusters
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

	singletons := findSingletons(clusters)
	if len(singletons) <= 1 {
		return clusters
	}

	uf := NewUnionFind(len(clusters))
	mergeSimilarSingletons(uf, singletons, clusters)
	return rebuildClusters(uf, clusters)
}

func findSingletons(clusters []Cluster) []int {
	var singletons []int
	for i, c := range clusters {
		if len(c.Failures) == 1 {
			singletons = append(singletons, i)
		}
	}
	return singletons
}

func mergeSimilarSingletons(uf *UnionFind, singletons []int, clusters []Cluster) {
	for i := 0; i < len(singletons); i++ {
		for j := i + 1; j < len(singletons); j++ {
			ci, cj := singletons[i], singletons[j]
			if uf.Find(ci) == uf.Find(cj) {
				continue
			}
			sim := jaccard(clusters[ci].Signature.Tokens, clusters[cj].Signature.Tokens)
			if sim >= jaccardThreshold {
				uf.Union(ci, cj)
			}
		}
	}
}

func rebuildClusters(uf *UnionFind, clusters []Cluster) []Cluster {
	grouped := map[int][]FailureInfo{}
	groupSig := map[int]ErrorSignature{}
	for i, c := range clusters {
		root := uf.Find(i)
		grouped[root] = append(grouped[root], c.Failures...)
		if _, ok := groupSig[root]; !ok {
			groupSig[root] = c.Signature
		}
	}

	var result []Cluster
	for root, failures := range grouped {
		c := buildCluster(len(result), failures)
		c.Signature = groupSig[root]
		result = append(result, c)
	}
	return result
}

// UnionFind is a simple disjoint-set data structure with path compression.
type UnionFind struct {
	parent []int
}

func NewUnionFind(n int) *UnionFind {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	return &UnionFind{parent: parent}
}

// Find returns the root of the set containing x, with path compression.
func (uf *UnionFind) Find(x int) int {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]]
		x = uf.parent[x]
	}
	return x
}

// Union merges the sets containing a and b.
func (uf *UnionFind) Union(a, b int) {
	ra, rb := uf.Find(a), uf.Find(b)
	if ra != rb {
		uf.parent[rb] = ra
	}
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
