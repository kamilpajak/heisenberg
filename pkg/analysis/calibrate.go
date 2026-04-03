package analysis

import (
	"context"
	"regexp"
	"strings"

	"github.com/kamilpajak/heisenberg/pkg/ci"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// infraEvidenceRe matches HTTP error codes, connection errors, and API failure
// patterns in free text. Used to detect infrastructure signals the LLM saw
// but didn't factor into its classification.
var infraEvidenceRe = regexp.MustCompile(`(?i)(` +
	`HTTP\s*[45]\d\d|` + // HTTP 400, HTTP 503, etc.
	`\b[45]\d\d\s+(Bad Request|Unauthorized|Forbidden|Not Found|Internal Server Error|Bad Gateway|Service Unavailable)|` +
	`ECONNREFUSED|ERR_CONNECTION|connection refused|` +
	`API\s+(error|failure|failed)|` +
	`service\s+unavailable|gateway\s+timeout` +
	`)`)

// diffProvider abstracts the ability to fetch changed files for a PR/commit.
// Satisfied by ci.Provider but allows testing with mocks.
type diffProvider interface {
	GetChangedFiles(ctx context.Context, ref ci.ChangeRef) ([]ci.ChangedFile, error)
}

// CalibrationSignals contains deterministic signals for confidence adjustment.
// Exported with JSON tags to enable logging as training data for future
// data-driven calibration (#50).
type CalibrationSignals struct {
	BlastRadius            float64 `json:"blast_radius"`
	DiffIntersection       bool    `json:"diff_intersection"`
	AllSameErrorType       bool    `json:"all_same_error_type"`
	HasNetworkErrors       bool    `json:"has_network_errors"`
	BugLocationIsCode      bool    `json:"bug_location_is_code"`
	BugLocConfLow          bool    `json:"bug_loc_conf_low"`
	HasHiddenInfraEvidence bool    `json:"has_hidden_infra_evidence"`
	DiffTouchesErrorPaths  bool    `json:"diff_touches_error_paths"`
}

// calibrateResult adjusts confidence based on heuristic signals that the LLM
// cannot self-assess: diff-fault intersection, blast radius, and internal
// consistency of the diagnosis.
func calibrateResult(ctx context.Context, result *llm.AnalysisResult,
	provider diffProvider, jobs []ci.Job, run *ci.Run) {

	if result == nil || result.Category != llm.CategoryDiagnosis {
		return
	}

	signals := computeSignals(ctx, result, provider, jobs, run)
	applyConfidenceCaps(result, signals)
}

// Confidence cap thresholds aligned with the LLM rubric:
//
//	80-100: clear root cause with strong evidence
//	40-79:  likely cause but some ambiguity
//	0-39:   uncertain
const (
	capUncertain = 39 // "uncertain" tier
	capAmbiguous = 49 // low end of "likely" tier
)

// applyConfidenceCaps enforces hard confidence ceilings when the LLM's
// structured output contradicts deterministic pipeline signals.
func applyConfidenceCaps(result *llm.AnalysisResult, s CalibrationSignals) {
	cap := 100
	reason := ""

	// Rule 1: Claims production bug but diff doesn't touch the blamed files.
	// The LLM is correlating symptoms with unrelated code changes.
	if s.BugLocationIsCode && !s.DiffIntersection {
		if capUncertain < cap {
			cap = capUncertain
			reason = "production_bug_no_diff_intersection"
		}
	}

	// Rule 2: >50% of jobs failed with same pattern — almost certainly systemic,
	// not a localized code regression.
	if s.BlastRadius > 0.5 && s.BugLocationIsCode {
		if capUncertain < cap {
			cap = capUncertain
			reason = "high_blast_radius_code_blame"
		}
	}

	// Rule 3: All RCAs are network errors blamed on code — strong infrastructure signal.
	if s.HasNetworkErrors && s.AllSameErrorType && s.BugLocationIsCode {
		if capAmbiguous < cap {
			cap = capAmbiguous
			reason = "uniform_network_errors_code_blame"
		}
	}

	// Rule 5: LLM classified as code bug but its own evidence mentions HTTP/network
	// errors — it's confusing a proximal UI crash with an underlying API failure.
	// Exception: if the PR diff touches error handling / API code, the HTTP error
	// may be a legitimate code bug (the PR broke the error handler).
	if s.BugLocationIsCode && s.HasHiddenInfraEvidence && !s.HasNetworkErrors && !s.DiffTouchesErrorPaths {
		if capAmbiguous < cap {
			cap = capAmbiguous
			reason = "evidence_contradicts_classification"
		}
	}

	// Rule 4: LLM itself is uncertain about bug location — can't be 80+ confident
	// about the diagnosis if you don't know where the bug lives.
	if s.BugLocConfLow {
		if capAmbiguous < cap {
			cap = capAmbiguous
			reason = "low_bug_location_confidence"
		}
	}

	if result.Confidence > cap {
		result.OriginalConfidence = result.Confidence
		result.CalibrationReason = reason
		result.Confidence = cap
	}
}

// computeSignals extracts deterministic calibration signals from the analysis
// result, PR diff, and job data.
func computeSignals(ctx context.Context, result *llm.AnalysisResult,
	diff diffProvider, jobs []ci.Job, run *ci.Run) CalibrationSignals {

	s := CalibrationSignals{
		DiffIntersection: true, // assume intersection unless proven otherwise
	}

	// Blast radius: fraction of jobs that failed
	if len(jobs) > 0 {
		failed := 0
		for _, j := range jobs {
			if j.Conclusion == ci.ConclusionFailure {
				failed++
			}
		}
		s.BlastRadius = float64(failed) / float64(len(jobs))
	}

	// RCA-level signals
	failureTypes := map[string]struct{}{}
	for _, rca := range result.RCAs {
		if rca.BugLocation == llm.BugLocationProduction {
			s.BugLocationIsCode = true
		}
		if strings.EqualFold(rca.BugLocationConfidence, "low") {
			s.BugLocConfLow = true
		}
		if rca.FailureType != "" {
			failureTypes[rca.FailureType] = struct{}{}
		}
		if rca.FailureType == llm.FailureTypeNetwork {
			s.HasNetworkErrors = true
		}
	}
	s.AllSameErrorType = len(failureTypes) == 1 && len(result.RCAs) > 0

	// Hidden infra evidence: LLM's own text mentions HTTP/network errors
	// but failure_type is not "network" — cognitive dissonance.
	if s.BugLocationIsCode && !s.HasNetworkErrors {
		for _, rca := range result.RCAs {
			if containsInfraEvidence(rca) {
				s.HasHiddenInfraEvidence = true
				break
			}
		}
	}

	// Diff-fault intersection and error path detection
	if diff != nil && run != nil && s.BugLocationIsCode {
		files, err := diff.GetChangedFiles(ctx, ci.ChangeRef{HeadSHA: run.CommitSHA})
		if err == nil && len(files) > 0 {
			s.DiffIntersection = checkDiffIntersectionFromFiles(result, files)
			s.DiffTouchesErrorPaths = checkErrorPaths(files)
		}
	}

	return s
}

// errorPathRe matches file paths likely related to error handling or API client code.
var errorPathRe = regexp.MustCompile(`(?i)(error[_-]?handler|api[_-]?client|http[_-]?client|interceptor|middleware|error[_-]?boundary|exception|retry|fallback)`)

// containsInfraEvidence checks if an RCA's text mentions HTTP errors or
// connection failures that suggest an infrastructure issue the LLM didn't
// classify as such.
func containsInfraEvidence(rca llm.RootCauseAnalysis) bool {
	if infraEvidenceRe.MatchString(rca.Symptom) || infraEvidenceRe.MatchString(rca.RootCause) {
		return true
	}
	for _, ev := range rca.Evidence {
		if infraEvidenceRe.MatchString(ev.Content) {
			return true
		}
	}
	return false
}

// checkDiffIntersectionFromFiles checks if any RCA file paths appear in the
// changed files list.
func checkDiffIntersectionFromFiles(result *llm.AnalysisResult, files []ci.ChangedFile) bool {
	diffPaths := make(map[string]struct{}, len(files))
	for _, f := range files {
		diffPaths[f.Path] = struct{}{}
	}

	for _, rca := range result.RCAs {
		if rca.Location != nil && rca.Location.FilePath != "" {
			if _, ok := diffPaths[rca.Location.FilePath]; ok {
				return true
			}
		}
		if rca.BugCodeLocation != nil && rca.BugCodeLocation.FilePath != "" {
			if _, ok := diffPaths[rca.BugCodeLocation.FilePath]; ok {
				return true
			}
		}
	}

	return false
}

// checkErrorPaths returns true if any changed file matches common error
// handling or API client patterns.
func checkErrorPaths(files []ci.ChangedFile) bool {
	for _, f := range files {
		if errorPathRe.MatchString(f.Path) {
			return true
		}
	}
	return false
}
