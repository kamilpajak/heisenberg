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

func TestPrintResult_WithStructuredRCA(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  90,
		Sensitivity: "low",
		RCA: &llm.RootCauseAnalysis{
			Title:       "Timeout waiting for Submit Button",
			FailureType: llm.FailureTypeTimeout,
			Location: &llm.CodeLocation{
				FilePath:   "tests/checkout.spec.ts",
				LineNumber: 45,
			},
			Symptom:   "Test timed out after 30000ms",
			RootCause: "Cookie banner overlays submit button",
			Evidence: []llm.Evidence{
				{Type: llm.EvidenceScreenshot, Content: "Overlay visible at z=999"},
				{Type: llm.EvidenceTrace, Content: "Click blocked at 12:34:56"},
			},
			Remediation: "Dismiss cookie banner before form submission",
		},
	}

	printResult(&stderr, &stdout, r)

	out := stdout.String()
	assert.Contains(t, out, "TIMEOUT")
	assert.Contains(t, out, "tests/checkout.spec.ts:45")
	assert.Contains(t, out, "ROOT CAUSE")
	assert.Contains(t, out, "Cookie banner overlays submit button")
	assert.Contains(t, out, "EVIDENCE")
	assert.Contains(t, out, "[Screenshot]")
	assert.Contains(t, out, "Overlay visible")
	assert.Contains(t, out, "FIX")
	assert.Contains(t, out, "Dismiss cookie banner")
}

func TestPrintResult_FallbackToText(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Text:        "Legacy text analysis",
		Category:    llm.CategoryDiagnosis,
		Confidence:  75,
		Sensitivity: "medium",
		RCA:         nil, // No structured RCA
	}

	printResult(&stderr, &stdout, r)

	out := stdout.String()
	assert.Contains(t, out, "Legacy text analysis")
	assert.NotContains(t, out, "ROOT CAUSE")
}

func TestPrintResult_EmptyRCATitle(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Text:        "Fallback text",
		Category:    llm.CategoryDiagnosis,
		Confidence:  60,
		Sensitivity: "medium",
		RCA:         &llm.RootCauseAnalysis{Title: ""}, // Empty title = fallback
	}

	printResult(&stderr, &stdout, r)

	out := stdout.String()
	assert.Contains(t, out, "Fallback text")
}

func TestPrintStructuredRCA_NoLocation(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Network Error",
		FailureType: llm.FailureTypeNetwork,
		Symptom:     "HTTP 500",
		RootCause:   "Backend service down",
		Evidence:    []llm.Evidence{},
		Remediation: "Check backend logs",
	}

	printStructuredRCA(&buf, rca)

	out := buf.String()
	assert.Contains(t, out, "NETWORK")
	assert.NotContains(t, out, ":0") // No line number shown
	assert.Contains(t, out, "Backend service down")
}

func TestPrintStructuredRCA_AllEvidenceTypes(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Test Error",
		FailureType: llm.FailureTypeAssertion,
		RootCause:   "Assertion failed",
		Evidence: []llm.Evidence{
			{Type: llm.EvidenceScreenshot, Content: "Screenshot data"},
			{Type: llm.EvidenceTrace, Content: "Trace data"},
			{Type: llm.EvidenceLog, Content: "Log data"},
			{Type: llm.EvidenceNetwork, Content: "Network data"},
			{Type: llm.EvidenceCode, Content: "Code data"},
		},
		Remediation: "Fix assertion",
	}

	printStructuredRCA(&buf, rca)

	out := buf.String()
	assert.Contains(t, out, "[Screenshot]")
	assert.Contains(t, out, "[Trace]")
	assert.Contains(t, out, "[Log]")
	assert.Contains(t, out, "[Network]")
	assert.Contains(t, out, "[Code]")
}

func TestJSONOutput_WithRCA(t *testing.T) {
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  85,
		Sensitivity: "low",
		RCA: &llm.RootCauseAnalysis{
			Title:       "Timeout Error",
			FailureType: llm.FailureTypeTimeout,
			Location: &llm.CodeLocation{
				FilePath:   "tests/login.spec.ts",
				LineNumber: 42,
			},
			Symptom:     "Timed out",
			RootCause:   "Slow network",
			Evidence:    []llm.Evidence{{Type: llm.EvidenceTrace, Content: "Delay"}},
			Remediation: "Add retry",
		},
	}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(r)
	require.NoError(t, err)

	var decoded llm.AnalysisResult
	err = json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	require.NotNil(t, decoded.RCA)
	assert.Equal(t, "Timeout Error", decoded.RCA.Title)
	assert.Equal(t, llm.FailureTypeTimeout, decoded.RCA.FailureType)
	assert.Equal(t, "tests/login.spec.ts", decoded.RCA.Location.FilePath)
	assert.Equal(t, 42, decoded.RCA.Location.LineNumber)
	assert.Len(t, decoded.RCA.Evidence, 1)
}
