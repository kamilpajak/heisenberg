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
	LowIterations          bool    `json:"low_iterations"`
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

// capRule holds a confidence ceiling and the reason for applying it.
type capRule struct {
	ceiling int
	reason  string
}

// applyConfidenceCaps enforces hard confidence ceilings when the LLM's
// structured output contradicts deterministic pipeline signals.
func applyConfidenceCaps(result *llm.AnalysisResult, s CalibrationSignals) {
	rules := evaluateRules(s)

	best := capRule{ceiling: 100}
	for _, r := range rules {
		if r.ceiling < best.ceiling {
			best = r
		}
	}

	if result.Confidence > best.ceiling {
		result.OriginalConfidence = result.Confidence
		result.CalibrationReason = best.reason
		result.Confidence = best.ceiling
	}
}

// evaluateRules returns all cap rules that fire for the given signals.
func evaluateRules(s CalibrationSignals) []capRule {
	var rules []capRule

	// Rule 1: Claims production bug but diff doesn't touch the blamed files.
	if s.BugLocationIsCode && !s.DiffIntersection {
		rules = append(rules, capRule{capUncertain, "production_bug_no_diff_intersection"})
	}

	// Rule 2: >50% of jobs failed — almost certainly systemic, not localized.
	if s.BlastRadius > 0.5 && s.BugLocationIsCode {
		rules = append(rules, capRule{capUncertain, "high_blast_radius_code_blame"})
	}

	// Rule 3: All RCAs are network errors blamed on code.
	if s.HasNetworkErrors && s.AllSameErrorType && s.BugLocationIsCode {
		rules = append(rules, capRule{capAmbiguous, "uniform_network_errors_code_blame"})
	}

	// Rule 5: Evidence mentions HTTP/API errors but classified as code bug.
	// Exception: skipped when PR diff touches error handling code.
	if s.BugLocationIsCode && s.HasHiddenInfraEvidence && !s.HasNetworkErrors && !s.DiffTouchesErrorPaths {
		rules = append(rules, capRule{capAmbiguous, "evidence_contradicts_classification"})
	}

	// Rule 4: LLM itself uncertain about bug location.
	if s.BugLocConfLow {
		rules = append(rules, capRule{capAmbiguous, "low_bug_location_confidence"})
	}

	// Rule 6: Model used very few iterations — likely guessed without thorough investigation.
	if s.LowIterations {
		rules = append(rules, capRule{capAmbiguous, "low_iteration_count"})
	}

	return rules
}

// computeSignals extracts deterministic calibration signals from the analysis
// result, PR diff, and job data.
func computeSignals(ctx context.Context, result *llm.AnalysisResult,
	diff diffProvider, jobs []ci.Job, run *ci.Run) CalibrationSignals {

	s := CalibrationSignals{
		DiffIntersection: true, // assume intersection unless proven otherwise
	}

	s.BlastRadius = computeBlastRadius(jobs)
	extractRCASignals(&s, result.RCAs)

	// Low iteration count suggests model guessed without thorough investigation.
	const lowIterThreshold = 3
	if result.Eval != nil && result.Eval.Iterations <= lowIterThreshold {
		s.LowIterations = true
	}

	if s.BugLocationIsCode && !s.HasNetworkErrors {
		s.HasHiddenInfraEvidence = anyRCAContainsInfraEvidence(result.RCAs)
	}

	if diff != nil && run != nil && s.BugLocationIsCode {
		files, err := diff.GetChangedFiles(ctx, ci.ChangeRef{HeadSHA: run.CommitSHA})
		if err == nil && len(files) > 0 {
			s.DiffIntersection = checkDiffIntersectionFromFiles(result, files)
			s.DiffTouchesErrorPaths = checkErrorPaths(files)
		}
	}

	return s
}

// computeBlastRadius returns the fraction of jobs that failed.
func computeBlastRadius(jobs []ci.Job) float64 {
	if len(jobs) == 0 {
		return 0
	}
	failed := 0
	for _, j := range jobs {
		if j.Conclusion == ci.ConclusionFailure {
			failed++
		}
	}
	return float64(failed) / float64(len(jobs))
}

// extractRCASignals populates RCA-level flags from the analyses array.
func extractRCASignals(s *CalibrationSignals, rcas []llm.RootCauseAnalysis) {
	failureTypes := make(map[string]struct{}, len(rcas))
	for _, rca := range rcas {
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
	s.AllSameErrorType = len(failureTypes) == 1 && len(rcas) > 0
}

// anyRCAContainsInfraEvidence checks if any RCA's text mentions infrastructure
// signals (HTTP errors, connection failures) that the LLM didn't classify.
func anyRCAContainsInfraEvidence(rcas []llm.RootCauseAnalysis) bool {
	for _, rca := range rcas {
		if containsInfraEvidence(rca) {
			return true
		}
	}
	return false
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
