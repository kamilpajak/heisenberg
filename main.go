package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	gh "github.com/kamilpajak/heisenberg/internal/github"
	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/kamilpajak/heisenberg/internal/playwright"
	"github.com/kamilpajak/heisenberg/internal/server"
	"github.com/spf13/cobra"
)

var (
	verbose bool
	runID   int64
)

var rootCmd = &cobra.Command{
	Use:   "heisenberg <owner/repo>",
	Short: "AI-powered test failure analysis",
	Long:  `Analyzes test artifacts from GitHub repos using AI to identify root causes.`,
	Args:  cobra.ExactArgs(1),
	RunE:  run,
}

func init() {
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed tool call info")
	rootCmd.Flags().Int64Var(&runID, "run-id", 0, "Specific workflow run ID to analyze")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	repo := args[0]
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format, use: owner/repo")
	}
	owner, repoName := parts[0], parts[1]

	ctx := context.Background()
	ghClient := gh.NewClient()

	// Resolve run ID
	resolvedRunID := runID
	if resolvedRunID == 0 {
		fmt.Fprintf(os.Stderr, "Finding latest failed run for %s...\n", repo)
		runs, err := ghClient.ListWorkflowRuns(ctx, owner, repoName)
		if err != nil {
			return fmt.Errorf("failed to list runs: %w", err)
		}
		for _, r := range runs {
			if r.Conclusion == "failure" {
				resolvedRunID = r.ID
				break
			}
		}
		if resolvedRunID == 0 {
			return fmt.Errorf("no failed workflow runs found for %s", repo)
		}
	}

	fmt.Fprintf(os.Stderr, "Analyzing run %d for %s...\n", resolvedRunID, repo)

	// Fetch run metadata, jobs, and artifacts
	wfRun, err := ghClient.GetWorkflowRun(ctx, owner, repoName, resolvedRunID)
	if err != nil {
		return fmt.Errorf("failed to fetch run: %w", err)
	}

	jobs, err := ghClient.ListJobs(ctx, owner, repoName, resolvedRunID)
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	artifacts, err := ghClient.ListArtifacts(ctx, owner, repoName, resolvedRunID)
	if err != nil {
		return fmt.Errorf("failed to list artifacts: %w", err)
	}

	// Build initial context
	initialContext := buildInitialContext(wfRun, jobs, artifacts)

	if verbose {
		fmt.Fprintf(os.Stderr, "\n=== INITIAL CONTEXT ===\n%s\n=== END CONTEXT ===\n\n", initialContext)
	}

	// Create tool handler
	handler := &llm.ToolHandler{
		GitHub:       ghClient,
		Owner:        owner,
		Repo:         repoName,
		RunID:        resolvedRunID,
		SnapshotHTML: snapshotHTML,
	}

	// Run agentic loop
	llmClient, err := llm.NewClient()
	if err != nil {
		return err
	}

	result, err := llmClient.RunAgentLoop(ctx, handler, initialContext, verbose)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	fmt.Println(result)
	return nil
}

func buildInitialContext(run *gh.WorkflowRun, jobs []gh.Job, artifacts []gh.Artifact) string {
	var b strings.Builder

	b.WriteString("## Workflow Run\n")
	fmt.Fprintf(&b, "- Run ID: %d\n", run.ID)
	fmt.Fprintf(&b, "- Name: %s\n", run.Name)
	fmt.Fprintf(&b, "- Title: %s\n", run.DisplayTitle)
	fmt.Fprintf(&b, "- Branch: %s\n", run.HeadBranch)
	fmt.Fprintf(&b, "- Commit: %s\n", run.HeadSHA)
	fmt.Fprintf(&b, "- Event: %s\n", run.Event)
	fmt.Fprintf(&b, "- Conclusion: %s\n", run.Conclusion)
	if run.Path != "" {
		fmt.Fprintf(&b, "- Workflow file: %s\n", run.Path)
	}

	b.WriteString("\n## Jobs\n")
	for _, j := range jobs {
		fmt.Fprintf(&b, "- %s (id=%d, status=%s, conclusion=%s)\n", j.Name, j.ID, j.Status, j.Conclusion)
	}

	b.WriteString("\n## Artifacts\n")
	if len(artifacts) == 0 {
		b.WriteString("No artifacts found.\n")
	}
	for _, a := range artifacts {
		expired := ""
		if a.Expired {
			expired = " [EXPIRED]"
		}
		fmt.Fprintf(&b, "- %s (%d bytes)%s\n", a.Name, a.SizeBytes, expired)
	}

	b.WriteString("\n## Instructions\n")
	b.WriteString("Analyze this workflow run to determine why it failed.\n")
	b.WriteString("Use the available tools to fetch artifacts, logs, and source files as needed.\n")
	b.WriteString("When you have enough information, call the 'done' tool, then provide your final root cause analysis.\n")

	return b.String()
}

func snapshotHTML(htmlContent []byte) ([]byte, error) {
	fmt.Fprintf(os.Stderr, "  Rendering HTML report with Playwright...\n")

	if !playwright.IsAvailable() {
		return nil, fmt.Errorf("playwright not installed. Run: go run github.com/playwright-community/playwright-go/cmd/playwright install chromium")
	}

	srv, err := server.Start(htmlContent, "index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to start server: %w", err)
	}
	defer srv.Stop()

	snapshot, err := playwright.Snapshot(srv.URL("index.html"))
	if err != nil {
		return nil, fmt.Errorf("failed to capture snapshot: %w", err)
	}

	return snapshot, nil
}
