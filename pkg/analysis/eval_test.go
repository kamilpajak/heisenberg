//go:build eval

package analysis_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kamilpajak/heisenberg/pkg/analysis"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groundTruth defines expected results for a test case.
type groundTruth struct {
	Repo                  string             `json:"repo"`
	RunID                 int64              `json:"run_id"`
	ExpectedCategory      string             `json:"expected_category"`
	MinConfidence         int                `json:"min_confidence"`
	ExpectedAnalysesCount int                `json:"expected_analyses_count"` // 0 = any count
	ExpectedAnalyses      []expectedAnalysis `json:"expected_analyses"`
	Notes                 string             `json:"notes"`
}

type expectedAnalysis struct {
	FilePathContains string `json:"file_path_contains"`
	FailureType      string `json:"failure_type"`
	BugLocation      string `json:"bug_location"`
}

// evalEntry is one line in eval.jsonl.
type evalEntry struct {
	Timestamp  string                  `json:"timestamp"`
	Model      string                  `json:"model"`
	Repo       string                  `json:"repo"`
	RunID      int64                   `json:"run_id"`
	Category   string                  `json:"category"`
	Confidence int                     `json:"confidence"`
	Analyses   int                     `json:"analyses_count"`
	Iterations int                     `json:"iterations"`
	ModelMs    int                     `json:"model_ms"`
	Tokens     int                     `json:"tokens"`
	WallMs     int                     `json:"wall_ms"`
	Matched    int                     `json:"ground_truth_matched"`
	Expected   int                     `json:"ground_truth_expected"`
	RCAs       []llm.RootCauseAnalysis `json:"rca_details"`
}

func requireEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("GITHUB_TOKEN not set")
	}
	if os.Getenv("GOOGLE_API_KEY") == "" {
		t.Skip("GOOGLE_API_KEY not set")
	}
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

// scoreResult checks how many expected analyses match actual RCAs.
func scoreResult(gt groundTruth, result *llm.AnalysisResult) (matched, expected int) {
	expected = len(gt.ExpectedAnalyses)
	for _, exp := range gt.ExpectedAnalyses {
		for _, rca := range result.RCAs {
			if matchesExpected(exp, rca) {
				matched++
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

	cases := loadGroundTruth(t)
	logPath := filepath.Join("..", "..", "testdata", "e2e", "eval.jsonl")

	for _, gt := range cases {
		gt := gt
		parts := strings.SplitN(gt.Repo, "/", 2)
		require.Len(t, parts, 2)

		t.Run(gt.Repo, func(t *testing.T) {
			emitter := llm.NewTextEmitter(os.Stderr, false)
			result, err := analysis.Run(context.Background(), analysis.Params{
				Owner:        parts[0],
				Repo:         parts[1],
				RunID:        gt.RunID,
				Verbose:      false,
				Emitter:      emitter,
				SnapshotHTML: trace.SnapshotHTML,
				Model:        model,
			})
			emitter.Close()
			require.NoError(t, err)

			// Score against ground truth
			matched, expected := scoreResult(gt, result)

			// Assertions
			assert.Equal(t, gt.ExpectedCategory, result.Category, "category mismatch")
			assert.GreaterOrEqual(t, result.Confidence, gt.MinConfidence, "confidence too low")
			if gt.ExpectedAnalysesCount > 0 {
				assert.Equal(t, gt.ExpectedAnalysesCount, len(result.RCAs), "analyses count mismatch")
			}
			assert.Equal(t, expected, matched, "ground truth match: %d/%d", matched, expected)

			// Log results
			t.Logf("category=%s confidence=%d analyses=%d matched=%d/%d iterations=%d",
				result.Category, result.Confidence, len(result.RCAs), matched, expected,
				evalIterations(result))

			// Append to eval.jsonl
			entry := evalEntry{
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
				Model:      model,
				Repo:       gt.Repo,
				RunID:      gt.RunID,
				Category:   result.Category,
				Confidence: result.Confidence,
				Analyses:   len(result.RCAs),
				Iterations: evalIterations(result),
				ModelMs:    evalModelMs(result),
				Tokens:     evalTokens(result),
				WallMs:     evalWallMs(result),
				Matched:    matched,
				Expected:   expected,
				RCAs:       result.RCAs,
			}
			appendEvalLog(t, logPath, entry)
		})
	}
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

		// Collect all models
		var models []string
		for m := range byModel {
			models = append(models, m)
		}

		// Header
		fmt.Printf("  %-25s", "")
		for _, m := range models {
			fmt.Printf("  %-20s", m)
		}
		fmt.Println()

		// Runs
		fmt.Printf("  %-25s", "Runs")
		for _, m := range models {
			fmt.Printf("  %-20d", len(byModel[m]))
		}
		fmt.Println()

		// Category match
		fmt.Printf("  %-25s", "Category match")
		for _, m := range models {
			match := 0
			for _, e := range byModel[m] {
				if e.Category == "diagnosis" {
					match++
				}
			}
			fmt.Printf("  %-20s", fmt.Sprintf("%d/%d", match, len(byModel[m])))
		}
		fmt.Println()

		// Confidence
		fmt.Printf("  %-25s", "Confidence")
		for _, m := range models {
			vals := make([]float64, len(byModel[m]))
			for i, e := range byModel[m] {
				vals[i] = float64(e.Confidence)
			}
			fmt.Printf("  %-20s", formatStats(vals))
		}
		fmt.Println()

		// Analyses count
		fmt.Printf("  %-25s", "Analyses count")
		for _, m := range models {
			vals := make([]float64, len(byModel[m]))
			for i, e := range byModel[m] {
				vals[i] = float64(e.Analyses)
			}
			fmt.Printf("  %-20s", formatStats(vals))
		}
		fmt.Println()

		// Ground truth
		fmt.Printf("  %-25s", "Ground truth match")
		for _, m := range models {
			match := 0
			total := 0
			for _, e := range byModel[m] {
				match += e.Matched
				total += e.Expected
			}
			fmt.Printf("  %-20s", fmt.Sprintf("%d/%d", match, total))
		}
		fmt.Println()

		// Iterations
		fmt.Printf("  %-25s", "Iterations")
		for _, m := range models {
			vals := make([]float64, len(byModel[m]))
			for i, e := range byModel[m] {
				vals[i] = float64(e.Iterations)
			}
			fmt.Printf("  %-20s", formatStats(vals))
		}
		fmt.Println()

		// Wall time
		fmt.Printf("  %-25s", "Wall time")
		for _, m := range models {
			vals := make([]float64, len(byModel[m]))
			for i, e := range byModel[m] {
				vals[i] = float64(e.WallMs) / 1000
			}
			fmt.Printf("  %-20s", formatStatsUnit(vals, "s"))
		}
		fmt.Println()
		fmt.Println()
	}
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
