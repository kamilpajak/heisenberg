package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fatih/color"
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
	if rootCmd.Execute() != nil {
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

	emitter := llm.NewTextEmitter(os.Stderr)

	result, err := analysis.Run(context.Background(), analysis.Params{
		Owner:        owner,
		Repo:         repoName,
		RunID:        runID,
		Verbose:      verbose,
		Emitter:      emitter,
		SnapshotHTML: playwright.SnapshotHTML,
	})
	if err != nil {
		emitter.Close()
		return fmt.Errorf("analysis failed: %w", err)
	}

	emitter.Close()
	printResult(os.Stderr, os.Stdout, result)
	return nil
}

func printResult(stderr, stdout io.Writer, r *llm.AnalysisResult) {
	if r.Category == llm.CategoryDiagnosis {
		fmt.Fprintln(stderr)
		dim := color.New(color.FgHiBlack)
		_, _ = dim.Fprintln(stderr, "  "+strings.Repeat("━", 50))
		printConfidenceBar(stderr, r.Confidence, r.Sensitivity)
	}

	fmt.Fprintln(stderr)
	fmt.Fprintln(stdout, r.Text)

	if r.Category == llm.CategoryDiagnosis && r.Confidence < 70 && r.Sensitivity == "high" {
		fmt.Fprintln(stderr)
		yellow := color.New(color.FgYellow)
		_, _ = yellow.Fprintln(stderr, "  Tip: Additional data sources (backend logs, Docker state) may improve this diagnosis.")
	}
}

func printConfidenceBar(w io.Writer, confidence int, sensitivity string) {
	const barWidth = 24
	filled := confidence * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}

	var barColor *color.Color
	switch {
	case confidence >= 80:
		barColor = color.New(color.FgGreen)
	case confidence >= 40:
		barColor = color.New(color.FgYellow)
	default:
		barColor = color.New(color.FgRed)
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	fmt.Fprintf(w, "  Confidence: %d%% ", confidence)
	_, _ = barColor.Fprint(w, bar)
	dim := color.New(color.FgHiBlack)
	_, _ = dim.Fprintf(w, " (%s sensitivity)\n", sensitivity)
}

func serve(cmd *cobra.Command, args []string) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
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
