package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kamilpajak/heisenberg/internal/analysis"
	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/kamilpajak/heisenberg/internal/playwright"
	"github.com/kamilpajak/heisenberg/internal/web"
	"github.com/spf13/cobra"
)

var (
	verbose bool
	runID   int64
	port    int
)

var rootCmd = &cobra.Command{
	Use:   "heisenberg <owner/repo>",
	Short: "AI-powered test failure analysis",
	Long:  `Analyzes test artifacts from GitHub repos using AI to identify root causes.`,
	Args:  cobra.ExactArgs(1),
	RunE:  run,
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the local web dashboard",
	RunE:  serve,
}

func init() {
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed tool call info")
	rootCmd.Flags().Int64Var(&runID, "run-id", 0, "Specific workflow run ID to analyze")

	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
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

	result, err := analysis.Run(context.Background(), analysis.Params{
		Owner:        owner,
		Repo:         repoName,
		RunID:        runID,
		Verbose:      verbose,
		Emitter:      &llm.TextEmitter{W: os.Stderr},
		SnapshotHTML: playwright.SnapshotHTML,
	})
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	printResult(result)
	return nil
}

func printResult(r *llm.AnalysisResult) {
	if r.Category == llm.CategoryDiagnosis {
		fmt.Printf("Confidence: %d%% (sensitivity: %s)\n\n", r.Confidence, r.Sensitivity)
	}

	fmt.Println(r.Text)

	if r.Category == llm.CategoryDiagnosis && r.Confidence < 70 && r.Sensitivity == "high" {
		fmt.Println()
		fmt.Println("Tip: Additional data sources (backend logs, Docker state) may improve this diagnosis.")
	}
}

func serve(cmd *cobra.Command, args []string) error {
	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:    addr,
		Handler: web.NewHandler(),
	}

	// Graceful shutdown on SIGINT/SIGTERM
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-done
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		srv.Close()
	}()

	fmt.Fprintf(os.Stderr, "Dashboard: http://localhost%s\n", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

