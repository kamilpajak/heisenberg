package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCauseAnalysis_JSONSerialization(t *testing.T) {
	rca := &RootCauseAnalysis{
		Title:       "Timeout waiting for Submit Button",
		FailureType: FailureTypeTimeout,
		Location: &CodeLocation{
			FilePath:     "tests/checkout.spec.ts",
			LineNumber:   45,
			FunctionName: "test('user can checkout')",
		},
		Symptom:   "Test timed out after 30000ms waiting for selector '#submit-btn'",
		RootCause: "Cookie banner overlays the submit button, intercepting click events",
		Evidence: []Evidence{
			{Type: EvidenceScreenshot, Content: "Screenshot shows .cookie-overlay at z-index 999"},
			{Type: EvidenceTrace, Content: "Click action blocked at 12:34:56.123"},
		},
		Remediation: "Close the cookie banner before interacting with the submit button",
	}

	data, err := json.Marshal(rca)
	require.NoError(t, err)

	var decoded RootCauseAnalysis
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, rca.Title, decoded.Title)
	assert.Equal(t, rca.FailureType, decoded.FailureType)
	assert.Equal(t, rca.Location.FilePath, decoded.Location.FilePath)
	assert.Equal(t, rca.Location.LineNumber, decoded.Location.LineNumber)
	assert.Equal(t, rca.Symptom, decoded.Symptom)
	assert.Equal(t, rca.RootCause, decoded.RootCause)
	assert.Len(t, decoded.Evidence, 2)
	assert.Equal(t, rca.Remediation, decoded.Remediation)
}

func TestRootCauseAnalysis_MinimalValid(t *testing.T) {
	// Minimal valid RCA - only required fields
	rca := &RootCauseAnalysis{
		Title:       "Test failed",
		FailureType: FailureTypeAssertion,
		Symptom:     "Assertion failed",
		RootCause:   "Expected value mismatch",
		Evidence:    []Evidence{},
		Remediation: "Fix the expected value",
	}

	data, err := json.Marshal(rca)
	require.NoError(t, err)

	var decoded RootCauseAnalysis
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, rca.Title, decoded.Title)
	assert.Nil(t, decoded.Location) // Optional field
}

func TestCodeLocation_JSONSerialization(t *testing.T) {
	loc := &CodeLocation{
		FilePath:     "src/components/Button.tsx",
		LineNumber:   42,
		FunctionName: "handleClick",
	}

	data, err := json.Marshal(loc)
	require.NoError(t, err)

	var decoded CodeLocation
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, loc.FilePath, decoded.FilePath)
	assert.Equal(t, loc.LineNumber, decoded.LineNumber)
	assert.Equal(t, loc.FunctionName, decoded.FunctionName)
}

func TestCodeLocation_WithoutOptionalFields(t *testing.T) {
	loc := &CodeLocation{
		FilePath: "tests/login.spec.ts",
	}

	data, err := json.Marshal(loc)
	require.NoError(t, err)

	var decoded CodeLocation
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "tests/login.spec.ts", decoded.FilePath)
	assert.Equal(t, 0, decoded.LineNumber)
	assert.Equal(t, "", decoded.FunctionName)
}

func TestEvidence_AllTypes(t *testing.T) {
	tests := []struct {
		evidenceType string
		content      string
	}{
		{EvidenceScreenshot, "Screenshot shows error dialog"},
		{EvidenceTrace, "Trace shows network timeout at 5000ms"},
		{EvidenceLog, "Error: Connection refused"},
		{EvidenceNetwork, "HTTP 500 from /api/login"},
		{EvidenceCode, "Line 45: await page.click('#btn')"},
	}

	for _, tc := range tests {
		t.Run(tc.evidenceType, func(t *testing.T) {
			ev := Evidence{Type: tc.evidenceType, Content: tc.content}
			data, err := json.Marshal(ev)
			require.NoError(t, err)

			var decoded Evidence
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			assert.Equal(t, tc.evidenceType, decoded.Type)
			assert.Equal(t, tc.content, decoded.Content)
		})
	}
}

func TestAnalysisResult_WithRCA(t *testing.T) {
	result := &AnalysisResult{
		Category:    CategoryDiagnosis,
		Confidence:  85,
		Sensitivity: "low",
		RCA: &RootCauseAnalysis{
			Title:       "Element Interception",
			FailureType: FailureTypeTimeout,
			Location: &CodeLocation{
				FilePath:   "tests/checkout.spec.ts",
				LineNumber: 45,
			},
			Symptom:     "Timeout waiting for '#submit-btn'",
			RootCause:   "Cookie banner blocks element",
			Evidence:    []Evidence{{Type: EvidenceScreenshot, Content: "Overlay visible"}},
			Remediation: "Dismiss cookie banner first",
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded AnalysisResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, CategoryDiagnosis, decoded.Category)
	assert.Equal(t, 85, decoded.Confidence)
	require.NotNil(t, decoded.RCA)
	assert.Equal(t, "Element Interception", decoded.RCA.Title)
	assert.Equal(t, FailureTypeTimeout, decoded.RCA.FailureType)
}

func TestAnalysisResult_WithoutRCA_LegacyText(t *testing.T) {
	// For backward compatibility: category != diagnosis should not have RCA
	result := &AnalysisResult{
		Text:     "All tests passing",
		Category: CategoryNoFailures,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded AnalysisResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, CategoryNoFailures, decoded.Category)
	assert.Equal(t, "All tests passing", decoded.Text)
	assert.Nil(t, decoded.RCA)
}

func TestFailureType_Constants(t *testing.T) {
	// Verify all failure type constants are defined
	assert.Equal(t, "timeout", FailureTypeTimeout)
	assert.Equal(t, "assertion", FailureTypeAssertion)
	assert.Equal(t, "network", FailureTypeNetwork)
	assert.Equal(t, "infra", FailureTypeInfra)
	assert.Equal(t, "flake", FailureTypeFlake)
}

func TestEvidenceType_Constants(t *testing.T) {
	// Verify all evidence type constants are defined
	assert.Equal(t, "screenshot", EvidenceScreenshot)
	assert.Equal(t, "trace", EvidenceTrace)
	assert.Equal(t, "log", EvidenceLog)
	assert.Equal(t, "network", EvidenceNetwork)
	assert.Equal(t, "code", EvidenceCode)
}

func TestParseRCAFromArgs_Complete(t *testing.T) {
	args := map[string]any{
		"title":        "Timeout Error",
		"failure_type": "timeout",
		"file_path":    "tests/login.spec.ts",
		"line_number":  float64(42),
		"symptom":      "Test timed out",
		"root_cause":   "Slow network",
		"evidence": []any{
			map[string]any{"type": "trace", "content": "Network delay"},
		},
		"remediation": "Add retry logic",
	}

	rca := ParseRCAFromArgs(args)

	require.NotNil(t, rca)
	assert.Equal(t, "Timeout Error", rca.Title)
	assert.Equal(t, FailureTypeTimeout, rca.FailureType)
	require.NotNil(t, rca.Location)
	assert.Equal(t, "tests/login.spec.ts", rca.Location.FilePath)
	assert.Equal(t, 42, rca.Location.LineNumber)
	assert.Equal(t, "Test timed out", rca.Symptom)
	assert.Equal(t, "Slow network", rca.RootCause)
	require.Len(t, rca.Evidence, 1)
	assert.Equal(t, EvidenceTrace, rca.Evidence[0].Type)
	assert.Equal(t, "Add retry logic", rca.Remediation)
}

func TestParseRCAFromArgs_Minimal(t *testing.T) {
	args := map[string]any{
		"title":        "Error",
		"failure_type": "assertion",
		"symptom":      "Failed",
		"root_cause":   "Bug",
		"remediation":  "Fix it",
	}

	rca := ParseRCAFromArgs(args)

	require.NotNil(t, rca)
	assert.Equal(t, "Error", rca.Title)
	assert.Nil(t, rca.Location)
	assert.Empty(t, rca.Evidence)
}

func TestParseRCAFromArgs_InvalidFailureType(t *testing.T) {
	args := map[string]any{
		"title":        "Error",
		"failure_type": "unknown_type",
		"symptom":      "Failed",
		"root_cause":   "Bug",
		"remediation":  "Fix it",
	}

	rca := ParseRCAFromArgs(args)

	require.NotNil(t, rca)
	// Invalid type should default to empty (model can provide any value, we don't reject)
	assert.Equal(t, "unknown_type", rca.FailureType)
}

func TestParseRCAFromArgs_MissingRequiredFields(t *testing.T) {
	args := map[string]any{
		"title": "Only title",
	}

	rca := ParseRCAFromArgs(args)

	// Should still parse, just with empty fields
	require.NotNil(t, rca)
	assert.Equal(t, "Only title", rca.Title)
	assert.Equal(t, "", rca.Symptom)
}

func TestParseRCAFromArgs_EmptyArgs(t *testing.T) {
	rca := ParseRCAFromArgs(map[string]any{})

	// Empty RCA with all defaults
	require.NotNil(t, rca)
	assert.Equal(t, "", rca.Title)
}

func TestParseRCAFromArgs_NilArgs(t *testing.T) {
	rca := ParseRCAFromArgs(nil)

	// Should handle nil gracefully
	require.NotNil(t, rca)
}

func TestRCA_FormatForCLI(t *testing.T) {
	rca := &RootCauseAnalysis{
		Title:       "Timeout waiting for Submit Button",
		FailureType: FailureTypeTimeout,
		Location: &CodeLocation{
			FilePath:   "tests/checkout.spec.ts",
			LineNumber: 45,
		},
		Symptom:     "Test timed out after 30000ms",
		RootCause:   "Cookie banner overlays submit button",
		Evidence:    []Evidence{{Type: EvidenceScreenshot, Content: "Overlay at z=999"}},
		Remediation: "Add cookie banner dismiss step",
	}

	formatted := rca.FormatForCLI()

	assert.Contains(t, formatted, "TIMEOUT")
	assert.Contains(t, formatted, "tests/checkout.spec.ts:45")
	assert.Contains(t, formatted, "ROOT CAUSE")
	assert.Contains(t, formatted, "Cookie banner overlays submit button")
	assert.Contains(t, formatted, "EVIDENCE")
	assert.Contains(t, formatted, "Overlay at z=999")
	assert.Contains(t, formatted, "FIX")
	assert.Contains(t, formatted, "Add cookie banner dismiss step")
}

func TestRCA_FormatForCLI_NoLocation(t *testing.T) {
	rca := &RootCauseAnalysis{
		Title:       "Network Error",
		FailureType: FailureTypeNetwork,
		Symptom:     "Request failed",
		RootCause:   "Server down",
		Evidence:    []Evidence{},
		Remediation: "Check server status",
	}

	formatted := rca.FormatForCLI()

	assert.Contains(t, formatted, "NETWORK")
	assert.NotContains(t, formatted, ":0") // Should not show line 0
}
