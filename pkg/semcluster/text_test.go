package semcluster

import (
	"strings"
	"testing"

	pkgcluster "github.com/kamilpajak/heisenberg/pkg/cluster"
	"github.com/stretchr/testify/assert"
)

func TestComputeClusterEmbeddingText_IncludesAllSignals(t *testing.T) {
	sig := pkgcluster.ErrorSignature{
		Category:   "stack_trace",
		Normalized: "main.go: runtime error",
		RawExcerpt: "panic: runtime error: index out of range",
		TopFrames:  []string{"main.go:42", "handler.go:88", "router.go:15"},
	}

	out := ComputeClusterEmbeddingText(sig)

	assert.Contains(t, out, "stack_trace")
	assert.Contains(t, out, "panic: runtime error")
	assert.Contains(t, out, "main.go: runtime error")
	assert.Contains(t, out, "main.go:42")
	assert.Contains(t, out, "handler.go:88")
	assert.Contains(t, out, "router.go:15")
}

func TestComputeClusterEmbeddingText_OmitsEmptyTopFrames(t *testing.T) {
	sig := pkgcluster.ErrorSignature{
		Category:   "exit_code",
		Normalized: "exit code 1",
		RawExcerpt: "Process completed with exit code 1",
	}

	out := ComputeClusterEmbeddingText(sig)

	assert.Contains(t, out, "exit_code")
	assert.Contains(t, out, "exit code 1")
	assert.NotContains(t, out, "top_frames:")
}

func TestComputeClusterEmbeddingText_IsDeterministic(t *testing.T) {
	sig := pkgcluster.ErrorSignature{
		Category:   "error_message",
		Normalized: "connection refused",
		RawExcerpt: "connection refused",
		TopFrames:  []string{"a.go:1"},
	}
	a := ComputeClusterEmbeddingText(sig)
	b := ComputeClusterEmbeddingText(sig)
	assert.Equal(t, a, b)
}

func TestComputeClusterEmbeddingText_BoundedLength(t *testing.T) {
	// Guard against accidental raw log interpolation blowing up token cost.
	long := strings.Repeat("x", 10000)
	sig := pkgcluster.ErrorSignature{
		Category:   "error_message",
		Normalized: long,
		RawExcerpt: long,
	}

	out := ComputeClusterEmbeddingText(sig)

	assert.LessOrEqual(t, len(out), maxEmbeddingTextLen, "text must be bounded for predictable embedding cost")
}
