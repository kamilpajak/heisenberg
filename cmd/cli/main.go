package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/kamilpajak/heisenberg/internal/dashboard"
	"github.com/kamilpajak/heisenberg/pkg/analysis"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/trace"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var (
	verbose    bool
	jsonOutput bool
	runID      int64
	port       int
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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("heisenberg %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
	},
}

func init() {
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed tool call info")
	rootCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output result as JSON")
	rootCmd.Flags().Int64Var(&runID, "run-id", 0, "Specific workflow run ID to analyze")

	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
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
		SnapshotHTML: trace.SnapshotHTML,
	})
	if err != nil {
		emitter.Close()
		return fmt.Errorf("analysis failed: %w", err)
	}

	emitter.Close()

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(result)
	}

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

	// Use structured RCA if available, otherwise fall back to legacy text
	if r.RCA != nil && r.RCA.Title != "" {
		printStructuredRCA(stdout, r.RCA)
	} else {
		fmt.Fprintln(stdout, r.Text)
	}

	if r.Category == llm.CategoryDiagnosis && r.Confidence < 70 && r.Sensitivity == "high" {
		fmt.Fprintln(stderr)
		yellow := color.New(color.FgYellow)
		_, _ = yellow.Fprintln(stderr, "  Tip: Additional data sources (backend logs, Docker state) may improve this diagnosis.")
	}
}

func printStructuredRCA(w io.Writer, rca *llm.RootCauseAnalysis) {
	bold := color.New(color.Bold)
	dim := color.New(color.FgHiBlack)

	// Header with failure type and location
	failureType := strings.ToUpper(rca.FailureType)
	if failureType == "" {
		failureType = "ERROR"
	}

	header := failureType
	if rca.Location != nil && rca.Location.FilePath != "" {
		loc := rca.Location.FilePath
		if rca.Location.LineNumber > 0 {
			loc = fmt.Sprintf("%s:%d", rca.Location.FilePath, rca.Location.LineNumber)
		}
		header = fmt.Sprintf("%s in %s", header, loc)
	}
	_, _ = bold.Fprintln(w, header)
	fmt.Fprintln(w)

	// Root cause
	_, _ = bold.Fprintln(w, "ROOT CAUSE")
	fmt.Fprintln(w, rca.RootCause)
	fmt.Fprintln(w)

	// Evidence (if any)
	if len(rca.Evidence) > 0 {
		_, _ = bold.Fprintln(w, "EVIDENCE")
		for _, ev := range rca.Evidence {
			icon := evidenceIcon(ev.Type)
			_, _ = dim.Fprintf(w, "%s ", icon)
			fmt.Fprintln(w, ev.Content)
		}
		fmt.Fprintln(w)
	}

	// Remediation
	_, _ = bold.Fprintln(w, "FIX")
	fmt.Fprintln(w, rca.Remediation)
}

func evidenceIcon(t string) string {
	switch t {
	case llm.EvidenceScreenshot:
		return "[Screenshot]"
	case llm.EvidenceTrace:
		return "[Trace]"
	case llm.EvidenceLog:
		return "[Log]"
	case llm.EvidenceNetwork:
		return "[Network]"
	case llm.EvidenceCode:
		return "[Code]"
	default:
		return "[Evidence]"
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
		Handler: dashboard.NewHandler(),
	}

	// Graceful shutdown on interrupt (Ctrl+C)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	go func() {
		<-quit
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "Dashboard: http://localhost:%d\n", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}
