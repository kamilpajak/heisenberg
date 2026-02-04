package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoneDiagnosis(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(85),
			"missing_information_sensitivity": "low",
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, CategoryDiagnosis, h.DiagnosisCategory())
	assert.Equal(t, 85, h.DiagnosisConfidence())
	assert.Equal(t, "low", h.DiagnosisSensitivity())
}

func TestDoneNoFailures(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "no_failures"},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, CategoryNoFailures, h.DiagnosisCategory())
}

func TestDoneNotSupported(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "not_supported"},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, CategoryNotSupported, h.DiagnosisCategory())
}

func TestDoneWithNoArgs(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, CategoryDiagnosis, h.DiagnosisCategory())
	assert.Equal(t, 50, h.DiagnosisConfidence())
	assert.Equal(t, "medium", h.DiagnosisSensitivity())
}

func TestDoneWithFloat64Confidence(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(72.8),
			"missing_information_sensitivity": "high",
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, 72, h.DiagnosisConfidence())
}

func TestDoneWithInvalidSensitivity(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(60),
			"missing_information_sensitivity": "invalid",
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, "medium", h.DiagnosisSensitivity())
}

func TestDoneWithInvalidCategory(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "unknown_value"},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, CategoryDiagnosis, h.DiagnosisCategory(), "invalid category should fall back to diagnosis")
}

func TestDoneConfidenceClampedAbove100(t *testing.T) {
	h := &ToolHandler{}
	_, _, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "diagnosis", "confidence": float64(150)},
	})
	require.NoError(t, err)
	assert.Equal(t, 100, h.DiagnosisConfidence())
}

func TestDoneConfidenceClampedBelowZero(t *testing.T) {
	h := &ToolHandler{}
	_, _, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "diagnosis", "confidence": float64(-10)},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, h.DiagnosisConfidence())
}

func TestDoneSkippedCategory(t *testing.T) {
	h := &ToolHandler{}
	assert.Empty(t, h.DiagnosisCategory())
}
