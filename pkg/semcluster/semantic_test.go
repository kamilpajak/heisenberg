package semcluster

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	pkgcluster "github.com/kamilpajak/heisenberg/pkg/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const diskFullExcerpt = "disk full"

// fakeEmbedder returns canned vectors per input text. Thread-safe so the
// parallel embedSingletons path can be exercised under -race.
type fakeEmbedder struct {
	byText map[string][]float32
	err    error
	calls  atomic.Int64
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if v, ok := f.byText[text]; ok {
		return v, nil
	}
	// Default to orthogonal-to-everything.
	return []float32{0, 0, 1}, nil
}

func singletonCluster(id int, normalized, excerpt string) pkgcluster.Cluster {
	sig := pkgcluster.ErrorSignature{
		Category:   "error_message",
		Normalized: normalized,
		RawExcerpt: excerpt,
	}
	f := pkgcluster.FailureInfo{
		JobID:     int64(id),
		JobName:   normalized,
		Signature: sig,
		LogTail:   excerpt,
	}
	return pkgcluster.Cluster{
		ID:             id,
		Signature:      sig,
		Failures:       []pkgcluster.FailureInfo{f},
		Representative: f,
	}
}

func TestSemanticStage_MergesNearDuplicates(t *testing.T) {
	// A and B are cosine-close; C is orthogonal.
	aText := ComputeClusterEmbeddingText(singletonCluster(1, "a1", "auth failed").Signature)
	bText := ComputeClusterEmbeddingText(singletonCluster(2, "a2", "auth error").Signature)
	cText := ComputeClusterEmbeddingText(singletonCluster(3, "different", diskFullExcerpt).Signature)

	embedder := &fakeEmbedder{byText: map[string][]float32{
		aText: {1, 0, 0},
		bText: {0.99, 0.14, 0}, // cosine ~ 0.99
		cText: {0, 0, 1},
	}}

	stage := SemanticStage{Embedder: embedder, Threshold: 0.9}
	in := []pkgcluster.Cluster{
		singletonCluster(1, "a1", "auth failed"),
		singletonCluster(2, "a2", "auth error"),
		singletonCluster(3, "different", diskFullExcerpt),
	}

	out, err := stage.Refine(context.Background(), in)

	require.NoError(t, err)
	assert.Len(t, out, 2, "A and B should merge; C untouched")
}

func TestSemanticStage_BelowThreshold_NoMerge(t *testing.T) {
	aText := ComputeClusterEmbeddingText(singletonCluster(1, "a1", "x").Signature)
	bText := ComputeClusterEmbeddingText(singletonCluster(2, "a2", "y").Signature)

	embedder := &fakeEmbedder{byText: map[string][]float32{
		aText: {1, 0, 0},
		bText: {0.8, 0.6, 0}, // cosine ~ 0.80
	}}

	stage := SemanticStage{Embedder: embedder, Threshold: 0.9}
	in := []pkgcluster.Cluster{
		singletonCluster(1, "a1", "x"),
		singletonCluster(2, "a2", "y"),
	}

	out, err := stage.Refine(context.Background(), in)

	require.NoError(t, err)
	assert.Len(t, out, 2, "below-threshold pairs must not merge")
}

func TestSemanticStage_EmbedderError_DegradesGracefully(t *testing.T) {
	embedder := &fakeEmbedder{err: errors.New("API down")}
	stage := SemanticStage{Embedder: embedder, Threshold: 0.9}
	in := []pkgcluster.Cluster{
		singletonCluster(1, "a", "x"),
		singletonCluster(2, "b", "y"),
	}

	out, err := stage.Refine(context.Background(), in)

	require.NoError(t, err, "embedder failures must not break the pipeline")
	assert.Equal(t, in, out, "input returned unchanged on embedder error")
}

func TestSemanticStage_SkipsMultiMemberClusters(t *testing.T) {
	// Multi-member clusters already have >1 grouping evidence; don't touch them.
	multi := pkgcluster.Cluster{
		ID:        1,
		Signature: pkgcluster.ErrorSignature{Category: "error_message", Normalized: "x"},
		Failures: []pkgcluster.FailureInfo{
			{JobID: 10}, {JobID: 11},
		},
	}
	sing := singletonCluster(2, "other", diskFullExcerpt)

	embedder := &fakeEmbedder{byText: map[string][]float32{}}
	stage := SemanticStage{Embedder: embedder, Threshold: 0.9}

	out, err := stage.Refine(context.Background(), []pkgcluster.Cluster{multi, sing})

	require.NoError(t, err)
	assert.Len(t, out, 2)
	assert.Equal(t, int64(0), embedder.calls.Load(), "multi-member cluster needs no embedding; only 1 singleton → no pairs")
}

func TestSemanticStage_AntonymGuardrail_VetoesOppositeOutcomes(t *testing.T) {
	// Embeddings would say 0.99 similar, but antonym veto blocks it.
	aCluster := singletonCluster(1, "test passed", "all tests passed")
	bCluster := singletonCluster(2, "test failed", "test failed: auth")

	embedder := &fakeEmbedder{byText: map[string][]float32{
		ComputeClusterEmbeddingText(aCluster.Signature): {1, 0, 0},
		ComputeClusterEmbeddingText(bCluster.Signature): {0.99, 0.14, 0},
	}}

	stage := SemanticStage{Embedder: embedder, Threshold: 0.9}
	out, err := stage.Refine(context.Background(), []pkgcluster.Cluster{aCluster, bCluster})

	require.NoError(t, err)
	assert.Len(t, out, 2, "antonym guardrail must block a passed-vs-failed merge")
}

func TestSemanticStage_SingleSingleton_Skips(t *testing.T) {
	embedder := &fakeEmbedder{byText: map[string][]float32{}}
	stage := SemanticStage{Embedder: embedder, Threshold: 0.9}

	out, err := stage.Refine(context.Background(), []pkgcluster.Cluster{
		singletonCluster(1, "only", "x"),
	})

	require.NoError(t, err)
	assert.Len(t, out, 1)
	assert.Equal(t, int64(0), embedder.calls.Load(), "single singleton: no pair to compare, no embedding call")
}

func TestSemanticStage_NameReportsCorrectly(t *testing.T) {
	assert.Equal(t, "semantic", SemanticStage{}.Name())
}

func TestSemanticStage_MaxPairs_CapsComparisons(t *testing.T) {
	// 4 singletons with identical embeddings → 6 potential pairs, all above
	// threshold. MaxPairs=1 caps it so only the first pair merges.
	embedder := &fakeEmbedder{byText: map[string][]float32{}}
	stage := SemanticStage{Embedder: embedder, Threshold: 0.0, MaxPairs: 1}

	in := []pkgcluster.Cluster{
		singletonCluster(1, "a", "a"),
		singletonCluster(2, "b", "b"),
		singletonCluster(3, "c", "c"),
		singletonCluster(4, "d", "d"),
	}

	out, err := stage.Refine(context.Background(), in)

	require.NoError(t, err)
	// First pair (indices 0,1) merges; indices 2,3 untouched = 3 clusters total.
	assert.Len(t, out, 3, "MaxPairs=1 must short-circuit after the first comparison")
}
