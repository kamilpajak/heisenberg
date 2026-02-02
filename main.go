package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/kamilpajak/heisenberg/internal/github"
	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "heisenberg <owner/repo>",
	Short: "AI-powered test failure analysis",
	Long:  `Analyzes test artifacts from GitHub repos using AI to identify root causes.`,
	Args:  cobra.ExactArgs(1),
	RunE:  run,
}

func init() {
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show prompts and responses")
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

	gh := github.NewClient()
	content, artifactName, err := gh.FetchTestArtifact(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("failed to fetch artifacts: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Found artifact: %s (%d bytes)\n", artifactName, len(content))

	// Analyze with LLM
	fmt.Fprintf(os.Stderr, "Analyzing with AI...\n")

	llmClient, err := llm.NewClient()
	if err != nil {
		return err
	}

	result, err := llmClient.Analyze(ctx, content)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Output
	if verbose {
		fmt.Println("\n=== PROMPT ===")
		fmt.Println(result.Prompt)
		fmt.Println("\n=== RESPONSE ===")
		fmt.Println(result.Response)
	} else {
		fmt.Println()
		fmt.Println(result.Response)
	}

	return nil
}
