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
	"github.com/kamilpajak/heisenberg/pkg/azure"
	"github.com/kamilpajak/heisenberg/pkg/config"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testRepoE2ETests       = "e2e-tests"
	testRepoOrgRepo        = "org/repo"
	testAzureDevOrgURL     = "https://dev.azure.com/myorg/"
	testModelFromConfig    = "from-config"
	testModelGemini3Pro    = "gemini-3-pro"
	testSpecFile           = "test.spec.ts"
	testCheckoutSpec       = "tests/checkout.spec.ts"
	testCheckoutSpecLine45 = "tests/checkout.spec.ts:45"
	testPerfSpec           = "tests/perf.spec.ts"
	testLoginSpec          = "tests/login.spec.ts"
	testRootCauseHeading   = "Root Cause"
	testCookieBannerMsg    = "Cookie banner overlays submit button"
	testTimeoutError       = "Timeout Error"
	testTimeoutInCheckout  = "Timeout in checkout"
	testCookieBanner       = "Cookie banner"
	testAssertionInLogin   = "Assertion in login"
	testChangedRedirect    = "Changed redirect"
	testStatus429          = "429 Too Many Requests"
	testErrorPrefix        = "Error:"
	testAnalyzingMsg       = "Analyzing run 123 for owner/repo..."
	testRootCauseTimeout   = "Root cause: timeout in login"
)

func init() {
	color.NoColor = true
}

func TestPrintResult_Diagnosis(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Text:        testRootCauseTimeout,
		Category:    llm.CategoryDiagnosis,
		Confidence:  85,
		Sensitivity: "low",
	}

	printResult(&stderr, &stdout, r)

	assert.Contains(t, stderr.String(), "━")
	assert.Contains(t, stderr.String(), "Confidence: 85%")
	assert.Contains(t, stderr.String(), "low sensitivity")
	assert.Contains(t, stdout.String(), testRootCauseTimeout)
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
		Text:        testRootCauseTimeout,
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
		Location:              &llm.CodeLocation{FilePath: testCheckoutSpec, LineNumber: 45},
		BugLocation:           llm.BugLocationProduction,
		BugLocationConfidence: "high",
		BugCodeLocation:       &llm.CodeLocation{FilePath: "src/pricing.ts", LineNumber: 42},
		RootCause:             "Pricing returns zero",
		Remediation:           "Fix calculatePrice()",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "ASSERTION")
	assert.Contains(t, out, testCheckoutSpecLine45)
	assert.Contains(t, out, "[production bug]")
	assert.Contains(t, out, "src/pricing.ts:42")
}

func TestPrintStructuredRCA_ProductionBug_LowConfidence(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:                 "Possible regression",
		FailureType:           llm.FailureTypeAssertion,
		Location:              &llm.CodeLocation{FilePath: testCheckoutSpec, LineNumber: 45},
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
				Location:    &llm.CodeLocation{FilePath: testPerfSpec, LineNumber: 29},
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
	assert.Contains(t, out, testRootCauseHeading)
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
				Location:    &llm.CodeLocation{FilePath: testCheckoutSpec, LineNumber: 45},
				RootCause:   testCookieBannerMsg,
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
	assert.Contains(t, out, testCheckoutSpecLine45)
	assert.Contains(t, out, testRootCauseHeading)
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
					FilePath:   testCheckoutSpec,
					LineNumber: 45,
				},
				Symptom:   "Test timed out after 30000ms",
				RootCause: testCookieBannerMsg,
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
	assert.Contains(t, out, testCheckoutSpecLine45)
	assert.Contains(t, out, testRootCauseHeading)
	assert.Contains(t, out, testCookieBannerMsg)
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

func TestPrintStructuredRCA_FixConfidenceHigh(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:         "Selector changed",
		FailureType:   llm.FailureTypeTimeout,
		RootCause:     "CSS selector outdated",
		Remediation:   "Update selector to [data-loaded]",
		FixConfidence: "high",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "Fix (high confidence)")
}

func TestPrintStructuredRCA_FixConfidenceLow(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:         "Rate limit test",
		FailureType:   llm.FailureTypeAssertion,
		RootCause:     "Limits mismatch",
		Remediation:   "Mock maxRequests",
		FixConfidence: "low",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "Fix (suggested direction)")
}

func TestPrintStructuredRCA_FixConfidenceMedium(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:         "Config mismatch",
		FailureType:   llm.FailureTypeAssertion,
		RootCause:     "Rate limits differ",
		Remediation:   "Adjust rate limiter config",
		FixConfidence: "medium",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "Fix (medium confidence)")
}

func TestPrintStructuredRCA_NoFixConfidence(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Some failure",
		FailureType: llm.FailureTypeAssertion,
		RootCause:   "Something broke",
		Remediation: "Fix it",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "  Fix\n")
	assert.NotContains(t, out, "confidence")
	assert.NotContains(t, out, "direction")
}

func TestPrintStructuredRCA_WordWrap(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       testTimeoutError,
		FailureType: llm.FailureTypeTimeout,
		Location:    &llm.CodeLocation{FilePath: testSpecFile, LineNumber: 1},
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
				Title:       testTimeoutError,
				FailureType: llm.FailureTypeTimeout,
				Location: &llm.CodeLocation{
					FilePath:   testLoginSpec,
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
	assert.Equal(t, testTimeoutError, decoded.RCAs[0].Title)
	assert.Equal(t, llm.FailureTypeTimeout, decoded.RCAs[0].FailureType)
	assert.Equal(t, testLoginSpec, decoded.RCAs[0].Location.FilePath)
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
				Title:       testTimeoutInCheckout,
				FailureType: llm.FailureTypeTimeout,
				Location:    &llm.CodeLocation{FilePath: testCheckoutSpec, LineNumber: 45},
				RootCause:   testCookieBanner,
				Remediation: "Dismiss banner",
			},
			{
				Title:       testAssertionInLogin,
				FailureType: llm.FailureTypeAssertion,
				Location:    &llm.CodeLocation{FilePath: testLoginSpec, LineNumber: 12},
				RootCause:   testChangedRedirect,
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
	assert.Equal(t, testTimeoutInCheckout, decoded.RCAs[0].Title)
	assert.Equal(t, testAssertionInLogin, decoded.RCAs[1].Title)
}

func TestPrintResult_ThreeRCAs_Numbering(t *testing.T) {
	var stderr, stdout bytes.Buffer
	r := &llm.AnalysisResult{
		Category:    llm.CategoryDiagnosis,
		Confidence:  85,
		Sensitivity: "low",
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       testTimeoutInCheckout,
				FailureType: llm.FailureTypeTimeout,
				BugLocation: llm.BugLocationTest,
				Location:    &llm.CodeLocation{FilePath: testCheckoutSpec, LineNumber: 45},
				RootCause:   testCookieBanner,
				Remediation: "Dismiss banner",
			},
			{
				Title:       testAssertionInLogin,
				FailureType: llm.FailureTypeAssertion,
				BugLocation: llm.BugLocationProduction,
				Location:    &llm.CodeLocation{FilePath: testLoginSpec, LineNumber: 12},
				RootCause:   testChangedRedirect,
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
	assert.Contains(t, out, testCookieBanner)
	assert.Contains(t, out, testChangedRedirect)
	assert.Contains(t, out, "DNS failure")
}

func TestParseRunURL_GitHub(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantRunID int64
		wantErr   bool
	}{
		{"https://github.com/org/repo/actions/runs/12345", "org", "repo", 12345, false},
		{"https://github.com/microsoft/playwright/actions/runs/23642131867", "microsoft", "playwright", 23642131867, false},
		{"https://github.com/bad-url", "", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			target, err := parseRunURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "github", target.provider)
				assert.Equal(t, tt.wantOwner, target.owner)
				assert.Equal(t, tt.wantRepo, target.repo)
				assert.Equal(t, tt.wantRunID, target.runID)
			}
		})
	}
}

func TestParseRunURL_Azure(t *testing.T) {
	target, err := parseRunURL("https://dev.azure.com/myorg/myproject/_build/results?buildId=456")
	require.NoError(t, err)
	assert.Equal(t, "azure", target.provider)
	assert.Equal(t, "myorg", target.owner)
	assert.Equal(t, "myproject", target.repo)
	assert.Equal(t, int64(456), target.runID)
}

func TestParseRunURL_Azure_ExtraParams(t *testing.T) {
	target, err := parseRunURL("https://dev.azure.com/myorg/myproject/_build/results?view=results&buildId=789&tab=tests")
	require.NoError(t, err)
	assert.Equal(t, "azure", target.provider)
	assert.Equal(t, int64(789), target.runID)
}

func TestParseRunURL_Azure_VisualStudio(t *testing.T) {
	target, err := parseRunURL("https://myorg.visualstudio.com/myproject/_build/results?buildId=321")
	require.NoError(t, err)
	assert.Equal(t, "azure", target.provider)
	assert.Equal(t, "myorg", target.owner)
	assert.Equal(t, "myproject", target.repo)
	assert.Equal(t, int64(321), target.runID)
}

func TestParseRunURL_Invalid(t *testing.T) {
	_, err := parseRunURL("https://gitlab.com/org/repo/pipelines/123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognized CI URL")
}

func TestResolveTarget_FromArgs(t *testing.T) {
	fromEnv = false
	runURL = ""
	providerFlag = ""
	target, err := resolveTarget([]string{testRepoOrgRepo}, 0)
	require.NoError(t, err)
	assert.Equal(t, "github", target.provider)
	assert.Equal(t, "org", target.owner)
	assert.Equal(t, "repo", target.repo)
}

func TestResolveTarget_InvalidArgs(t *testing.T) {
	fromEnv = false
	runURL = ""
	providerFlag = ""
	_, err := resolveTarget([]string{"invalid"}, 0)
	assert.Error(t, err)
}

func TestResolveTarget_NoArgs(t *testing.T) {
	fromEnv = false
	runURL = ""
	providerFlag = ""
	azureOrg = ""
	azureProject = ""
	_, err := resolveTarget(nil, runID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provide owner/repo")
}

func TestResolveTarget_FromGitHubEnv(t *testing.T) {
	fromEnv = true
	runURL = ""
	runID = 0
	providerFlag = ""
	t.Setenv("GITHUB_REPOSITORY", testRepoOrgRepo)
	t.Setenv("GITHUB_RUN_ID", "99999")
	t.Setenv("SYSTEM_TEAMPROJECT", "")

	target, err := resolveTarget(nil, runID)
	require.NoError(t, err)
	assert.Equal(t, "github", target.provider)
	assert.Equal(t, "org", target.owner)
	assert.Equal(t, "repo", target.repo)
	assert.Equal(t, int64(99999), target.runID)

	fromEnv = false
}

func TestResolveTarget_FromAzureEnv(t *testing.T) {
	fromEnv = true
	runURL = ""
	runID = 0
	providerFlag = ""
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("SYSTEM_TEAMFOUNDATIONCOLLECTIONURI", testAzureDevOrgURL)
	t.Setenv("SYSTEM_TEAMPROJECT", "myproject")
	t.Setenv("BUILD_BUILDID", "789")

	target, err := resolveTarget(nil, runID)
	require.NoError(t, err)
	assert.Equal(t, "azure", target.provider)
	assert.Equal(t, "myorg", target.owner)
	assert.Equal(t, "myproject", target.repo)
	assert.Equal(t, int64(789), target.runID)

	fromEnv = false
}

func TestResolveTarget_AmbiguousEnv(t *testing.T) {
	fromEnv = true
	runURL = ""
	providerFlag = ""
	t.Setenv("GITHUB_REPOSITORY", testRepoOrgRepo)
	t.Setenv("SYSTEM_TEAMPROJECT", "myproject")

	_, err := resolveTarget(nil, runID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")

	fromEnv = false
}

func TestResolveTarget_AmbiguousEnv_Resolved(t *testing.T) {
	fromEnv = true
	runURL = ""
	runID = 0
	providerFlag = "azure"
	t.Setenv("GITHUB_REPOSITORY", testRepoOrgRepo)
	t.Setenv("SYSTEM_TEAMFOUNDATIONCOLLECTIONURI", testAzureDevOrgURL)
	t.Setenv("SYSTEM_TEAMPROJECT", "myproject")
	t.Setenv("BUILD_BUILDID", "456")

	target, err := resolveTarget(nil, runID)
	require.NoError(t, err)
	assert.Equal(t, "azure", target.provider)
	assert.Equal(t, "myorg", target.owner)

	fromEnv = false
	providerFlag = ""
}

func TestResolveTarget_AzureFlags(t *testing.T) {
	fromEnv = false
	runURL = ""
	providerFlag = "azure"
	azureOrg = "myorg"
	azureProject = "myproject"
	runID = 100

	target, err := resolveTarget(nil, runID)
	require.NoError(t, err)
	assert.Equal(t, "azure", target.provider)
	assert.Equal(t, "myorg", target.owner)
	assert.Equal(t, "myproject", target.repo)
	assert.Equal(t, int64(100), target.runID)

	providerFlag = ""
	azureOrg = ""
	azureProject = ""
	runID = 0
}

func TestResolveTarget_AzureFlags_MissingOrg(t *testing.T) {
	fromEnv = false
	runURL = ""
	providerFlag = "azure"
	azureOrg = ""
	azureProject = "myproject"

	_, err := resolveTarget(nil, runID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--azure-org")

	providerFlag = ""
	azureProject = ""
}

func TestResolveTarget_FromURL(t *testing.T) {
	fromEnv = false
	runURL = "https://github.com/microsoft/playwright/actions/runs/12345"
	runID = 0

	target, err := resolveTarget(nil, runID)
	require.NoError(t, err)
	assert.Equal(t, "github", target.provider)
	assert.Equal(t, "microsoft", target.owner)
	assert.Equal(t, "playwright", target.repo)
	assert.Equal(t, int64(12345), target.runID)

	runURL = ""
}

func TestExtractAzureOrg(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{testAzureDevOrgURL, "myorg"},
		{"https://dev.azure.com/myorg", "myorg"},
		{"https://dev.azure.com/my-org/", "my-org"},
		{"https://myorg.visualstudio.com/", "myorg"},
		{"https://myorg.visualstudio.com", "myorg"},
		{"https://my-org.visualstudio.com/DefaultCollection", "my-org"},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			assert.Equal(t, tt.want, extractAzureOrg(tt.uri))
		})
	}
}

func TestBuildProvider_GitHub(t *testing.T) {
	target := &targetInfo{provider: "github", owner: "org", repo: "repo"}
	p := buildProvider(target, &config.Config{GitHubToken: "ghp_test"})
	assert.Equal(t, "github", p.Name())
}

func TestBuildProvider_Azure(t *testing.T) {
	target := &targetInfo{provider: "azure", owner: "myorg", repo: "myproject"}
	p := buildProvider(target, &config.Config{AzureDevOpsPAT: "pat_test"})
	assert.Equal(t, "azure", p.Name())
}

func TestBuildProvider_AzureFromEnv(t *testing.T) {
	t.Setenv("AZURE_DEVOPS_PAT", "env-pat")
	target := &targetInfo{provider: "azure", owner: "myorg", repo: "myproject"}
	p := buildProvider(target, &config.Config{})
	assert.Equal(t, "azure", p.Name())
}

func TestBuildProvider_AzureWithTestRepo(t *testing.T) {
	target := &targetInfo{provider: "azure", owner: "myorg", repo: "myproject"}
	testRepo = testRepoE2ETests
	defer func() { testRepo = "" }()

	p := buildProvider(target, &config.Config{AzureDevOpsPAT: "pat"})
	azClient := p.(*azure.Client)
	require.Len(t, azClient.ExtraRepos, 1)
	// Single name: project and repo should BOTH be the test repo name, not the org
	assert.Equal(t, testRepoE2ETests, azClient.ExtraRepos[0].Project)
	assert.Equal(t, testRepoE2ETests, azClient.ExtraRepos[0].Repo)
}

func TestBuildProvider_AzureWithTestRepoExplicitProject(t *testing.T) {
	target := &targetInfo{provider: "azure", owner: "myorg", repo: "myproject"}
	testRepo = "other-project/test-repo"
	defer func() { testRepo = "" }()

	p := buildProvider(target, &config.Config{AzureDevOpsPAT: "pat"})
	azClient := p.(*azure.Client)
	require.Len(t, azClient.ExtraRepos, 1)
	assert.Equal(t, "other-project", azClient.ExtraRepos[0].Project)
	assert.Equal(t, "test-repo", azClient.ExtraRepos[0].Repo)
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
			Location: &llm.CodeLocation{FilePath: testSpecFile, LineNumber: 42}},
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

func TestResolveTarget_FromEnvMissingRepo(t *testing.T) {
	fromEnv = true
	runURL = ""
	providerFlag = ""
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("SYSTEM_TEAMPROJECT", "")
	_, err := resolveTarget(nil, runID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_REPOSITORY not set")
	fromEnv = false
}

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name     string
		flag     string
		jsonFlag bool
		isTTY    bool
		want     string
	}{
		{"json flag", "", true, true, "json"},
		{"format flag json", "json", false, true, "json"},
		{"format flag human", "human", false, false, "human"},
		{"TTY auto-detect", "", false, true, "human"},
		{"pipe auto-detect", "", false, false, "json"},
		{"json flag overrides TTY", "", true, false, "json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveFormat(tt.flag, tt.jsonFlag, tt.isTTY))
		})
	}
}

func TestResolveModel(t *testing.T) {
	// flag > env > config > empty
	assert.Equal(t, "gemini-2.5-pro", resolveModel("gemini-2.5-pro", ""))
	assert.Equal(t, "", resolveModel("", ""))
	assert.Equal(t, testModelFromConfig, resolveModel("", testModelFromConfig))

	t.Setenv("HEISENBERG_MODEL", testModelGemini3Pro)
	assert.Equal(t, testModelGemini3Pro, resolveModel("", ""))
	assert.Equal(t, testModelGemini3Pro, resolveModel("", testModelFromConfig)) // env beats config
	assert.Equal(t, "override", resolveModel("override", testModelFromConfig))  // flag beats all
}

func TestExitCode_APIError(t *testing.T) {
	var buf bytes.Buffer
	apiErr := &llm.APIError{
		StatusCode: 429,
		Status:     testStatus429,
		Message:    "Resource has been exhausted",
		RawBody:    `{"error":{"code":429}}`,
	}
	wrapped := fmt.Errorf("step 4: %w", apiErr)

	code := printError(&buf, wrapped)

	out := buf.String()
	assert.Equal(t, exitAPIError, code)
	assert.Contains(t, out, testErrorPrefix)
	assert.Contains(t, out, testStatus429)
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
	assert.Contains(t, out, testErrorPrefix)
	assert.Contains(t, out, "something went wrong")
	assert.Contains(t, out, "Exit code: 1  (runtime error)")
	assert.NotContains(t, out, "Hint:")
}

func TestExitCodeLabels_AllDefined(t *testing.T) {
	for _, code := range []int{exitGeneral, exitUsage, exitAPIError, exitConfigError} {
		label := exitCodeLabel[code]
		assert.NotEmpty(t, label, "exitCodeLabel missing for code %d", code)
	}
}

func TestJSONOutput_SchemaVersion(t *testing.T) {
	r := &llm.AnalysisResult{
		Text:       "Root cause: timeout",
		Category:   llm.CategoryDiagnosis,
		Confidence: 85,
	}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(struct {
		SchemaVersion string `json:"schema_version"`
		*llm.AnalysisResult
	}{SchemaVersion: llm.SchemaV1, AnalysisResult: r})
	require.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	assert.Equal(t, "1", decoded["schema_version"])
	assert.Equal(t, "Root cause: timeout", decoded["text"])
	assert.Equal(t, "diagnosis", decoded["category"])
}

func TestPrintError_JSONFormat(t *testing.T) {
	oldFormat := format
	format = "json"
	defer func() { format = oldFormat }()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfgErr := &llm.ConfigError{Message: "GOOGLE_API_KEY environment variable required"}
	code := printError(os.Stderr, cfgErr)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	assert.Equal(t, exitConfigError, code)

	var decoded map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	assert.Equal(t, "1", decoded["schema_version"])
	assert.Contains(t, decoded["error"], "GOOGLE_API_KEY")
	assert.Equal(t, float64(exitConfigError), decoded["exit_code"])
}

// Integration tests — verify full output for each UX state (emitter + printError combined)

func TestIntegration_State4_ErrorFlow(t *testing.T) {
	var stderr bytes.Buffer

	// Simulate: emitter shows progress, then error occurs
	emitter := llm.NewTextEmitter(&stderr, false)
	emitter.Emit(llm.ProgressEvent{Type: "info", Message: testAnalyzingMsg})
	emitter.Emit(llm.ProgressEvent{Type: "tool", Step: 2, MaxStep: 30, Tool: "get_job_logs"})
	emitter.MarkFailed()
	emitter.Close()

	// Then printError renders the error
	apiErr := &llm.APIError{
		StatusCode: 429,
		Status:     testStatus429,
		Message:    "Resource has been exhausted",
		Retries:    3,
	}
	code := printError(&stderr, apiErr)

	out := stderr.String()

	// Verify order: info → close summary → error → hint → exit code
	assert.Equal(t, exitAPIError, code)
	infoIdx := strings.Index(out, "Analyzing run 123")
	closeIdx := strings.Index(out, "Stopped at 2/30")
	errorIdx := strings.Index(out, testErrorPrefix)
	hintIdx := strings.Index(out, "Hint:")
	exitIdx := strings.Index(out, "Exit code: 3")

	assert.Greater(t, closeIdx, infoIdx, "close summary should appear after info")
	assert.Greater(t, errorIdx, closeIdx, "error should appear after close summary")
	assert.Greater(t, hintIdx, errorIdx, "hint should appear after error")
	assert.Greater(t, exitIdx, hintIdx, "exit code should appear last")

	// Verify content
	assert.Contains(t, out, "✗")
	assert.Contains(t, out, "Stopped at 2/30 iterations")
	assert.Contains(t, out, testStatus429)
	assert.Contains(t, out, "Retried 3 times")
	assert.Contains(t, out, "Exit code: 3  (external API error)")
}

func TestIntegration_State1And2_ProgressFlow(t *testing.T) {
	var stderr bytes.Buffer

	emitter := llm.NewTextEmitter(&stderr, false)

	// State 1: initial
	emitter.Emit(llm.ProgressEvent{Type: "info", Message: testAnalyzingMsg})
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
	emitter.Emit(llm.ProgressEvent{Type: "info", Message: testAnalyzingMsg})
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
				Location:    &llm.CodeLocation{FilePath: testPerfSpec, LineNumber: 29},
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
				Location:    &llm.CodeLocation{FilePath: testPerfSpec, LineNumber: 29},
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
				Title:       testTimeoutError,
				FailureType: llm.FailureTypeTimeout,
				Location:    &llm.CodeLocation{FilePath: testSpecFile, LineNumber: 1},
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
	assert.NotContains(t, out, testRootCauseHeading, "should use shorter 'Cause' heading")
}

// --- Phase 1: analyze subcommand + flag relocation ---

func TestAnalyzeCmd_Exists(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"analyze"})
	require.NoError(t, err)
	assert.Equal(t, "analyze", cmd.Name())
}

func TestAnalyzeCmd_HasExamples(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"analyze"})
	require.NotNil(t, cmd)
	assert.NotEmpty(t, cmd.Example)
	assert.Contains(t, cmd.Example, "--from-env")
	assert.Contains(t, cmd.Example, "analyze owner/repo")
	assert.Contains(t, cmd.Example, "-f json")

	// --run URL example should NOT include owner/repo (URL already contains it)
	for _, line := range strings.Split(cmd.Example, "\n") {
		if strings.Contains(line, "-r ") && strings.Contains(line, "http") {
			assert.NotRegexp(t, `analyze\s+\S+/\S+\s+-r`, line,
				"--run URL example should not include owner/repo — URL already identifies the target")
		}
	}
}

func TestGlobalFlags_OnPersistentFlags(t *testing.T) {
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("verbose"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("format"))
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("debug"))
}

func TestAnalyzeCmd_OwnFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"analyze"})
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.Flags().Lookup("from-env"))
	assert.NotNil(t, cmd.Flags().Lookup("run"))
	assert.NotNil(t, cmd.Flags().Lookup("run-id"))
	assert.NotNil(t, cmd.Flags().Lookup("model"))
	assert.NotNil(t, cmd.Flags().Lookup("provider"))
}

func TestRootCmd_UseIsClean(t *testing.T) {
	assert.Equal(t, "heisenberg", rootCmd.Use)
}

func TestRootCmd_BackwardCompat_Runnable(t *testing.T) {
	assert.True(t, rootCmd.Runnable(), "rootCmd must remain runnable for backward compat")
}

func TestRootCmd_AnalyzeFlags_Hidden(t *testing.T) {
	flag := rootCmd.Flags().Lookup("from-env")
	require.NotNil(t, flag, "rootCmd must accept --from-env for backward compat")
	assert.True(t, flag.Hidden, "--from-env should be hidden on rootCmd")
}

// --- Phase 2: Rename provider-specific flags ---

func TestAnalyzeCmd_AzureFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"analyze"})
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.Flags().Lookup("azure-org"), "--azure-org should exist")
	assert.NotNil(t, cmd.Flags().Lookup("azure-project"), "--azure-project should exist")
	assert.NotNil(t, cmd.Flags().Lookup("azure-test-repo"), "--azure-test-repo should exist")
}

func TestAnalyzeCmd_DeprecatedFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"analyze"})
	require.NotNil(t, cmd)
	orgFlag := cmd.Flags().Lookup("org")
	require.NotNil(t, orgFlag, "--org should still exist as deprecated alias")
	assert.NotEmpty(t, orgFlag.Deprecated)
}

// --- Phase 3: Short flag aliases ---

func TestShortFlags(t *testing.T) {
	fmtFlag := rootCmd.PersistentFlags().ShorthandLookup("f")
	require.NotNil(t, fmtFlag, "-f should exist")
	assert.Equal(t, "format", fmtFlag.Name)

	cmd, _, _ := rootCmd.Find([]string{"analyze"})
	require.NotNil(t, cmd)
	runFlag := cmd.Flags().ShorthandLookup("r")
	require.NotNil(t, runFlag, "-r should exist")
	assert.Equal(t, "run", runFlag.Name)
}

// --- CR fixes ---

func TestRootCmd_ProviderFlagFallsThrough(t *testing.T) {
	// "heisenberg --provider azure" should NOT show help — should fall through
	// to run() which returns a proper validation error.
	providerFlag = "azure"
	azureOrg = ""
	azureProject = ""
	fromEnv = false
	runURL = ""
	defer func() { providerFlag = "" }()

	err := rootCmd.RunE(rootCmd, nil)
	// Should be an error from resolveTarget, not nil (help)
	require.Error(t, err, "should fall through to run(), not show help")
	assert.Contains(t, err.Error(), "--azure-org")
}

func TestRootCmd_AzureProjectFallsThrough(t *testing.T) {
	// "heisenberg --azure-project foo" should NOT show help
	azureProject = "foo"
	azureOrg = ""
	fromEnv = false
	runURL = ""
	providerFlag = ""
	defer func() { azureProject = "" }()

	err := rootCmd.RunE(rootCmd, nil)
	assert.Error(t, err)
}

func TestServeCmd_SilencesErrors(t *testing.T) {
	assert.True(t, serveCmd.SilenceUsage, "serveCmd should silence usage on error")
	assert.True(t, serveCmd.SilenceErrors, "serveCmd should silence errors")
}

func TestRootCmd_DeprecatedFlagsOnRoot(t *testing.T) {
	// Legacy flags on rootCmd should also be marked deprecated
	orgFlag := rootCmd.Flags().Lookup("org")
	require.NotNil(t, orgFlag)
	assert.NotEmpty(t, orgFlag.Deprecated, "--org on rootCmd should be deprecated")

	projectFlag := rootCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.NotEmpty(t, projectFlag.Deprecated, "--project on rootCmd should be deprecated")

	testRepoFlag := rootCmd.Flags().Lookup("test-repo")
	require.NotNil(t, testRepoFlag)
	assert.NotEmpty(t, testRepoFlag.Deprecated, "--test-repo on rootCmd should be deprecated")
}

func TestRun_ResolvesFormatBeforeError(t *testing.T) {
	// Pre-existing bug: resolveFormat ran AFTER resolveTarget, so if
	// resolveTarget failed, format was still "" and printError would
	// use human output even when piped (non-TTY expects JSON).
	oldFormat := format
	oldFromEnv := fromEnv
	oldRunURL := runURL
	format = ""
	fromEnv = false
	runURL = ""
	defer func() { format = oldFormat; fromEnv = oldFromEnv; runURL = oldRunURL }()

	// run() with invalid args — fails at resolveTarget
	_ = run(rootCmd, []string{"invalid"})

	// format should already be resolved (not still "")
	assert.NotEmpty(t, format, "format should be resolved before resolveTarget can fail")
}

// --- Pattern matching rendering ---

func TestPrintStructuredRCA_WithMatchedPattern(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Timeout in beforeEach",
		FailureType: llm.FailureTypeTimeout,
		Location:    &llm.CodeLocation{FilePath: testCheckoutSpec, LineNumber: 28},
		RootCause:   "Selector changed",
		Remediation: "Update selector",
		MatchedPatterns: []llm.MatchedPattern{
			{
				Name:        "playwright-beforeeach-timeout",
				Description: "Typically caused by selector/DOM changes",
				Similarity:  0.85,
				Frequency:   "Common in Playwright test suites",
			},
		},
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.Contains(t, out, "Known Pattern: playwright-beforeeach-timeout")
	assert.Contains(t, out, "Typically caused by selector/DOM changes")
	assert.Contains(t, out, "Common in Playwright test suites")
}

func TestPrintStructuredRCA_NoMatchedPattern(t *testing.T) {
	var buf bytes.Buffer
	rca := &llm.RootCauseAnalysis{
		Title:       "Assertion failed",
		FailureType: llm.FailureTypeAssertion,
		RootCause:   "Value mismatch",
		Remediation: "Fix the test",
	}

	printStructuredRCA(&buf, rca)
	out := buf.String()

	assert.NotContains(t, out, "Known Pattern")
}
