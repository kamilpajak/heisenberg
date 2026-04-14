//go:build eval

package analysis_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kamilpajak/heisenberg/internal/vcr"
	"github.com/kamilpajak/heisenberg/pkg/analysis"
	"github.com/kamilpajak/heisenberg/pkg/azure"
	"github.com/kamilpajak/heisenberg/pkg/ci"
	"github.com/kamilpajak/heisenberg/pkg/github"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	labelFmt = "  %-25s"
	valueFmt = "  %-20s"
)

// groundTruth defines expected results for an eval test case.
// Separates "what actually happened" (GroundTruth) from "what the tool
// should produce" (ExpectedOutput).
type groundTruth struct {
	CaseID       string         `json:"case_id"`
	Repo         string         `json:"repo"`
	RunID        int64          `json:"run_id"`
	Provider     string         `json:"provider,omitempty"`
	AzureOrg     string         `json:"azure_org,omitempty"`
	AzureProject string         `json:"azure_project,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Truth        truthInfo      `json:"ground_truth"`
	Expected     expectedOutput `json:"expected_output"`
	Metadata     evalMetadata   `json:"metadata,omitempty"`
}

type truthInfo struct {
	ActualCause      string `json:"actual_cause"`
	ObservableByTool bool   `json:"observable_by_tool"`
	IssueURL         string `json:"issue_url,omitempty"`
}

type expectedOutput struct {
	Category          string             `json:"category"`
	ConfidenceMin     int                `json:"confidence_min"`
	ConfidenceMax     int                `json:"confidence_max"` // 0 = no upper bound
	Analyses          []expectedAnalysis `json:"analyses"`
	AnalysesCount     int                `json:"analyses_count,omitempty"` // 0 = any count
	AllowPartialMatch bool               `json:"allow_partial_match"`
}

type expectedAnalysis struct {
	FilePathContains string `json:"file_path_contains"`
	FailureType      string `json:"failure_type"`
	BugLocation      string `json:"bug_location"`
}

type evalMetadata struct {
	ValidatedDate string `json:"validated_date,omitempty"`
	OriginalModel string `json:"original_model,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

// evalEntry is one line in eval.jsonl.
type evalEntry struct {
	CaseID           string                  `json:"case_id,omitempty"`
	Timestamp        string                  `json:"timestamp"`
	Model            string                  `json:"model"`
	Repo             string                  `json:"repo"`
	RunID            int64                   `json:"run_id"`
	Category         string                  `json:"category"`
	Confidence       int                     `json:"confidence"`
	Analyses         int                     `json:"analyses_count"`
	Iterations       int                     `json:"iterations"`
	ModelMs          int                     `json:"model_ms"`
	Tokens           int                     `json:"tokens"`
	WallMs           int                     `json:"wall_ms"`
	Matched          int                     `json:"ground_truth_matched"`
	Expected         int                     `json:"ground_truth_expected"`
	ObservableByTool bool                    `json:"observable_by_tool"`
	Tags             []string                `json:"tags,omitempty"`
	RCAs             []llm.RootCauseAnalysis `json:"rca_details"`
}

// setupVCR creates a VCR recorder for the given test case. In record mode
// (HEISENBERG_EVAL_RECORD=1), real API calls are made and saved. In replay
// mode (default), cached responses are used without network calls.
func setupVCR(t *testing.T, caseName string) *http.Client {
	t.Helper()

	cassettePath := filepath.Join("..", "..", "testdata", "e2e", "cassettes", caseName)

	mode := vcr.ModeReplay
	if os.Getenv("HEISENBERG_EVAL_RECORD") == "1" {
		mode = vcr.ModeRecord
	}

	scrub := vcr.WithScrub(func(ix *vcr.Interaction) {
		ix.Request.Header.Del("Authorization")
		ix.Request.Header.Del("X-Goog-Api-Key")
		if u := ix.Request.URL; strings.Contains(u, "key=") {
			parts := strings.SplitN(u, "key=", 2)
			if len(parts) == 2 {
				end := strings.IndexByte(parts[1], '&')
				if end == -1 {
					ix.Request.URL = parts[0] + "key=REDACTED"
				} else {
					ix.Request.URL = parts[0] + "key=REDACTED" + parts[1][end:]
				}
			}
		}
	})

	r, err := vcr.New(cassettePath, mode, scrub)
	if err != nil && mode == vcr.ModeReplay {
		t.Logf("VCR: cassette not found, falling back to record: %v", err)
		r, err = vcr.New(cassettePath, vcr.ModeRecord, scrub)
	}
	require.NoError(t, err)
	t.Logf("VCR: mode=%d cassette=%s", mode, cassettePath)

	t.Cleanup(func() {
		if err := r.Stop(); err != nil {
			t.Logf("VCR: stop error: %v", err)
		}
	})
	return r.HTTPClient()
}

func buildEvalProvider(t *testing.T, gt groundTruth, httpClient *http.Client) ci.Provider {
	t.Helper()
	isReplay := httpClient != nil && os.Getenv("HEISENBERG_EVAL_RECORD") != "1"
	switch gt.Provider {
	case "azure":
		pat := os.Getenv("AZURE_DEVOPS_PAT")
		if pat == "" && !isReplay {
			t.Skip("AZURE_DEVOPS_PAT not set")
		}
		if pat == "" {
			pat = "vcr-replay"
		}
		return azure.NewClient(gt.AzureOrg, gt.AzureProject, pat, httpClient)
	default: // "github" or empty
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" && !isReplay {
			t.Skip("GITHUB_TOKEN not set")
		}
		if token == "" {
			token = "vcr-replay"
		}
		parts := strings.SplitN(gt.Repo, "/", 2)
		require.Len(t, parts, 2, "repo must be owner/name")
		return github.NewClient(parts[0], parts[1], token, httpClient)
	}
}

func TestConfidenceInRange(t *testing.T) {
	tests := []struct {
		name       string
		confidence int
		min, max   int
		want       bool
	}{
		{"within range", 85, 80, 100, true},
		{"below min", 30, 80, 100, false},
		{"above max", 95, 0, 49, false},
		{"exact min", 80, 80, 100, true},
		{"exact max", 100, 80, 100, true},
		{"no upper bound (max=0)", 95, 80, 0, true},
		{"no upper bound, below min", 30, 80, 0, false},
		{"full range (0-100)", 50, 0, 100, true},
		{"zero confidence in low range", 0, 0, 49, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := confidenceInRange(tt.confidence, tt.min, tt.max)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Weighted scoring ---

func TestScoreCase_AllMatch(t *testing.T) {
	score := scoreCase(
		expectedAnalysis{FailureType: "assertion", BugLocation: "production"},
		llm.RootCauseAnalysis{FailureType: "assertion", BugLocation: llm.BugLocationProduction},
	)
	assert.InDelta(t, 1.0, score, 0.01)
}

func TestScoreCase_CategoryOnly(t *testing.T) {
	// failure_type unrelated (assertion vs timeout = 0), bug_location partial (test vs production = 0.3)
	score := scoreCase(
		expectedAnalysis{FailureType: "timeout", BugLocation: "test"},
		llm.RootCauseAnalysis{FailureType: "assertion", BugLocation: llm.BugLocationProduction},
	)
	// 0.3 * 0.0 + 0.5 * 0.3 = 0.15 / 0.8 ≈ 0.1875
	assert.InDelta(t, 0.1875, score, 0.01)
}

func TestScoreCase_FailureTypeOnly(t *testing.T) {
	score := scoreCase(
		expectedAnalysis{FailureType: "timeout", BugLocation: "test"},
		llm.RootCauseAnalysis{FailureType: "timeout", BugLocation: llm.BugLocationProduction},
	)
	// failure_type exact (0.3 * 1.0), bug_location partial (0.5 * 0.3) = 0.45 / 0.8 ≈ 0.5625
	assert.InDelta(t, 0.5625, score, 0.01)
}

func TestScoreCase_BugLocationOnly(t *testing.T) {
	score := scoreCase(
		expectedAnalysis{FailureType: "timeout", BugLocation: "production"},
		llm.RootCauseAnalysis{FailureType: "assertion", BugLocation: llm.BugLocationProduction},
	)
	// bug_location matches (0.5/0.8), failure_type doesn't (0.0/0.8)
	assert.InDelta(t, 0.625, score, 0.01)
}

func TestScoreCase_EmptyExpected(t *testing.T) {
	// No expected fields set — any result is a match
	score := scoreCase(
		expectedAnalysis{},
		llm.RootCauseAnalysis{FailureType: "assertion", BugLocation: llm.BugLocationProduction},
	)
	assert.InDelta(t, 1.0, score, 0.01)
}

func TestScoreCase_FilePathMatch(t *testing.T) {
	score := scoreCase(
		expectedAnalysis{FilePathContains: "checkout.spec.ts", FailureType: "timeout", BugLocation: "test"},
		llm.RootCauseAnalysis{
			FailureType: "timeout",
			BugLocation: llm.BugLocationTest,
			Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts"},
		},
	)
	assert.InDelta(t, 1.0, score, 0.01)
}

func TestScoreSuite_AggregateReport(t *testing.T) {
	report := evalReport{}
	report.addCategoryResult(true)
	report.addCategoryResult(true)
	report.addCategoryResult(false)
	report.addCaseScore(1.0)
	report.addCaseScore(0.5)
	report.addCaseScore(0.0)
	report.addConfidence(90, true)  // correct diagnosis
	report.addConfidence(80, false) // wrong diagnosis

	assert.InDelta(t, 0.667, report.categoryAccuracy(), 0.01)
	assert.InDelta(t, 0.5, report.meanScore(), 0.01)
	assert.Equal(t, 3, report.totalCases)
	assert.InDelta(t, 90.0, report.avgConfidenceCorrect(), 0.1)
	assert.InDelta(t, 80.0, report.avgConfidenceWrong(), 0.1)
}

// --- Semantic similarity ---

func TestFailureTypeSimilarity(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string
		wantMin float64
		wantMax float64
	}{
		{"exact match", "assertion", "assertion", 1.0, 1.0},
		{"network≡infra (merged)", "network", "infra", 1.0, 1.0},
		{"infra≡network (merged)", "infra", "network", 1.0, 1.0},
		{"timeout≈infra", "timeout", "infra", 0.2, 0.4},
		{"network≈timeout", "network", "timeout", 0.2, 0.4},
		{"assertion vs network", "assertion", "network", 0.0, 0.1},
		{"assertion vs timeout", "assertion", "timeout", 0.0, 0.1},
		{"flake vs assertion", "flake", "assertion", 0.0, 0.1},
		{"empty string", "", "assertion", 0.0, 0.0},
		{"unknown type", "assertion", "unknown", 0.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim := failureTypeSimilarity(tt.a, tt.b)
			assert.GreaterOrEqual(t, sim, tt.wantMin, "similarity too low")
			assert.LessOrEqual(t, sim, tt.wantMax, "similarity too high")
		})
	}
}

func TestBugLocationSimilarity(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string
		wantMin float64
		wantMax float64
	}{
		{"exact match", "production", "production", 1.0, 1.0},
		{"production≈test", "production", "test", 0.2, 0.5},
		{"test≈production", "test", "production", 0.2, 0.5},
		{"infra vs production", "infrastructure", "production", 0.0, 0.2},
		{"infra vs test", "infrastructure", "test", 0.0, 0.2},
		{"empty string", "", "test", 0.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim := bugLocationSimilarity(tt.a, tt.b)
			assert.GreaterOrEqual(t, sim, tt.wantMin, "similarity too low")
			assert.LessOrEqual(t, sim, tt.wantMax, "similarity too high")
		})
	}
}

func TestScoreCase_SemanticPartialCredit(t *testing.T) {
	// network expected, model returns infra — should get partial credit, not zero
	score := scoreCase(
		expectedAnalysis{FailureType: "network", BugLocation: "infrastructure"},
		llm.RootCauseAnalysis{FailureType: "infra", BugLocation: llm.BugLocationInfrastructure},
	)
	// bug_location exact match (full credit), failure_type partial (~0.5)
	assert.Greater(t, score, 0.5, "network→infra should get partial credit")
}

func TestScoreCase_UnrelatedNoCredit(t *testing.T) {
	// assertion expected, model returns timeout — no partial credit
	score := scoreCase(
		expectedAnalysis{FailureType: "assertion", BugLocation: "test"},
		llm.RootCauseAnalysis{FailureType: "timeout", BugLocation: llm.BugLocationProduction},
	)
	// assertion→timeout = 0 similarity, production→test = 0.3 similarity
	// 0.3 * 0.0 + 0.5 * 0.3 = 0.15 / 0.8 ≈ 0.1875
	assert.Less(t, score, 0.2, "assertion→timeout should get minimal credit")
}

func TestNormalizeFailureType(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"network", "infra"},
		{"infra", "infra"},
		{"assertion", "assertion"},
		{"timeout", "timeout"},
		{"flake", "flake"},
		{"unknown", "unknown"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeFailureType(tt.input))
		})
	}
}

func TestScoreCase_NetworkInfraMerge(t *testing.T) {
	score := scoreCase(
		expectedAnalysis{FailureType: "network", BugLocation: "infrastructure"},
		llm.RootCauseAnalysis{FailureType: "infra", BugLocation: llm.BugLocationInfrastructure},
	)
	assert.InDelta(t, 1.0, score, 0.01, "network≡infra should be full match")
}

func TestScoreCase_NetworkNetworkStillMatches(t *testing.T) {
	score := scoreCase(
		expectedAnalysis{FailureType: "network", BugLocation: "infrastructure"},
		llm.RootCauseAnalysis{FailureType: "network", BugLocation: llm.BugLocationInfrastructure},
	)
	assert.InDelta(t, 1.0, score, 0.01)
}

func TestScoreResult_PartialMatch(t *testing.T) {
	gt := groundTruth{
		Expected: expectedOutput{
			Analyses: []expectedAnalysis{
				{FailureType: "network"},
			},
			AllowPartialMatch: true,
		},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{FailureType: "network"},
			{FailureType: "timeout"}, // extra RCA — allowed with partial match
		},
	}

	matched, expected := scoreResult(gt, result)
	assert.Equal(t, 1, matched)
	assert.Equal(t, 1, expected)
}

func TestScoreResult_NoDuplicateCounting(t *testing.T) {
	gt := groundTruth{
		Expected: expectedOutput{
			Analyses: []expectedAnalysis{
				{FailureType: "network"},
				{FailureType: "network"}, // two expected, both "network"
			},
		},
	}
	result := &llm.AnalysisResult{
		RCAs: []llm.RootCauseAnalysis{
			{FailureType: "network"}, // only one actual
		},
	}

	matched, expected := scoreResult(gt, result)
	assert.Equal(t, 2, expected)
	assert.Equal(t, 1, matched, "same RCA should not be counted twice")
}

func requireEnv(t *testing.T) {
	t.Helper()
	// In VCR replay mode, API keys are not required.
	if os.Getenv("HEISENBERG_EVAL_RECORD") == "1" || os.Getenv("HEISENBERG_EVAL_VCR") == "0" {
		if os.Getenv("GOOGLE_API_KEY") == "" {
			t.Skip("GOOGLE_API_KEY not set (required for live/record mode)")
		}
	}
	// In replay mode (default), no env check needed — cassettes provide responses.
}

func loadGroundTruth(t *testing.T) []groundTruth {
	t.Helper()
	pattern := filepath.Join("..", "..", "testdata", "e2e", "ground-truth", "*.json")
	files, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.NotEmpty(t, files, "no ground truth files found at %s", pattern)

	var cases []groundTruth
	for _, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		var gt groundTruth
		require.NoError(t, json.Unmarshal(data, &gt))
		cases = append(cases, gt)
	}
	return cases
}

// --- Weighted scoring ---

const (
	weightFailureType = 0.3
	weightBugLocation = 0.5
	weightFilePath    = 0.2
)

// normalizeFailureType maps equivalent failure types to a canonical form.
// "network" and "infra" are merged — the model rarely distinguishes them.
func normalizeFailureType(ft string) string {
	if ft == "network" {
		return "infra"
	}
	return ft
}

// failureTypeSimilarity returns a similarity score (0.0-1.0) between two failure types.
// Categories are normalized before comparison (network→infra merge).
// Remaining related pairs receive partial credit.
func failureTypeSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	a = normalizeFailureType(a)
	b = normalizeFailureType(b)
	if a == b {
		return 1.0
	}
	// Symmetric lookup: normalize pair order.
	pair := [2]string{a, b}
	if pair[0] > pair[1] {
		pair[0], pair[1] = pair[1], pair[0]
	}
	sim, ok := failureTypeSimilarityMatrix[pair]
	if !ok {
		return 0
	}
	return sim
}

// failureTypeSimilarityMatrix defines semantic similarity between failure types.
// Keys are sorted alphabetically to ensure symmetric lookup.
var failureTypeSimilarityMatrix = map[[2]string]float64{
	{"infra", "network"}:   0.5, // both external/environment — high overlap
	{"infra", "timeout"}:   0.3, // both resource/timing related
	{"network", "timeout"}: 0.3, // network timeouts overlap
	{"flake", "timeout"}:   0.2, // flakes can manifest as timeouts
}

// bugLocationSimilarity returns a similarity score (0.0-1.0) between two bug locations.
func bugLocationSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1.0
	}
	pair := [2]string{a, b}
	if pair[0] > pair[1] {
		pair[0], pair[1] = pair[1], pair[0]
	}
	sim, ok := bugLocationSimilarityMatrix[pair]
	if !ok {
		return 0
	}
	return sim
}

// bugLocationSimilarityMatrix defines semantic similarity between bug locations.
var bugLocationSimilarityMatrix = map[[2]string]float64{
	{"production", "test"}: 0.3, // both are "code" (vs infrastructure)
}

// scoreCase returns a weighted score (0.0-1.0) for how well an RCA matches expected.
// Uses semantic similarity for partial credit on related categories.
func scoreCase(exp expectedAnalysis, rca llm.RootCauseAnalysis) float64 {
	totalWeight := 0.0
	earned := 0.0

	if exp.FailureType != "" {
		totalWeight += weightFailureType
		earned += weightFailureType * failureTypeSimilarity(exp.FailureType, rca.FailureType)
	}

	if exp.BugLocation != "" {
		totalWeight += weightBugLocation
		earned += weightBugLocation * bugLocationSimilarity(exp.BugLocation, string(rca.BugLocation))
	}

	if exp.FilePathContains != "" {
		totalWeight += weightFilePath
		path := ""
		if rca.Location != nil {
			path = rca.Location.FilePath
		}
		if strings.Contains(path, exp.FilePathContains) {
			earned += weightFilePath
		}
	}

	if totalWeight == 0 {
		return 1.0 // no expectations = match
	}
	return earned / totalWeight
}

// evalReport accumulates scoring metrics across an eval suite.
type evalReport struct {
	totalCases      int
	categoryMatches int
	scoreSum        float64
	confCorrect     []int // confidences on correct diagnoses (score > 0.5)
	confWrong       []int // confidences on wrong diagnoses (score <= 0.5)
}

func (r *evalReport) addCategoryResult(matched bool) {
	r.totalCases++
	if matched {
		r.categoryMatches++
	}
}

func (r *evalReport) addCaseScore(score float64) {
	r.scoreSum += score
}

func (r *evalReport) addConfidence(confidence int, correct bool) {
	if correct {
		r.confCorrect = append(r.confCorrect, confidence)
	} else {
		r.confWrong = append(r.confWrong, confidence)
	}
}

func (r *evalReport) categoryAccuracy() float64 {
	if r.totalCases == 0 {
		return 0
	}
	return float64(r.categoryMatches) / float64(r.totalCases)
}

func (r *evalReport) meanScore() float64 {
	if r.totalCases == 0 {
		return 0
	}
	return r.scoreSum / float64(r.totalCases)
}

func (r *evalReport) avgConfidenceCorrect() float64 {
	return avgInts(r.confCorrect)
}

func (r *evalReport) avgConfidenceWrong() float64 {
	return avgInts(r.confWrong)
}

func avgInts(vals []int) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0
	for _, v := range vals {
		sum += v
	}
	return float64(sum) / float64(len(vals))
}

// confidenceInRange checks if confidence falls within [min, max].
// max=0 means no upper bound.
func confidenceInRange(confidence, min, max int) bool {
	if confidence < min {
		return false
	}
	if max > 0 && confidence > max {
		return false
	}
	return true
}

// scoreResult checks how many expected analyses match actual RCAs.
// Each actual RCA can only be consumed once to prevent double-counting.
func scoreResult(gt groundTruth, result *llm.AnalysisResult) (matched, expected int) {
	expected = len(gt.Expected.Analyses)
	used := make(map[int]bool)
	for _, exp := range gt.Expected.Analyses {
		for i, rca := range result.RCAs {
			if !used[i] && matchesExpected(exp, rca) {
				matched++
				used[i] = true
				break
			}
		}
	}
	return
}

func matchesExpected(exp expectedAnalysis, rca llm.RootCauseAnalysis) bool {
	if exp.FailureType != "" && string(rca.FailureType) != exp.FailureType {
		return false
	}
	if exp.FilePathContains != "" {
		path := ""
		if rca.Location != nil {
			path = rca.Location.FilePath
		}
		if !strings.Contains(path, exp.FilePathContains) {
			return false
		}
	}
	if exp.BugLocation != "" && string(rca.BugLocation) != exp.BugLocation {
		return false
	}
	return true
}

func TestEval_Suite(t *testing.T) {
	requireEnv(t)

	model := os.Getenv("HEISENBERG_MODEL")
	if model == "" {
		model = llm.DefaultModel
	}

	allCases := loadGroundTruth(t)

	// Filter by eval tier: HEISENBERG_EVAL_TIER=smoke runs only tagged cases.
	// Default (empty or "full") runs all cases.
	tier := os.Getenv("HEISENBERG_EVAL_TIER")
	var cases []groundTruth
	if tier == "smoke" {
		for _, gt := range allCases {
			for _, tag := range gt.Tags {
				if tag == "smoke" {
					cases = append(cases, gt)
					break
				}
			}
		}
		t.Logf("Smoke tier: %d/%d cases", len(cases), len(allCases))
	} else {
		cases = allCases
	}

	logPath := filepath.Join("..", "..", "testdata", "e2e", "eval.jsonl")
	report := evalReport{}

	useVCR := os.Getenv("HEISENBERG_EVAL_RECORD") == "1" || os.Getenv("HEISENBERG_EVAL_VCR") != "0"
	isReplay := useVCR && os.Getenv("HEISENBERG_EVAL_RECORD") != "1"

	googleAPIKey := os.Getenv("GOOGLE_API_KEY")
	if googleAPIKey == "" && isReplay {
		googleAPIKey = "vcr-replay"
	}

	for _, gt := range cases {
		gt := gt
		name := fmt.Sprintf("%s/%d", gt.Repo, gt.RunID)

		t.Run(name, func(t *testing.T) {
			cassetteName := strings.ReplaceAll(gt.Repo, "/", "_") + "_" + fmt.Sprint(gt.RunID)
			cassettePath := filepath.Join("..", "..", "testdata", "e2e", "cassettes", cassetteName+".json.gz")

			// In record mode, skip cases that already have a cassette.
			if os.Getenv("HEISENBERG_EVAL_RECORD") == "1" {
				if _, err := os.Stat(cassettePath); err == nil {
					t.Skipf("cassette exists, skipping re-record")
				}
			}

			var vcrClient *http.Client
			var llmOpts []llm.ClientOption
			if useVCR {
				vcrClient = setupVCR(t, cassetteName)
				llmOpts = append(llmOpts, llm.WithHTTPClient(vcrClient))
			}

			provider := buildEvalProvider(t, gt, vcrClient)

			parts := strings.SplitN(gt.Repo, "/", 2)
			require.Len(t, parts, 2)

			emitter := llm.NewTextEmitter(os.Stderr, false)
			result, err := analysis.Run(context.Background(), analysis.Params{
				Owner:        parts[0],
				Repo:         parts[1],
				RunID:        gt.RunID,
				Verbose:      false,
				Emitter:      emitter,
				SnapshotHTML: trace.SnapshotHTML,
				Model:        model,
				CI:           provider,
				GoogleAPIKey: googleAPIKey,
				LLMOptions:   llmOpts,
			})
			emitter.Close()
			if err != nil {
				t.Logf("ERROR: %v", err)
				report.addCategoryResult(false)
				report.addCaseScore(0)
				return
			}

			scoreAndLog(t, gt, result, model, logPath, &report)
		})
	}

	// Aggregate report
	t.Logf("\n=== EVALUATION REPORT ===")
	t.Logf("Total cases: %d", report.totalCases)
	t.Logf("Mean score: %.1f%% (%0.1f/%d)", report.meanScore()*100, report.scoreSum, report.totalCases)
	t.Logf("Category accuracy: %.1f%%", report.categoryAccuracy()*100)
	t.Logf("Confidence (correct): %.0f avg", report.avgConfidenceCorrect())
	t.Logf("Confidence (wrong):   %.0f avg", report.avgConfidenceWrong())

	// Aggregate threshold assertion — catches regressions
	assert.GreaterOrEqual(t, report.meanScore(), 0.3,
		"aggregate score below minimum threshold — possible regression")
}

// scoreAndLog scores one eval case, logs per-case results, and appends to eval.jsonl.
func scoreAndLog(t *testing.T, gt groundTruth, result *llm.AnalysisResult,
	model, logPath string, report *evalReport) {
	t.Helper()

	report.addCategoryResult(result.Category == gt.Expected.Category)

	bestScore := bestRCAScore(gt.Expected.Analyses, result.RCAs)
	report.addCaseScore(bestScore)
	report.addConfidence(result.Confidence, bestScore > 0.5)

	icon := "✓"
	if bestScore < 0.5 {
		icon = "✗"
	}
	t.Logf("%s score=%.2f category=%s confidence=%d analyses=%d iterations=%d",
		icon, bestScore, result.Category, result.Confidence, len(result.RCAs),
		evalIterations(result))

	matched, expected := scoreResult(gt, result)
	t.Logf("category=%s confidence=%d analyses=%d matched=%d/%d iterations=%d",
		result.Category, result.Confidence, len(result.RCAs), matched, expected,
		evalIterations(result))

	entry := evalEntry{
		CaseID:           gt.CaseID,
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		Model:            model,
		Repo:             gt.Repo,
		RunID:            gt.RunID,
		Category:         result.Category,
		Confidence:       result.Confidence,
		Analyses:         len(result.RCAs),
		Iterations:       evalIterations(result),
		ModelMs:          evalModelMs(result),
		Tokens:           evalTokens(result),
		WallMs:           evalWallMs(result),
		Matched:          matched,
		Expected:         expected,
		ObservableByTool: gt.Truth.ObservableByTool,
		Tags:             gt.Tags,
		RCAs:             result.RCAs,
	}
	appendEvalLog(t, logPath, entry)
}

// bestRCAScore finds the highest weighted score across all expected × actual RCA pairs.
func bestRCAScore(expected []expectedAnalysis, rcas []llm.RootCauseAnalysis) float64 {
	if len(expected) == 0 {
		return 1.0
	}
	best := 0.0
	for _, exp := range expected {
		for _, rca := range rcas {
			if s := scoreCase(exp, rca); s > best {
				best = s
			}
		}
	}
	return best
}

func evalIterations(r *llm.AnalysisResult) int {
	if r.Eval != nil {
		return r.Eval.Iterations
	}
	return 0
}

func evalModelMs(r *llm.AnalysisResult) int {
	if r.Eval != nil {
		return r.Eval.ModelMs
	}
	return 0
}

func evalTokens(r *llm.AnalysisResult) int {
	if r.Eval != nil {
		return r.Eval.Tokens
	}
	return 0
}

func evalWallMs(r *llm.AnalysisResult) int {
	if r.Eval != nil {
		return r.Eval.WallMs
	}
	return 0
}

func appendEvalLog(t *testing.T, path string, entry evalEntry) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	_, err = f.Write(append(data, '\n'))
	require.NoError(t, err)
}

// TestEval_Report reads eval.jsonl and prints a comparison table.
func TestEval_Report(t *testing.T) {
	logPath := filepath.Join("..", "..", "testdata", "e2e", "eval.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Skipf("no eval log at %s", logPath)
	}

	entries := parseEvalEntries(t, data)

	// Group by repo+run_id → model → entries
	type key struct {
		repo  string
		runID int64
	}
	grouped := map[key]map[string][]evalEntry{}
	for _, e := range entries {
		k := key{e.Repo, e.RunID}
		if grouped[k] == nil {
			grouped[k] = map[string][]evalEntry{}
		}
		grouped[k][e.Model] = append(grouped[k][e.Model], e)
	}

	fmt.Println()
	for k, byModel := range grouped {
		fmt.Printf("  %s #%d\n", k.repo, k.runID)
		fmt.Printf("  %s\n", strings.Repeat("─", 70))

		var models []string
		for m := range byModel {
			models = append(models, m)
		}

		printReportHeader(models)
		printReportRows(models, byModel)
		fmt.Println()
	}
}

func parseEvalEntries(t *testing.T, data []byte) []evalEntry {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.NotEmpty(t, lines, "eval log is empty")

	var entries []evalEntry
	for _, line := range lines {
		if line == "" {
			continue
		}
		var e evalEntry
		require.NoError(t, json.Unmarshal([]byte(line), &e))
		entries = append(entries, e)
	}
	return entries
}

func printReportHeader(models []string) {
	fmt.Printf(labelFmt, "")
	for _, m := range models {
		fmt.Printf(valueFmt, m)
	}
	fmt.Println()
}

func printReportRows(models []string, byModel map[string][]evalEntry) {
	printRowInt(models, byModel, "Runs", func(es []evalEntry) string {
		return fmt.Sprintf("%d", len(es))
	})
	printRowInt(models, byModel, "Category match", func(es []evalEntry) string {
		match := 0
		for _, e := range es {
			if e.Category == "diagnosis" {
				match++
			}
		}
		return fmt.Sprintf("%d/%d", match, len(es))
	})
	printRowStats(models, byModel, "Confidence", func(e evalEntry) float64 {
		return float64(e.Confidence)
	})
	printRowStats(models, byModel, "Analyses count", func(e evalEntry) float64 {
		return float64(e.Analyses)
	})
	printRowInt(models, byModel, "Ground truth match", func(es []evalEntry) string {
		match, total := 0, 0
		for _, e := range es {
			match += e.Matched
			total += e.Expected
		}
		return fmt.Sprintf("%d/%d", match, total)
	})
	printRowStats(models, byModel, "Iterations", func(e evalEntry) float64 {
		return float64(e.Iterations)
	})
	printRowStatsUnit(models, byModel, "Wall time", "s", func(e evalEntry) float64 {
		return float64(e.WallMs) / 1000
	})
}

func printRowInt(models []string, byModel map[string][]evalEntry, label string, fn func([]evalEntry) string) {
	fmt.Printf(labelFmt, label)
	for _, m := range models {
		fmt.Printf(valueFmt, fn(byModel[m]))
	}
	fmt.Println()
}

func printRowStats(models []string, byModel map[string][]evalEntry, label string, extract func(evalEntry) float64) {
	fmt.Printf(labelFmt, label)
	for _, m := range models {
		vals := make([]float64, len(byModel[m]))
		for i, e := range byModel[m] {
			vals[i] = extract(e)
		}
		fmt.Printf(valueFmt, formatStats(vals))
	}
	fmt.Println()
}

func printRowStatsUnit(models []string, byModel map[string][]evalEntry, label, unit string, extract func(evalEntry) float64) {
	fmt.Printf(labelFmt, label)
	for _, m := range models {
		vals := make([]float64, len(byModel[m]))
		for i, e := range byModel[m] {
			vals[i] = extract(e)
		}
		fmt.Printf(valueFmt, formatStatsUnit(vals, unit))
	}
	fmt.Println()
}

func formatStats(vals []float64) string {
	if len(vals) == 0 {
		return "—"
	}
	if len(vals) == 1 {
		return fmt.Sprintf("%.0f", vals[0])
	}
	mean, stddev := meanStddev(vals)
	return fmt.Sprintf("%.1f ±%.1f", mean, stddev)
}

func formatStatsUnit(vals []float64, unit string) string {
	if len(vals) == 0 {
		return "—"
	}
	if len(vals) == 1 {
		return fmt.Sprintf("%.1f%s", vals[0], unit)
	}
	mean, stddev := meanStddev(vals)
	return fmt.Sprintf("%.1f ±%.1f%s", mean, stddev, unit)
}

func meanStddev(vals []float64) (float64, float64) {
	n := float64(len(vals))
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	mean := sum / n
	variance := 0.0
	for _, v := range vals {
		variance += (v - mean) * (v - mean)
	}
	return mean, math.Sqrt(variance / n)
}
