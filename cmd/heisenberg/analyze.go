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
	"github.com/kamilpajak/heisenberg/internal/discovery"
	"github.com/kamilpajak/heisenberg/internal/github"
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
  - A GitHub repo: owner/repo

Examples:
  heisenberg analyze ./playwright-report/results.json
  heisenberg analyze ./report.json --provider openai --model gpt-4o
  heisenberg analyze ./report.json --format json
  heisenberg analyze playwright/playwright`,
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
		return analyzeGitHubRepo(ctx, target)
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

	return runAnalysis(ctx, report)
}

func analyzeGitHubRepo(ctx context.Context, repo string) error {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format, expected owner/repo: %s", repo)
	}
	owner, repoName := parts[0], parts[1]

	fmt.Fprintf(os.Stderr, "Fetching artifacts from: %s\n", repo)

	// Create GitHub client
	ghClient := github.NewClient("")

	// Discover artifacts
	svc := discovery.New(ghClient)
	result, err := svc.DiscoverRepo(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	if !result.Compatible {
		return fmt.Errorf("no compatible test artifacts found in %s: %s", repo, result.Error)
	}

	// Find artifact with failures (prefer) or any artifact
	var targetArtifact *discovery.ArtifactDiscoveryResult
	for i := range result.Artifacts {
		a := &result.Artifacts[i]
		if a.HasFailures {
			targetArtifact = a
			break
		}
		if targetArtifact == nil {
			targetArtifact = a
		}
	}

	if targetArtifact == nil {
		return fmt.Errorf("no artifacts found in %s", repo)
	}

	fmt.Fprintf(os.Stderr, "Found artifact: %s (run #%d)\n",
		targetArtifact.ArtifactName, targetArtifact.WorkflowRun.RunNumber)

	// Download and parse the artifact
	files, err := ghClient.ExtractArtifact(ctx, owner, repoName, targetArtifact.ArtifactID, discovery.ReportPatterns)
	if err != nil {
		return fmt.Errorf("failed to download artifact: %w", err)
	}

	// Find and parse the report file
	var report *models.Report
	for _, file := range files {
		if file.Name == targetArtifact.FileName || strings.HasSuffix(file.Name, ".json") {
			p := &parser.PlaywrightParser{}
			report, err = p.ParseBytes(file.Content)
			if err == nil && report != nil {
				break
			}
		}
	}

	if report == nil {
		return fmt.Errorf("failed to parse report from artifact")
	}

	return runAnalysis(ctx, report)
}

func runAnalysis(ctx context.Context, report *models.Report) error {
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
