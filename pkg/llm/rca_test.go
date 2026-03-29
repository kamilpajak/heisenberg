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
		RCAs: []RootCauseAnalysis{
			{
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
		},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded AnalysisResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, CategoryDiagnosis, decoded.Category)
	assert.Equal(t, 85, decoded.Confidence)
	require.Len(t, decoded.RCAs, 1)
	assert.Equal(t, "Element Interception", decoded.RCAs[0].Title)
	assert.Equal(t, FailureTypeTimeout, decoded.RCAs[0].FailureType)
}

func TestAnalysisResult_WithoutRCA_LegacyText(t *testing.T) {
	// For backward compatibility: category != diagnosis should not have RCAs
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
	assert.Empty(t, decoded.RCAs)
}

func TestBugLocation_Constants(t *testing.T) {
	assert.Equal(t, BugLocation("test"), BugLocationTest)
	assert.Equal(t, BugLocation("production"), BugLocationProduction)
	assert.Equal(t, BugLocation("infrastructure"), BugLocationInfrastructure)
	assert.Equal(t, BugLocation("unknown"), BugLocationUnknown)
}

func TestParseRCAFromArgs_WithBugLocation(t *testing.T) {
	args := map[string]any{
		"title":                   "Price calculation broken",
		"failure_type":            "assertion",
		"file_path":               "tests/checkout.spec.ts",
		"line_number":             float64(45),
		"bug_location":            "production",
		"bug_location_confidence": "high",
		"bug_code_file_path":      "src/pricing.ts",
		"bug_code_line_number":    float64(42),
		"symptom":                 "Expected $10.00, got $0.00",
		"root_cause":              "Pricing function returns zero",
		"remediation":             "Fix calculatePrice() in pricing.ts",
	}

	rca := ParseRCAFromArgs(args)

	assert.Equal(t, BugLocationProduction, rca.BugLocation)
	assert.Equal(t, "high", rca.BugLocationConfidence)
	require.NotNil(t, rca.BugCodeLocation)
	assert.Equal(t, "src/pricing.ts", rca.BugCodeLocation.FilePath)
	assert.Equal(t, 42, rca.BugCodeLocation.LineNumber)
}

func TestParseRCAFromArgs_DefaultBugLocation(t *testing.T) {
	args := map[string]any{
		"title":        "Error",
		"failure_type": "timeout",
		"symptom":      "Timed out",
		"root_cause":   "Slow",
		"remediation":  "Fix",
	}

	rca := ParseRCAFromArgs(args)

	assert.Equal(t, BugLocation(""), rca.BugLocation)
	assert.Equal(t, "", rca.BugLocationConfidence)
	assert.Nil(t, rca.BugCodeLocation)
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

func TestParseRCAsFromArgs_MultipleAnalyses(t *testing.T) {
	args := map[string]any{
		"category":   "diagnosis",
		"confidence": float64(90),
		"analyses": []any{
			map[string]any{
				"title":        "utilsBundle not loaded",
				"failure_type": "assertion",
				"bug_location": "test",
				"file_path":    "tests/perf.spec.ts",
				"line_number":  float64(29),
				"symptom":      "Expected utilsBundle in output",
				"root_cause":   "Test assertion outdated",
				"remediation":  "Update test",
			},
			map[string]any{
				"title":        "File not found",
				"failure_type": "infra",
				"bug_location": "infrastructure",
				"file_path":    "registry/index.spec.ts",
				"symptom":      "ENOENT",
				"root_cause":   "Temp dir not created",
				"remediation":  "Fix CI setup",
			},
		},
	}

	rcas := ParseRCAsFromArgs(args)

	require.Len(t, rcas, 2)
	assert.Equal(t, "utilsBundle not loaded", rcas[0].Title)
	assert.Equal(t, FailureTypeAssertion, rcas[0].FailureType)
	assert.Equal(t, BugLocationTest, rcas[0].BugLocation)
	assert.Equal(t, "tests/perf.spec.ts", rcas[0].Location.FilePath)
	assert.Equal(t, 29, rcas[0].Location.LineNumber)

	assert.Equal(t, "File not found", rcas[1].Title)
	assert.Equal(t, FailureTypeInfra, rcas[1].FailureType)
	assert.Equal(t, BugLocationInfrastructure, rcas[1].BugLocation)
}

func TestParseRCAsFromArgs_BackwardCompat_FlatArgs(t *testing.T) {
	// Old-style flat args without analyses array → single-element slice
	args := map[string]any{
		"title":        "Timeout Error",
		"failure_type": "timeout",
		"file_path":    "tests/login.spec.ts",
		"symptom":      "Timed out",
		"root_cause":   "Slow",
		"remediation":  "Fix",
	}

	rcas := ParseRCAsFromArgs(args)

	require.Len(t, rcas, 1)
	assert.Equal(t, "Timeout Error", rcas[0].Title)
	assert.Equal(t, FailureTypeTimeout, rcas[0].FailureType)
}

func TestParseRCAsFromArgs_Empty(t *testing.T) {
	rcas := ParseRCAsFromArgs(nil)
	assert.Empty(t, rcas)
}
