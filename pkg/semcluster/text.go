package semcluster

import (
	"fmt"
	"strings"

	pkgcluster "github.com/kamilpajak/heisenberg/pkg/cluster"
)

// maxEmbeddingTextLen bounds the length of the string fed to the embedding
// model so per-cluster embedding cost stays predictable regardless of log
// size. 500 chars ~ 125 tokens — well above the semantic signal threshold,
// well below per-call pricing concerns.
const maxEmbeddingTextLen = 500

// ComputeClusterEmbeddingText builds a compact, deterministic textual
// representation of a failure signature for embedding. The format is
// stable across calls so identical signatures produce identical vectors.
func ComputeClusterEmbeddingText(sig pkgcluster.ErrorSignature) string {
	var b strings.Builder

	fmt.Fprintf(&b, "category: %s\n", sig.Category)
	if sig.RawExcerpt != "" {
		fmt.Fprintf(&b, "error: %s\n", sig.RawExcerpt)
	}
	if sig.Normalized != "" {
		fmt.Fprintf(&b, "normalized: %s\n", sig.Normalized)
	}
	if len(sig.TopFrames) > 0 {
		fmt.Fprintf(&b, "top_frames: %s\n", strings.Join(sig.TopFrames, " | "))
	}

	out := b.String()
	if len(out) > maxEmbeddingTextLen {
		out = out[:maxEmbeddingTextLen]
	}
	return out
}
