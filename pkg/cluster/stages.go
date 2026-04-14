package cluster

import "context"

// Stage refines a cluster list. Stages run after exact-match grouping
// and before the cluster cap. The semantic clustering feature plugs in
// as a Stage.
//
// Contract: Refine never increases the cluster count — stages only merge.
// On error, Refine must return the input clusters unchanged so the pipeline
// degrades gracefully.
type Stage interface {
	Name() string
	Refine(ctx context.Context, clusters []Cluster) ([]Cluster, error)
}

// JaccardStage merges singleton clusters with high Jaccard similarity on tokens.
// This is the default OSS stage.
type JaccardStage struct{}

func (JaccardStage) Name() string { return "jaccard" }

func (JaccardStage) Refine(_ context.Context, clusters []Cluster) ([]Cluster, error) {
	return jaccardMerge(clusters), nil
}
