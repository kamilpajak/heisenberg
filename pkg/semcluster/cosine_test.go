package semcluster

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	v := []float32{1, 2, 3}
	assert.InDelta(t, 1.0, cosineSimilarity(v, v), 1e-6)
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	assert.InDelta(t, 0.0, cosineSimilarity(a, b), 1e-6)
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	assert.InDelta(t, -1.0, cosineSimilarity(a, b), 1e-6)
}

func TestCosineSimilarity_DifferentLengths_Zero(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	assert.Equal(t, 0.0, cosineSimilarity(a, b))
}

func TestCosineSimilarity_ZeroVector_Zero(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	assert.Equal(t, 0.0, cosineSimilarity(a, b))
}

func TestCosineSimilarity_KnownCase(t *testing.T) {
	// Two unit vectors at 60° → cos(60°) = 0.5
	a := []float32{1, 0}
	b := []float32{float32(math.Cos(math.Pi / 3)), float32(math.Sin(math.Pi / 3))}
	assert.InDelta(t, 0.5, cosineSimilarity(a, b), 1e-6)
}
