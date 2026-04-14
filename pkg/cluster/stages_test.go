package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergeAllStage unconditionally collapses all clusters into one. Useful for
// testing stage plumbing without depending on real refinement logic.
type mergeAllStage struct{ name string }

func (m mergeAllStage) Name() string { return m.name }

func (mergeAllStage) Refine(_ context.Context, clusters []Cluster) ([]Cluster, error) {
	if len(clusters) <= 1 {
		return clusters, nil
	}
	var all []FailureInfo
	for _, c := range clusters {
		all = append(all, c.Failures...)
	}
	return []Cluster{buildCluster(0, all)}, nil
}

// noopStage leaves clusters untouched. Used to verify method attribution
// only updates when a stage actually reduces the cluster count.
type noopStage struct{ name string }

func (n noopStage) Name() string { return n.name }

func (noopStage) Refine(_ context.Context, clusters []Cluster) ([]Cluster, error) {
	return clusters, nil
}

// erroringStage returns an error. The pipeline must discard its output.
type erroringStage struct{}

func (erroringStage) Name() string { return "erroring" }

func (erroringStage) Refine(_ context.Context, _ []Cluster) ([]Cluster, error) {
	return nil, errors.New("boom")
}

func TestClusterFailuresWithStages_NoStages_UsesExactOnly(t *testing.T) {
	failures := []FailureInfo{
		fail(1, testNameA, sig("error_message", msgConnectionRefused)),
		fail(2, testNameB, sig("error_message", "other error")),
	}

	result, err := ClusterFailuresWithStages(context.Background(), failures)

	require.NoError(t, err)
	assert.Len(t, result.Clusters, 2, "without stages, only exact match groups apply")
	assert.Equal(t, "exact", result.Method)
}

func TestClusterFailuresWithStages_StageNameWinsWhenItMerges(t *testing.T) {
	failures := []FailureInfo{
		fail(1, testNameA, sig("error_message", "a")),
		fail(2, testNameB, sig("error_message", "b")),
	}

	result, err := ClusterFailuresWithStages(context.Background(), failures, mergeAllStage{name: "merge-all"})

	require.NoError(t, err)
	assert.Len(t, result.Clusters, 1)
	assert.Equal(t, "merge-all", result.Method)
}

func TestClusterFailuresWithStages_NoopStage_KeepsExactMethod(t *testing.T) {
	failures := []FailureInfo{
		fail(1, testNameA, sig("error_message", "a")),
		fail(2, testNameB, sig("error_message", "b")),
	}

	result, err := ClusterFailuresWithStages(context.Background(), failures, noopStage{name: "noop"})

	require.NoError(t, err)
	assert.Len(t, result.Clusters, 2)
	assert.Equal(t, "exact", result.Method, "method only changes when cluster count drops")
}

func TestClusterFailuresWithStages_ErroringStage_Degrades(t *testing.T) {
	failures := []FailureInfo{
		fail(1, testNameA, sig("error_message", "a")),
		fail(2, testNameB, sig("error_message", "b")),
	}

	result, err := ClusterFailuresWithStages(context.Background(), failures, erroringStage{})

	require.NoError(t, err, "stage errors must not fail the whole pipeline")
	assert.Len(t, result.Clusters, 2, "erroring stage output is discarded")
	assert.Equal(t, "exact", result.Method)
}

func TestClusterFailuresWithStages_MultipleStages_RunInOrder(t *testing.T) {
	failures := []FailureInfo{
		fail(1, testNameA, sig("error_message", "a")),
		fail(2, testNameB, sig("error_message", "b")),
	}

	result, err := ClusterFailuresWithStages(
		context.Background(),
		failures,
		noopStage{name: "first"},
		mergeAllStage{name: "second"},
	)

	require.NoError(t, err)
	assert.Len(t, result.Clusters, 1)
	assert.Equal(t, "second", result.Method, "later stage's name wins when it merges")
}

func TestJaccardStage_RefineMatchesLegacyBehavior(t *testing.T) {
	failures := []FailureInfo{
		fail(1, "a", sig("error_message", "authentication failed for user alice")),
		fail(2, "b", sig("error_message", "authentication failed for user bob")),
	}

	legacy := ClusterFailures(failures)
	viaStage, err := ClusterFailuresWithStages(context.Background(), failures, JaccardStage{})

	require.NoError(t, err)
	assert.Equal(t, len(legacy.Clusters), len(viaStage.Clusters))
	assert.Equal(t, legacy.Method, viaStage.Method)
}
