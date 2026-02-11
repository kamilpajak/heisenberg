package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/fatih/color"
	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	color.NoColor = true
}

func TestPrintResult_Diagnosis(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Text:        "Root cause: timeout in login",
		Category:    llm.CategoryDiagnosis,
		Confidence:  85,
		Sensitivity: "low",
	}

	printResult(&stderr, &stdout, r)

	assert.Contains(t, stderr.String(), "━")
	assert.Contains(t, stderr.String(), "Confidence: 85%")
	assert.Contains(t, stderr.String(), "low sensitivity")
	assert.Contains(t, stdout.String(), "Root cause: timeout in login")
	assert.NotContains(t, stderr.String(), "Tip:")
}

func TestPrintResult_DiagnosisLowConfidenceHighSensitivity(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Text:        "Possible timeout",
		Category:    llm.CategoryDiagnosis,
		Confidence:  40,
		Sensitivity: "high",
	}

	printResult(&stderr, &stdout, r)

	assert.Contains(t, stderr.String(), "Confidence: 40%")
	assert.Contains(t, stderr.String(), "Tip:")
	assert.Contains(t, stdout.String(), "Possible timeout")
}

func TestPrintResult_NoFailures(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Text:     "All tests passing",
		Category: llm.CategoryNoFailures,
	}

	printResult(&stderr, &stdout, r)

	assert.NotContains(t, stderr.String(), "Confidence")
	assert.NotContains(t, stderr.String(), "━")
	assert.Contains(t, stdout.String(), "All tests passing")
}

func TestPrintConfidenceBar_High(t *testing.T) {
	var buf bytes.Buffer
	printConfidenceBar(&buf, 90, "low")

	out := buf.String()
	assert.Contains(t, out, "Confidence: 90%")
	assert.Contains(t, out, "█")
	assert.Contains(t, out, "low sensitivity")
}

func TestPrintConfidenceBar_Medium(t *testing.T) {
	var buf bytes.Buffer
	printConfidenceBar(&buf, 50, "medium")

	assert.Contains(t, buf.String(), "Confidence: 50%")
}

func TestPrintConfidenceBar_Low(t *testing.T) {
	var buf bytes.Buffer
	printConfidenceBar(&buf, 20, "high")

	assert.Contains(t, buf.String(), "Confidence: 20%")
	assert.Contains(t, buf.String(), "high sensitivity")
}

func TestPrintConfidenceBar_OverflowClamped(t *testing.T) {
	var buf bytes.Buffer
	printConfidenceBar(&buf, 150, "low")

	assert.Contains(t, buf.String(), "Confidence: 150%")
}

func TestJSONOutput(t *testing.T) {
	r := &llm.AnalysisResult{
		Text:        "Root cause: timeout in login",
		Category:    llm.CategoryDiagnosis,
		Confidence:  85,
		Sensitivity: "low",
	}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(r)
	require.NoError(t, err)

	var decoded llm.AnalysisResult
	err = json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	assert.Equal(t, r.Text, decoded.Text)
	assert.Equal(t, r.Category, decoded.Category)
	assert.Equal(t, r.Confidence, decoded.Confidence)
	assert.Equal(t, r.Sensitivity, decoded.Sensitivity)
}
