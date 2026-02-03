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
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show prompts and full response")
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

	// Fetch artifact from GitHub
	fmt.Fprintf(os.Stderr, "Fetching test artifacts from %s...\n", repo)

	ghClient := gh.NewClient()
	artifact, err := ghClient.FetchTestArtifact(ctx, owner, repoName, runID)
	if err != nil {
		return fmt.Errorf("failed to fetch artifacts: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Found artifact: %s (type: %s)\n", artifact.Name, artifact.Type)

	var analysisContent []byte

	switch artifact.Type {
	case gh.ArtifactJSON:
		analysisContent = artifact.Content

	case gh.ArtifactHTML:
		content, err := snapshotHTML(artifact.Content)
		if err != nil {
			return err
		}
		analysisContent = content

	case gh.ArtifactBlob:
		fmt.Fprintf(os.Stderr, "Merging blob reports...\n")

		reportDir, err := playwright.MergeBlobReports(artifact.Blobs)
		if err != nil {
			return fmt.Errorf("failed to merge blob reports: %w", err)
		}
		defer os.RemoveAll(reportDir)

		// Read the merged HTML
		htmlContent, err := findHTMLInDir(reportDir)
		if err != nil {
			return fmt.Errorf("no HTML report after merge: %w", err)
		}

		content, err := snapshotHTML(htmlContent)
		if err != nil {
			return err
		}
		analysisContent = content
	}

	// Analyze with LLM
	fmt.Fprintf(os.Stderr, "Analyzing with AI...\n")

	llmClient, err := llm.NewClient()
	if err != nil {
		return err
	}

	result, err := llmClient.Analyze(ctx, analysisContent)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	if verbose {
		fmt.Println("\n=== PROMPT ===")
		fmt.Println(result.Prompt)
		fmt.Println("\n=== RESPONSE ===")
	}
	fmt.Println(result.Response)

	return nil
}

func snapshotHTML(htmlContent []byte) ([]byte, error) {
	fmt.Fprintf(os.Stderr, "HTML report detected, capturing with Playwright...\n")

	if !playwright.IsAvailable() {
		return nil, fmt.Errorf("playwright not installed. Run: go run github.com/playwright-community/playwright-go/cmd/playwright install chromium")
	}

	srv, err := server.Start(htmlContent, "index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to start server: %w", err)
	}
	defer srv.Stop()

	url := srv.URL("index.html")
	fmt.Fprintf(os.Stderr, "Serving at %s\n", url)

	snapshot, err := playwright.Snapshot(url)
	if err != nil {
		return nil, fmt.Errorf("failed to capture snapshot: %w", err)
	}

	return snapshot, nil
}

func findHTMLInDir(dir string) ([]byte, error) {
	// Serve the directory and get index.html
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".html") {
			return os.ReadFile(dir + "/" + e.Name())
		}
	}

	return nil, fmt.Errorf("no HTML file found in %s", dir)
}
