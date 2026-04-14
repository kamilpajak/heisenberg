package semcluster

import (
	"context"
	"sync"

	pkgcluster "github.com/kamilpajak/heisenberg/pkg/cluster"
)

// Embedder generates vector embeddings for text. Implemented by
// ee/patterns.EmbeddingClient.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// SemanticStage is a pkg/cluster.Stage that merges singleton clusters
// whose embedding-space cosine similarity exceeds Threshold, subject to
// the antonym guardrail.
//
// Only singletons participate — multi-member clusters already have
// grouping evidence and are left untouched.
type SemanticStage struct {
	Embedder  Embedder
	Threshold float64
	// MaxPairs caps total pairwise comparisons to avoid O(n²) embedding
	// calls on runs with many singletons. 0 = no cap.
	MaxPairs int
}

// Name implements pkg/cluster.Stage.
func (SemanticStage) Name() string { return "semantic" }

// Refine implements pkg/cluster.Stage. Always returns nil error — embedder
// failures degrade to the input cluster list.
func (s SemanticStage) Refine(ctx context.Context, clusters []pkgcluster.Cluster) ([]pkgcluster.Cluster, error) {
	singletons := findSingletonIndices(clusters)
	if len(singletons) < 2 {
		return clusters, nil
	}

	embeddings, ok := s.embedSingletons(ctx, clusters, singletons)
	if !ok {
		return clusters, nil
	}

	uf := pkgcluster.NewUnionFind(len(clusters))
	s.mergeByCosine(uf, clusters, singletons, embeddings)
	return rebuild(uf, clusters), nil
}

func findSingletonIndices(clusters []pkgcluster.Cluster) []int {
	var out []int
	for i, c := range clusters {
		if len(c.Failures) == 1 {
			out = append(out, i)
		}
	}
	return out
}

// embedSingletons returns embeddings keyed by the singleton index in clusters.
// Returns (nil, false) if any embedding call fails — signals the caller to skip the stage.
func (s SemanticStage) embedSingletons(ctx context.Context, clusters []pkgcluster.Cluster, singletons []int) (map[int][]float32, bool) {
	type result struct {
		idx int
		vec []float32
		err error
	}

	results := make(chan result, len(singletons))
	var wg sync.WaitGroup
	for _, idx := range singletons {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := ComputeClusterEmbeddingText(clusters[i].Signature)
			vec, err := s.Embedder.Embed(ctx, text)
			results <- result{idx: i, vec: vec, err: err}
		}(idx)
	}
	wg.Wait()
	close(results)

	out := make(map[int][]float32, len(singletons))
	for r := range results {
		if r.err != nil {
			return nil, false
		}
		out[r.idx] = r.vec
	}
	return out, true
}

func (s SemanticStage) mergeByCosine(uf *pkgcluster.UnionFind, clusters []pkgcluster.Cluster, singletons []int, embeddings map[int][]float32) {
	comparisons := 0
	for i := 0; i < len(singletons); i++ {
		for j := i + 1; j < len(singletons); j++ {
			if s.MaxPairs > 0 && comparisons >= s.MaxPairs {
				return
			}
			comparisons++
			ci, cj := singletons[i], singletons[j]
			if uf.Find(ci) == uf.Find(cj) {
				continue
			}
			sim := cosineSimilarity(embeddings[ci], embeddings[cj])
			if sim < s.Threshold {
				continue
			}
			if antonymGuardrail(clusters[ci].Signature.RawExcerpt, clusters[cj].Signature.RawExcerpt) {
				continue
			}
			uf.Union(ci, cj)
		}
	}
}

func rebuild(uf *pkgcluster.UnionFind, clusters []pkgcluster.Cluster) []pkgcluster.Cluster {
	grouped := map[int][]pkgcluster.FailureInfo{}
	groupSig := map[int]pkgcluster.ErrorSignature{}
	for i, c := range clusters {
		root := uf.Find(i)
		grouped[root] = append(grouped[root], c.Failures...)
		if _, ok := groupSig[root]; !ok {
			groupSig[root] = c.Signature
		}
	}

	var result []pkgcluster.Cluster
	for root, failures := range grouped {
		rep := failures[0]
		for _, f := range failures[1:] {
			if len(f.LogTail) > len(rep.LogTail) {
				rep = f
			}
		}
		result = append(result, pkgcluster.Cluster{
			ID:             len(result) + 1,
			Signature:      groupSig[root],
			Failures:       failures,
			Representative: rep,
		})
	}
	return result
}
