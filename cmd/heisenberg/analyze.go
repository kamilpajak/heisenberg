package heisenberg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/kamilpajak/heisenberg/internal/analyzer"
	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/kamilpajak/heisenberg/internal/parser"
	"github.com/kamilpajak/heisenberg/pkg/models"
	"github.com/spf13/cobra"
)

var (
	analyzeProvider string
	analyzeModel    string
	analyzeFormat   string
)

// Regex to detect GitHub repo format (owner/repo)
var githubRepoPattern = regexp.MustCompile(`^[\w-]+/[\w.-]+$`)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [target]",
	Short: "Analyze test failures from any source",
	Long: `Analyze test failures from a local file or GitHub repository.

TARGET can be:
  - A local file path: ./report.json
  - A GitHub repo: owner/repo (coming soon)

Examples:
  heisenberg analyze ./playwright-report/results.json
  heisenberg analyze ./report.json --provider openai --model gpt-4o
  heisenberg analyze ./report.json --format json`,
	Args: cobra.ExactArgs(1),
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().StringVarP(&analyzeProvider, "provider", "p", "google", "LLM provider (google, openai, anthropic)")
	analyzeCmd.Flags().StringVarP(&analyzeModel, "model", "m", "", "Specific model name")
	analyzeCmd.Flags().StringVarP(&analyzeFormat, "format", "f", "text", "Output format (text, json)")
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	target := args[0]
	ctx := context.Background()

	// Detect target type
	if githubRepoPattern.MatchString(target) {
		return fmt.Errorf("GitHub repo analysis not yet implemented: %s", target)
	}

	// Local file analysis
	return analyzeLocalFile(ctx, target)
}

func analyzeLocalFile(ctx context.Context, path string) error {
	// Check file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", path)
	}

	// Parse report
	fmt.Fprintf(os.Stderr, "Parsing report: %s\n", path)
	p := &parser.PlaywrightParser{}
	report, err := p.Parse(path)
	if err != nil {
		return fmt.Errorf("failed to parse report: %w", err)
	}

	// Check for failures
	if !report.HasFailures() {
		fmt.Println("No test failures found.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Found %d failures in %d tests\n", report.FailedTests, report.TotalTests)

	// Create analyzer
	provider := llm.Provider(analyzeProvider)
	a, err := analyzer.New(provider, analyzeModel, "")
	if err != nil {
		return fmt.Errorf("failed to create analyzer: %w", err)
	}

	// Run analysis
	fmt.Fprintf(os.Stderr, "Analyzing with %s...\n", analyzeProvider)
	start := time.Now()

	result, err := a.Analyze(ctx, report)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	elapsed := time.Since(start)
	fmt.Fprintf(os.Stderr, "Analysis complete (%.1fs, %d tokens)\n\n", elapsed.Seconds(), result.InputTokens+result.OutputTokens)

	// Output result
	if analyzeFormat == "json" {
		return outputJSON(result)
	}
	return outputText(result)
}

func outputJSON(result *models.AnalysisResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func outputText(result *models.AnalysisResult) error {
	d := result.Diagnosis

	// Confidence badge
	confidenceBadge := map[models.Confidence]string{
		models.ConfidenceHigh:   "HIGH",
		models.ConfidenceMedium: "MEDIUM",
		models.ConfidenceLow:    "LOW",
	}[d.Confidence]

	fmt.Printf("## Root Cause Analysis [%s]\n\n", confidenceBadge)
	fmt.Printf("%s\n\n", d.RootCause)

	if len(d.Evidence) > 0 {
		fmt.Println("### Evidence")
		for _, e := range d.Evidence {
			fmt.Printf("- %s\n", e)
		}
		fmt.Println()
	}

	if d.SuggestedFix != "" {
		fmt.Println("### Suggested Fix")
		fmt.Printf("%s\n\n", d.SuggestedFix)
	}

	// Footer
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("Model: %s | Tokens: %d in / %d out\n",
		result.Model, result.InputTokens, result.OutputTokens)

	return nil
}
