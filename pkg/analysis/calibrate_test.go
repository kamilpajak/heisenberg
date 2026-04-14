package analysis

import (
	"context"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/ci"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
)

const testPricingFile = "src/pricing.ts"

func TestApplyConfidenceCaps_ProductionBugNoDiffIntersection(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 95, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocationIsCode: true,
		DiffIntersection:  false,
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 39, result.Confidence, "should cap at 39 when production bug has no diff overlap")
	assert.Equal(t, 95, result.OriginalConfidence)
	assert.Contains(t, result.CalibrationReason, "no_diff_intersection")
}

func TestApplyConfidenceCaps_HighBlastRadiusCodeBlame(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 90, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BlastRadius:       0.8,
		BugLocationIsCode: true,
		DiffIntersection:  true, // even with intersection, blast radius overrides
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 39, result.Confidence, "should cap at 39 when >50% jobs fail and code blamed")
	assert.Equal(t, 90, result.OriginalConfidence)
}

func TestApplyConfidenceCaps_NetworkErrorsCodeBlame(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 85, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		HasNetworkErrors:  true,
		AllSameErrorType:  true,
		BugLocationIsCode: true,
		DiffIntersection:  true,
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 49, result.Confidence, "should cap at 49 for uniform network errors blamed on code")
	assert.Equal(t, 85, result.OriginalConfidence)
}

func TestApplyConfidenceCaps_LowBugLocationConfidence(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 80, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocConfLow:    true,
		DiffIntersection: true,
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 49, result.Confidence, "should cap at 49 when LLM is uncertain about bug location")
}

func TestApplyConfidenceCaps_NoCapNeeded(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 90, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocationIsCode: true,
		DiffIntersection:  true, // diff overlaps — legitimate
		BlastRadius:       0.1,  // low blast radius
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 90, result.Confidence, "should not cap when signals are clean")
	assert.Equal(t, 0, result.OriginalConfidence, "should not set original when no cap")
}

func TestApplyConfidenceCaps_AlreadyBelowCap(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 30, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocationIsCode: true,
		DiffIntersection:  false,
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 30, result.Confidence, "should not change confidence already below cap")
	assert.Equal(t, 0, result.OriginalConfidence, "should not set original when no cap applied")
}

func TestApplyConfidenceCaps_MultipleRulesPicksLowest(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 95, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocationIsCode: true,
		DiffIntersection:  false, // → cap 39
		BlastRadius:       0.8,   // → cap 39
		HasNetworkErrors:  true,  // → cap 49
		AllSameErrorType:  true,
		BugLocConfLow:     true, // → cap 49
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 39, result.Confidence, "should apply lowest cap from all rules")
}

// --- computeSignals tests ---

type mockDiffProvider struct {
	files []ci.ChangedFile
	err   error
}

func (m *mockDiffProvider) GetChangedFiles(_ context.Context, _ ci.ChangeRef) ([]ci.ChangedFile, error) {
	return m.files, m.err
}

func TestComputeSignals_BlastRadius(t *testing.T) {
	jobs := []ci.Job{
		{Conclusion: ci.ConclusionFailure},
		{Conclusion: ci.ConclusionFailure},
		{Conclusion: ci.ConclusionSuccess},
		{Conclusion: ci.ConclusionSuccess},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{FailureType: "assertion", BugLocation: llm.BugLocationProduction},
		},
	}

	signals := computeSignals(context.Background(), result, nil, jobs, &ci.Run{})
	assert.InDelta(t, 0.5, signals.BlastRadius, 0.01, "2 of 4 jobs failed = 50%")
}

func TestComputeSignals_DiffIntersection(t *testing.T) {
	diff := &mockDiffProvider{
		files: []ci.ChangedFile{
			{Path: testPricingFile},
			{Path: "tests/pricing.test.ts"},
		},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				BugLocation:     llm.BugLocationProduction,
				BugCodeLocation: &llm.CodeLocation{FilePath: testPricingFile},
			},
		},
	}
	run := &ci.Run{CommitSHA: "abc123"}

	signals := computeSignals(context.Background(), result, diff, nil, run)
	assert.True(t, signals.DiffIntersection, "bug_code_file_path matches diff file")
}

func TestComputeSignals_NoDiffIntersection(t *testing.T) {
	diff := &mockDiffProvider{
		files: []ci.ChangedFile{
			{Path: "src/checkout.ts"},
		},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				BugLocation:     llm.BugLocationProduction,
				BugCodeLocation: &llm.CodeLocation{FilePath: testPricingFile},
			},
		},
	}
	run := &ci.Run{CommitSHA: "abc123"}

	signals := computeSignals(context.Background(), result, diff, nil, run)
	assert.False(t, signals.DiffIntersection, "bug_code_file_path does NOT match any diff file")
}

func TestComputeSignals_DiffIntersectionViaTestLocation(t *testing.T) {
	diff := &mockDiffProvider{
		files: []ci.ChangedFile{
			{Path: "tests/checkout.spec.ts"},
		},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts"},
				BugLocation: llm.BugLocationProduction,
			},
		},
	}
	run := &ci.Run{CommitSHA: "abc123"}

	signals := computeSignals(context.Background(), result, diff, nil, run)
	assert.True(t, signals.DiffIntersection, "test location matches diff file")
}

func TestComputeSignals_NoDiffProvider(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{BugLocation: llm.BugLocationProduction},
		},
	}

	// nil provider = can't check diff, assume intersection (don't penalize)
	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.DiffIntersection, "nil provider should assume intersection")
}

func TestComputeSignals_AllSameErrorType(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{FailureType: "network"},
			{FailureType: "network"},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.AllSameErrorType)
	assert.True(t, signals.HasNetworkErrors)
}

func TestComputeSignals_MixedErrorTypes(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{FailureType: "network"},
			{FailureType: "assertion"},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.False(t, signals.AllSameErrorType)
	assert.True(t, signals.HasNetworkErrors)
}

func TestComputeSignals_BugLocationFlags(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				BugLocation:           llm.BugLocationProduction,
				BugLocationConfidence: "low",
			},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.BugLocationIsCode)
	assert.True(t, signals.BugLocConfLow)
}

func TestApplyConfidenceCaps_EvidenceContradicts(t *testing.T) {
	// LLM classified as assertion/production but evidence mentions HTTP errors
	result := &llm.AnalysisResult{Confidence: 95, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocationIsCode:      true,
		DiffIntersection:       true,  // diff overlaps — looks legit
		HasHiddenInfraEvidence: true,  // but evidence mentions HTTP 400!
		HasNetworkErrors:       false, // failure_type is assertion, not network
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 49, result.Confidence, "should cap when evidence contradicts classification")
	assert.Equal(t, "evidence_contradicts_classification", result.CalibrationReason)
}

func TestComputeSignals_HiddenInfraInSymptom(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				FailureType: "assertion",
				BugLocation: llm.BugLocationProduction,
				Symptom:     "Tests fail with NoSuchElementException. Browser console shows HTTP 400 Bad Request.",
				RootCause:   "TypeError in component rendering",
			},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.HasHiddenInfraEvidence, "HTTP 400 in symptom should be detected")
}

func TestComputeSignals_HiddenInfraInEvidence(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				FailureType: "assertion",
				BugLocation: llm.BugLocationProduction,
				RootCause:   "TypeError in component",
				Evidence: []llm.Evidence{
					{Type: "log", Content: "Browser console: GET /api/search returned 503 Service Unavailable"},
				},
			},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.HasHiddenInfraEvidence, "HTTP 503 in evidence should be detected")
}

func TestComputeSignals_HiddenInfraConnectionRefused(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				FailureType: "assertion",
				BugLocation: llm.BugLocationProduction,
				Symptom:     "UI does not load, ECONNREFUSED on port 3000",
			},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.HasHiddenInfraEvidence)
}

func TestComputeSignals_HiddenInfraAPIFailure(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				FailureType: "assertion",
				BugLocation: llm.BugLocationProduction,
				Symptom:     "Welcome page does not load",
				RootCause:   "API failure causes frontend to crash on undefined data",
			},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.HasHiddenInfraEvidence, "'API failure' in root cause should trigger")
}

func TestApplyConfidenceCaps_EvidenceContradicts_ButDiffTouchesErrorHandling(t *testing.T) {
	// LLM evidence mentions HTTP 500 but PR actually changed error handling code.
	// This is a legitimate code bug — Rule 5 should NOT fire.
	result := &llm.AnalysisResult{Confidence: 95, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocationIsCode:      true,
		DiffIntersection:       true,
		HasHiddenInfraEvidence: true,
		HasNetworkErrors:       false,
		DiffTouchesErrorPaths:  true, // PR changed API/error handling code
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 95, result.Confidence, "should not cap when diff touches error handling code")
}

func TestComputeSignals_DiffTouchesErrorPaths(t *testing.T) {
	diff := &mockDiffProvider{
		files: []ci.ChangedFile{
			{Path: "src/api/error-handler.ts"},
			{Path: "src/components/view.ts"},
		},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{BugLocation: llm.BugLocationProduction},
		},
	}

	signals := computeSignals(context.Background(), result, diff, nil, &ci.Run{CommitSHA: "abc"})
	assert.True(t, signals.DiffTouchesErrorPaths, "error-handler.ts should trigger")
}

func TestComputeSignals_DiffNoErrorPaths(t *testing.T) {
	diff := &mockDiffProvider{
		files: []ci.ChangedFile{
			{Path: "src/components/header.tsx"},
			{Path: "src/styles/main.css"},
		},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{BugLocation: llm.BugLocationProduction},
		},
	}

	signals := computeSignals(context.Background(), result, diff, nil, &ci.Run{CommitSHA: "abc"})
	assert.False(t, signals.DiffTouchesErrorPaths)
}

func TestComputeSignals_NoHiddenInfra(t *testing.T) {
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{
				FailureType: "assertion",
				BugLocation: llm.BugLocationProduction,
				Symptom:     "expect(price).toBe('$10.00') received '$0.00'",
				RootCause:   "Pricing logic returns zero for valid inputs",
			},
		},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.False(t, signals.HasHiddenInfraEvidence, "pure assertion without HTTP errors")
}

func TestApplyConfidenceCaps_InfrastructureBugLocationNotCapped(t *testing.T) {
	// When LLM correctly identifies infrastructure — no cap should apply
	result := &llm.AnalysisResult{Confidence: 95, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		BugLocationIsCode: false, // LLM said "infrastructure" — correct
		BlastRadius:       0.8,
		HasNetworkErrors:  true,
		AllSameErrorType:  true,
		DiffIntersection:  false,
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 95, result.Confidence, "should not cap when LLM correctly identified infra")
}

func TestApplyConfidenceCaps_LowIterationCap(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 100, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		LowIterations: true,
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, capAmbiguous, result.Confidence, "should cap at 49 when model used very few iterations")
	assert.Contains(t, result.CalibrationReason, "low_iteration")
}

func TestApplyConfidenceCaps_LowIterationNotCappedWhenAlreadyBelow(t *testing.T) {
	result := &llm.AnalysisResult{Confidence: 30, Category: llm.CategoryDiagnosis}
	signals := CalibrationSignals{
		LowIterations: true,
	}

	applyConfidenceCaps(result, signals)
	assert.Equal(t, 30, result.Confidence, "should not change when already below cap")
}

func TestComputeSignals_LowIterations(t *testing.T) {
	result := &llm.AnalysisResult{
		Category: llm.CategoryDiagnosis,
		Eval:     &llm.EvalMeta{Iterations: 3},
		RCAs: []llm.RootCauseAnalysis{{
			FailureType: llm.FailureTypeAssertion,
			BugLocation: llm.BugLocationProduction,
		}},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.True(t, signals.LowIterations, "iterations=3 should flag as low")
}

func TestComputeSignals_NormalIterations(t *testing.T) {
	result := &llm.AnalysisResult{
		Category: llm.CategoryDiagnosis,
		Eval:     &llm.EvalMeta{Iterations: 5},
		RCAs: []llm.RootCauseAnalysis{{
			FailureType: llm.FailureTypeAssertion,
			BugLocation: llm.BugLocationProduction,
		}},
	}

	signals := computeSignals(context.Background(), result, nil, nil, &ci.Run{})
	assert.False(t, signals.LowIterations, "iterations=5 should not flag as low")
}
