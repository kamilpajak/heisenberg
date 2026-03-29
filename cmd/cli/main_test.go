package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fatih/color"
	"github.com/kamilpajak/heisenberg/pkg/llm"
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
	assert.Contains(t, stderr.String(), "Heisenberg")
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

func TestPrintStructuredRCA_ProductionBug_HighConfidence(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:                 "Price calculation broken",
		FailureType:           llm.FailureTypeAssertion,
		Location:              &llm.CodeLocation{FilePath: "tests/checkout.spec.ts", LineNumber: 45},
		BugLocation:           llm.BugLocationProduction,
		BugLocationConfidence: "high",
		BugCodeLocation:       &llm.CodeLocation{FilePath: "src/pricing.ts", LineNumber: 42},
		RootCause:             "Pricing returns zero",
		Remediation:           "Fix calculatePrice()",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "ASSERTION")
	assert.Contains(t, out, "tests/checkout.spec.ts:45")
	assert.Contains(t, out, "[production bug]")
	assert.Contains(t, out, "src/pricing.ts:42")
}

func TestPrintStructuredRCA_ProductionBug_LowConfidence(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:                 "Possible regression",
		FailureType:           llm.FailureTypeAssertion,
		Location:              &llm.CodeLocation{FilePath: "tests/checkout.spec.ts", LineNumber: 45},
		BugLocation:           llm.BugLocationProduction,
		BugLocationConfidence: "low",
		RootCause:             "Maybe a regression",
		Remediation:           "Investigate",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "[production bug?]")
	assert.NotContains(t, out, "[production bug]  ") // no exact match without ?
}

func TestPrintStructuredRCA_TestBug_NoTag(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Wrong assertion",
		FailureType: llm.FailureTypeAssertion,
		BugLocation: llm.BugLocationTest,
		RootCause:   "Test has wrong expected value",
		Remediation: "Update test",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.NotContains(t, out, "[production")
	assert.NotContains(t, out, "[infrastructure")
	assert.NotContains(t, out, "[test") // test is default — no tag
}

func TestPrintStructuredRCA_InfraBug(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:                 "DB not seeded",
		FailureType:           llm.FailureTypeInfra,
		BugLocation:           llm.BugLocationInfrastructure,
		BugLocationConfidence: "high",
		RootCause:             "CI database empty",
		Remediation:           "Fix seed step",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "[infrastructure]")
}

func TestPrintStructuredRCA_UnknownBugLocation_NoTag(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Unclear failure",
		FailureType: llm.FailureTypeAssertion,
		BugLocation: llm.BugLocationUnknown,
		RootCause:   "Not enough evidence",
		Remediation: "Investigate",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.NotContains(t, out, "[production")
	assert.NotContains(t, out, "[infrastructure")
}

func TestPrintResult_MultipleRCAs(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  90,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "utilsBundle not loaded",
				FailureType: llm.FailureTypeAssertion,
				BugLocation: llm.BugLocationTest,
				Location:    &llm.CodeLocation{FilePath: "tests/perf.spec.ts", LineNumber: 29},
				RootCause:   "Test assertion outdated",
				Remediation: "Update test",
			},
			{
				Title:       "File not found",
				FailureType: llm.FailureTypeInfra,
				BugLocation: llm.BugLocationInfrastructure,
				Location:    &llm.CodeLocation{FilePath: "registry/index.spec.ts"},
				RootCause:   "Temp dir not created",
				Remediation: "Fix CI setup",
			},
		},
	}

	printResult(&stderr, &stdout, r)
	out := stdout.String()

	// Summary list
	assert.Contains(t, out, "2 root causes found")
	assert.Contains(t, out, "1.")
	assert.Contains(t, out, "2.")
	assert.Contains(t, out, "ASSERTION")
	assert.Contains(t, out, "INFRA")
	// Detail sections
	assert.Contains(t, out, "Root Cause")
	assert.Contains(t, out, "Test assertion outdated")
	assert.Contains(t, out, "Temp dir not created")
}

func TestPrintResult_SingleRCA_NoSummaryList(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  90,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "Timeout waiting for Submit Button",
				FailureType: llm.FailureTypeTimeout,
				Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts", LineNumber: 45},
				RootCause:   "Cookie banner overlays submit button",
				Remediation: "Dismiss cookie banner",
			},
		},
	}

	printResult(&stderr, &stdout, r)
	out := stdout.String()

	// No summary list for single RCA
	assert.NotContains(t, out, "root causes found")
	// But detail section present
	assert.Contains(t, out, "TIMEOUT")
	assert.Contains(t, out, "tests/checkout.spec.ts:45")
	assert.Contains(t, out, "Root Cause")
}

func TestPrintResult_WithStructuredRCA(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  90,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
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
		},
	}

	printResult(&stderr, &stdout, r)

	out := stdout.String()
	assert.Contains(t, out, "TIMEOUT")
	assert.Contains(t, out, "tests/checkout.spec.ts:45")
	assert.Contains(t, out, "Root Cause")
	assert.Contains(t, out, "Cookie banner overlays submit button")
	assert.Contains(t, out, "Evidence")
	assert.Contains(t, out, "[Screenshot]")
	assert.Contains(t, out, "Overlay visible")
	assert.Contains(t, out, "Fix")
	assert.Contains(t, out, "Dismiss cookie banner")
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		indent   string
		want     string
	}{
		{
			name:     "short text no wrap",
			input:    "hello world",
			maxWidth: 40,
			indent:   "  ",
			want:     "  hello world",
		},
		{
			name:     "wraps at word boundary",
			input:    "the quick brown fox jumps over the lazy dog",
			maxWidth: 25,
			indent:   "  ",
			want:     "  the quick brown fox\n  jumps over the lazy dog",
		},
		{
			name:     "empty string",
			input:    "",
			maxWidth: 40,
			indent:   "  ",
			want:     "",
		},
		{
			name:     "single long word",
			input:    "supercalifragilisticexpialidocious",
			maxWidth: 20,
			indent:   "  ",
			want:     "  supercalifragilisticexpialidocious",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.input, tt.maxWidth, tt.indent)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWrapBullets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "numbered item continuation aligns past marker",
			input: "1. This is a long numbered item that should wrap with continuation aligned past the marker",
			want:  "  1. This is a long numbered item that should\n     wrap with continuation aligned past the marker",
		},
		{
			name:  "bullet item continuation aligns past marker",
			input: "- This is a long bullet item that should wrap with continuation aligned past the dash",
			want:  "  - This is a long bullet item that should wrap\n    with continuation aligned past the dash",
		},
		{
			name:  "plain paragraph uses base indent",
			input: "This is a plain paragraph without any bullet marker that wraps normally here",
			want:  "  This is a plain paragraph without any bullet\n  marker that wraps normally here",
		},
		{
			name:  "multiple paragraphs",
			input: "1. First item\n2. Second item",
			want:  "  1. First item\n  2. Second item",
		},
		{
			name:  "multi-digit numbered item",
			input: "10. This is a long multi-digit numbered item that wraps properly",
			want:  "  10. This is a long multi-digit numbered item\n      that wraps properly",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapBullets(tt.input, 52, "  ")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPrintStructuredRCA_WordWrap(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Timeout Error",
		FailureType: llm.FailureTypeTimeout,
		Location:    &llm.CodeLocation{FilePath: "test.spec.ts", LineNumber: 1},
		Symptom:     "Timeout",
		RootCause:   "This is a very long root cause description that should definitely be wrapped across multiple lines because it exceeds the maximum line width of seventy-six characters",
		Evidence:    []llm.Evidence{},
		Remediation: "This is a very long remediation that should also be wrapped across multiple lines to ensure readability on standard terminal widths of eighty columns",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	// Verify content is present
	assert.Contains(t, out, "very long root cause")
	assert.Contains(t, out, "very long remediation")

	// Verify wrapping occurred — no visible line should exceed 80 runes
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	for _, line := range lines {
		visible := bytes.TrimSpace(line)
		if len(visible) > 0 {
			runeCount := utf8.RuneCount(visible)
			assert.LessOrEqual(t, runeCount, 80, "line too long (%d runes): %q", runeCount, string(line))
		}
	}
}

func TestPrintResult_FallbackToText(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Text:        "Legacy text analysis",
		Category:    llm.CategoryDiagnosis,
		Confidence:  75,
		Sensitivity: "medium",
		RCAs:        nil, // No structured RCA
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
		RCAs:        []llm.RootCauseAnalysis{{Title: ""}}, // Empty title = fallback
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
		RCAs: []llm.RootCauseAnalysis{
			{
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
		},
	}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(r)
	require.NoError(t, err)

	var decoded llm.AnalysisResult
	err = json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	require.Len(t, decoded.RCAs, 1)
	assert.Equal(t, "Timeout Error", decoded.RCAs[0].Title)
	assert.Equal(t, llm.FailureTypeTimeout, decoded.RCAs[0].FailureType)
	assert.Equal(t, "tests/login.spec.ts", decoded.RCAs[0].Location.FilePath)
	assert.Equal(t, 42, decoded.RCAs[0].Location.LineNumber)
	assert.Len(t, decoded.RCAs[0].Evidence, 1)
}

func TestJSONOutput_MultiRCA(t *testing.T) {
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  90,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "Timeout in checkout",
				FailureType: llm.FailureTypeTimeout,
				Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts", LineNumber: 45},
				RootCause:   "Cookie banner",
				Remediation: "Dismiss banner",
			},
			{
				Title:       "Assertion in login",
				FailureType: llm.FailureTypeAssertion,
				Location:    &llm.CodeLocation{FilePath: "tests/login.spec.ts", LineNumber: 12},
				RootCause:   "Changed redirect",
				Remediation: "Update URL",
			},
		},
	}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(r)
	require.NoError(t, err)

	// Verify JSON key is "analyses" not "rca"
	raw := buf.String()
	assert.Contains(t, raw, `"analyses"`)
	assert.NotContains(t, raw, `"rca"`)

	var decoded llm.AnalysisResult
	err = json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	require.Len(t, decoded.RCAs, 2)
	assert.Equal(t, "Timeout in checkout", decoded.RCAs[0].Title)
	assert.Equal(t, "Assertion in login", decoded.RCAs[1].Title)
}

func TestPrintResult_ThreeRCAs_Numbering(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  85,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "Timeout in checkout",
				FailureType: llm.FailureTypeTimeout,
				BugLocation: llm.BugLocationTest,
				Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts", LineNumber: 45},
				RootCause:   "Cookie banner",
				Remediation: "Dismiss banner",
			},
			{
				Title:       "Assertion in login",
				FailureType: llm.FailureTypeAssertion,
				BugLocation: llm.BugLocationProduction,
				Location:    &llm.CodeLocation{FilePath: "tests/login.spec.ts", LineNumber: 12},
				RootCause:   "Changed redirect",
				Remediation: "Update URL",
			},
			{
				Title:       "Network error in API",
				FailureType: llm.FailureTypeNetwork,
				BugLocation: llm.BugLocationInfrastructure,
				Location:    &llm.CodeLocation{FilePath: "tests/api.spec.ts"},
				RootCause:   "DNS failure",
				Remediation: "Fix DNS",
			},
		},
	}

	printResult(&stderr, &stdout, r)
	out := stdout.String()

	// Summary header
	assert.Contains(t, out, "3 root causes found")

	// Cluster cards
	assert.Contains(t, out, "Cluster 1/3")
	assert.Contains(t, out, "Cluster 2/3")
	assert.Contains(t, out, "Cluster 3/3")

	// All failure types present
	assert.Contains(t, out, "TIMEOUT")
	assert.Contains(t, out, "ASSERTION")
	assert.Contains(t, out, "NETWORK")

	// All detail sections present
	assert.Contains(t, out, "Cookie banner")
	assert.Contains(t, out, "Changed redirect")
	assert.Contains(t, out, "DNS failure")
}

func TestParseRunURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"https://github.com/org/repo/actions/runs/12345", "org", "repo", false},
		{"https://github.com/microsoft/playwright/actions/runs/23642131867", "microsoft", "playwright", false},
		{"https://github.com/bad-url", "", "", true},
		{"not-a-url", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			runID = 0 // reset global
			owner, repo, err := parseRunURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantOwner, owner)
				assert.Equal(t, tt.wantRepo, repo)
				assert.Positive(t, runID)
			}
		})
	}
}

func TestResolveRepo_FromArgs(t *testing.T) {
	fromEnv = false
	runURL = ""
	owner, repo, err := resolveRepo([]string{"org/repo"})
	require.NoError(t, err)
	assert.Equal(t, "org", owner)
	assert.Equal(t, "repo", repo)
}

func TestResolveRepo_InvalidArgs(t *testing.T) {
	fromEnv = false
	runURL = ""
	_, _, err := resolveRepo([]string{"invalid"})
	assert.Error(t, err)
}

func TestResolveRepo_NoArgs(t *testing.T) {
	fromEnv = false
	runURL = ""
	_, _, err := resolveRepo(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provide owner/repo")
}

func TestResolveRepo_FromEnv(t *testing.T) {
	fromEnv = true
	runURL = ""
	runID = 0
	t.Setenv("GITHUB_REPOSITORY", "org/repo")
	t.Setenv("GITHUB_RUN_ID", "99999")

	owner, repo, err := resolveRepo(nil)
	require.NoError(t, err)
	assert.Equal(t, "org", owner)
	assert.Equal(t, "repo", repo)
	assert.Equal(t, int64(99999), runID)

	fromEnv = false // cleanup
}

func TestResolveRepo_FromURL(t *testing.T) {
	fromEnv = false
	runURL = "https://github.com/microsoft/playwright/actions/runs/12345"
	runID = 0

	owner, repo, err := resolveRepo(nil)
	require.NoError(t, err)
	assert.Equal(t, "microsoft", owner)
	assert.Equal(t, "playwright", repo)
	assert.Equal(t, int64(12345), runID)

	runURL = "" // cleanup
}

func TestPrintRunHeader(t *testing.T) {
	var buf bytes.Buffer
	r := &llm.AnalysisResult{
		Owner:    "org",
		Repo:     "repo",
		RunID:    12345,
		Branch:   "main",
		Event:    "pull_request",
		Category: llm.CategoryDiagnosis,
	}
	printRunHeader(&buf, r)
	out := buf.String()

	assert.Contains(t, out, "Heisenberg — org/repo #12345")
	assert.Contains(t, out, "Branch: main")
	assert.Contains(t, out, "Event: pull_request")
	assert.Contains(t, out, "━")
}

func TestPrintRunHeader_Minimal(t *testing.T) {
	var buf bytes.Buffer
	r := &llm.AnalysisResult{Category: llm.CategoryNoFailures}
	printRunHeader(&buf, r)
	out := buf.String()

	assert.Contains(t, out, "Heisenberg")
	assert.NotContains(t, out, "—")
}

func TestBugLocationLabel(t *testing.T) {
	// Just verify non-empty returns
	assert.NotEmpty(t, bugLocationLabel(llm.BugLocationProduction))
	assert.NotEmpty(t, bugLocationLabel(llm.BugLocationInfrastructure))
	assert.NotEmpty(t, bugLocationLabel(llm.BugLocationTest))
	assert.NotEmpty(t, bugLocationLabel(llm.BugLocationUnknown))
}

func TestPrintClusterSummary(t *testing.T) {
	var buf bytes.Buffer
	rcas := []llm.RootCauseAnalysis{
		{Title: "Timeout", FailureType: llm.FailureTypeTimeout, BugLocation: llm.BugLocationInfrastructure,
			Location: &llm.CodeLocation{FilePath: "test.spec.ts", LineNumber: 42}},
		{Title: "Assertion", FailureType: llm.FailureTypeAssertion, BugLocation: llm.BugLocationTest},
	}
	printClusterSummary(&buf, rcas)
	out := buf.String()

	assert.Contains(t, out, "2 root causes found")
	assert.Contains(t, out, "TIMEOUT")
	assert.Contains(t, out, "test.spec.ts:42")
}

func TestPrintClusterSummary_EmptyFailureType(t *testing.T) {
	var buf bytes.Buffer
	rcas := []llm.RootCauseAnalysis{
		{Title: "Unknown", BugLocation: llm.BugLocationUnknown},
	}
	printClusterSummary(&buf, rcas)
	assert.Contains(t, buf.String(), "ERROR")
}

func TestPrintClusterCard(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:                 "DB timeout",
		FailureType:           llm.FailureTypeTimeout,
		BugLocation:           llm.BugLocationInfrastructure,
		BugLocationConfidence: "high",
		RootCause:             "DB not started",
		Remediation:           "Start DB",
	}
	printClusterCard(&buf, rca, 2, 3)
	out := buf.String()

	assert.Contains(t, out, "Cluster 2/3")
	assert.Contains(t, out, "high confidence")
	assert.Contains(t, out, "DB not started")
}

func TestIsTerminal_NotTTY(t *testing.T) {
	f, err := os.CreateTemp("", "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	defer f.Close()
	assert.False(t, isTerminal(f))
}

func TestResolveRepo_FromEnvMissingRepo(t *testing.T) {
	fromEnv = true
	runURL = ""
	t.Setenv("GITHUB_REPOSITORY", "")
	_, _, err := resolveRepo(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_REPOSITORY not set")
	fromEnv = false
}

func TestExitCode_APIError(t *testing.T) {
	var buf bytes.Buffer
	apiErr := &llm.APIError{
		StatusCode: 429,
		Status:     "429 Too Many Requests",
		Message:    "Resource has been exhausted",
		RawBody:    `{"error":{"code":429}}`,
	}
	wrapped := fmt.Errorf("step 4: %w", apiErr)

	code := printError(&buf, wrapped)

	out := buf.String()
	assert.Equal(t, exitAPIError, code)
	assert.Contains(t, out, "Error:")
	assert.Contains(t, out, "429 Too Many Requests")
	assert.Contains(t, out, "Hint:")
	assert.Contains(t, out, "Exit code: 3  (external API error)")
	assert.NotContains(t, out, `"error"`, "raw JSON should not appear without --verbose")
}

func TestExitCode_ConfigError(t *testing.T) {
	var buf bytes.Buffer
	code := printError(&buf, &llm.ConfigError{Message: "GOOGLE_API_KEY environment variable required"})

	out := buf.String()
	assert.Equal(t, exitConfigError, code)
	assert.Contains(t, out, "GOOGLE_API_KEY")
	assert.Contains(t, out, "Exit code: 4  (configuration error)")
}

func TestExitCode_GenericError(t *testing.T) {
	var buf bytes.Buffer
	code := printError(&buf, fmt.Errorf("something went wrong"))

	out := buf.String()
	assert.Equal(t, exitGeneral, code)
	assert.Contains(t, out, "Error:")
	assert.Contains(t, out, "something went wrong")
	assert.Contains(t, out, "Exit code: 1  (runtime error)")
	assert.NotContains(t, out, "Hint:")
}

func TestExitCodeLabels_AllDefined(t *testing.T) {
	for _, code := range []int{exitGeneral, exitAPIError, exitConfigError} {
		label := exitCodeLabel[code]
		assert.NotEmpty(t, label, "exitCodeLabel missing for code %d", code)
	}
}

// Integration tests — verify full output for each UX state (emitter + printError combined)

func TestIntegration_State4_ErrorFlow(t *testing.T) {
	var stderr bytes.Buffer

	// Simulate: emitter shows progress, then error occurs
	emitter := llm.NewTextEmitter(&stderr, false)
	emitter.Emit(llm.ProgressEvent{Type: "info", Message: "Analyzing run 123 for owner/repo..."})
	emitter.Emit(llm.ProgressEvent{Type: "tool", Step: 2, MaxStep: 30, Tool: "get_job_logs"})
	emitter.MarkFailed()
	emitter.Close()

	// Then printError renders the error
	apiErr := &llm.APIError{
		StatusCode: 429,
		Status:     "429 Too Many Requests",
		Message:    "Resource has been exhausted",
		Retries:    3,
	}
	code := printError(&stderr, apiErr)

	out := stderr.String()

	// Verify order: info → close summary → error → hint → exit code
	assert.Equal(t, exitAPIError, code)
	infoIdx := strings.Index(out, "Analyzing run 123")
	closeIdx := strings.Index(out, "Stopped at 2/30")
	errorIdx := strings.Index(out, "Error:")
	hintIdx := strings.Index(out, "Hint:")
	exitIdx := strings.Index(out, "Exit code: 3")

	assert.Greater(t, closeIdx, infoIdx, "close summary should appear after info")
	assert.Greater(t, errorIdx, closeIdx, "error should appear after close summary")
	assert.Greater(t, hintIdx, errorIdx, "hint should appear after error")
	assert.Greater(t, exitIdx, hintIdx, "exit code should appear last")

	// Verify content
	assert.Contains(t, out, "✗")
	assert.Contains(t, out, "Stopped at 2/30 iterations")
	assert.Contains(t, out, "429 Too Many Requests")
	assert.Contains(t, out, "Retried 3 times")
	assert.Contains(t, out, "Exit code: 3  (external API error)")
}

func TestIntegration_State1And2_ProgressFlow(t *testing.T) {
	var stderr bytes.Buffer

	emitter := llm.NewTextEmitter(&stderr, false)

	// State 1: initial
	emitter.Emit(llm.ProgressEvent{Type: "info", Message: "Analyzing run 123 for owner/repo..."})
	assert.Contains(t, stderr.String(), "Analyzing run 123")

	// State 2: tool events with aligned phases
	emitter.Emit(llm.ProgressEvent{Type: "tool", Step: 1, MaxStep: 30, Tool: "get_artifact"})
	assert.Contains(t, stderr.String(), "Fetching artifacts")
	assert.Contains(t, stderr.String(), "1/30")

	stderr.Reset()
	emitter.Emit(llm.ProgressEvent{Type: "tool", Step: 3, MaxStep: 30, Tool: "get_job_logs"})
	assert.Contains(t, stderr.String(), "Reading logs")
	assert.Contains(t, stderr.String(), "3/30")

	// Success close
	stderr.Reset()
	emitter.Close()
	assert.Contains(t, stderr.String(), "✓")
	assert.Contains(t, stderr.String(), "Used 3/30 iterations")
}

func TestIntegration_State3_SuccessFlow(t *testing.T) {
	t.Skip("TODO: executive summary — full success output integration test")

	var stderr, stdout bytes.Buffer

	emitter := llm.NewTextEmitter(&stderr, false)
	emitter.Emit(llm.ProgressEvent{Type: "info", Message: "Analyzing run 123 for owner/repo..."})
	emitter.Emit(llm.ProgressEvent{Type: "tool", Step: 9, MaxStep: 30, Tool: "done"})
	emitter.Close()

	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  95,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "Flaky Test Detected",
				FailureType: llm.FailureTypeFlake,
				Location:    &llm.CodeLocation{FilePath: "tests/perf.spec.ts", LineNumber: 29},
				RootCause:   "utilsBundle loads too fast on macOS runners",
				Remediation: "Relax assertion in perf.spec.ts",
			},
		},
	}
	printResult(&stderr, &stdout, r)

	combined := stderr.String() + stdout.String()

	// Verify order: info → close → result → cause → fix
	assert.Contains(t, combined, "✓")
	assert.Contains(t, combined, "Used 9/30 iterations")
	assert.Contains(t, combined, "Result:")
	assert.Contains(t, combined, "FLAKE")
	assert.Contains(t, combined, "95%")
	assert.Contains(t, combined, "tests/perf.spec.ts:29")
}

// State 3: Executive summary — future work
func TestPrintResult_ExecutiveSummary(t *testing.T) {
	t.Skip("TODO: executive summary — one-line Result: header above structured RCA")

	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  95,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "Flaky Test Detected",
				FailureType: llm.FailureTypeFlake,
				Location:    &llm.CodeLocation{FilePath: "tests/perf.spec.ts", LineNumber: 29},
				Symptom:     "utilsBundle not in output",
				RootCause:   "utilsBundle loads too fast on macOS runners",
				Evidence:    []llm.Evidence{{Type: llm.EvidenceTrace, Content: "Trace data"}},
				Remediation: "Relax assertion in perf.spec.ts",
			},
		},
	}

	printResult(&stderr, &stdout, r)

	out := stdout.String()
	// One-line summary above detailed sections
	assert.Contains(t, out, "Result:")
	assert.Contains(t, out, "FLAKE")
	assert.Contains(t, out, "95%")
	assert.Contains(t, out, "Test:")
	assert.Contains(t, out, "tests/perf.spec.ts:29")
}

func TestPrintResult_ExecutiveSummary_SectionNames(t *testing.T) {
	t.Skip("TODO: executive summary — rename sections to Cause/Fix/Details")

	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  90,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "Timeout Error",
				FailureType: llm.FailureTypeTimeout,
				Location:    &llm.CodeLocation{FilePath: "test.spec.ts", LineNumber: 1},
				Symptom:     "Timeout",
				RootCause:   "Slow network",
				Remediation: "Add retry",
			},
		},
	}

	printResult(&stderr, &stdout, r)

	out := stdout.String()
	assert.Contains(t, out, "Cause")
	assert.Contains(t, out, "Fix")
	assert.NotContains(t, out, "Root Cause", "should use shorter 'Cause' heading")
}
