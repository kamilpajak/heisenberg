package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
	"unicode"

	"github.com/fatih/color"
	"github.com/kamilpajak/heisenberg/internal/dashboard"
	"github.com/kamilpajak/heisenberg/pkg/analysis"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/saas"
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
	Use:           "heisenberg <owner/repo>",
	Short:         "AI-powered test failure analysis",
	Long:          `Analyzes test artifacts from GitHub repos using AI to identify root causes.`,
	Args:          cobra.ExactArgs(1),
	RunE:          run,
	SilenceUsage:  true,
	SilenceErrors: true,
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
	if err := rootCmd.Execute(); err != nil {
		printError(os.Stderr, err)
		os.Exit(1)
	}
}

func printError(w io.Writer, err error) {
	red := color.New(color.FgRed, color.Bold)
	dim := color.New(color.FgHiBlack)

	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintln(w)
		_, _ = red.Fprintf(w, "  Error: %s\n", apiErr)
		fmt.Fprintln(w)
		_, _ = dim.Fprintf(w, "  Hint: %s\n", apiErr.Hint())
		if verbose {
			fmt.Fprintln(w)
			_, _ = dim.Fprintf(w, "  Raw response:\n  %s\n", apiErr.RawBody)
		}
		return
	}

	fmt.Fprintln(w)
	_, _ = red.Fprintf(w, "  Error: %s\n", err)
}

func run(cmd *cobra.Command, args []string) error {
	repo := args[0]
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format, use: owner/repo")
	}
	owner, repoName := parts[0], parts[1]

	emitter := llm.NewTextEmitter(os.Stderr, verbose)

	result, err := analysis.Run(context.Background(), analysis.Params{
		Owner:        owner,
		Repo:         repoName,
		RunID:        runID,
		Verbose:      verbose,
		Emitter:      emitter,
		SnapshotHTML: trace.SnapshotHTML,
	})
	if err != nil {
		emitter.MarkFailed()
		emitter.Close()
		return err
	}

	emitter.Close()

	// Persist to SaaS dashboard (if configured)
	if client := saas.NewClient(); client != nil {
		id, err := client.SubmitAnalysis(context.Background(), saas.SubmitParams{
			OrgID:     os.Getenv("HEISENBERG_ORG_ID"),
			Owner:     owner,
			Repo:      repoName,
			RunID:     result.RunID,
			Branch:    result.Branch,
			CommitSHA: result.CommitSHA,
			Result:    result,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save to dashboard: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Saved to dashboard: %s/analyses/%s\n", client.BaseURL(), id)
		}
	}

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
	headerColor := color.New(color.Bold, color.FgRed)
	sectionColor := color.New(color.Bold, color.FgWhite)
	fixColor := color.New(color.Bold, color.FgGreen)
	separator := "  " + strings.Repeat("─", 40)

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
	_, _ = headerColor.Fprintln(w, "  "+header)
	fmt.Fprintln(w)

	// Root cause
	_, _ = sectionColor.Fprintln(w, "  Root Cause")
	_, _ = dim.Fprintln(w, separator)
	fmt.Fprintln(w, wrapBullets(rca.RootCause, maxLineWidth, "  "))
	fmt.Fprintln(w)

	// Evidence (if any)
	if len(rca.Evidence) > 0 {
		_, _ = sectionColor.Fprintln(w, "  Evidence")
		_, _ = dim.Fprintln(w, separator)
		// Find max icon width for alignment
		maxIcon := 0
		for _, ev := range rca.Evidence {
			if l := len(evidenceIcon(ev.Type)); l > maxIcon {
				maxIcon = l
			}
		}
		evidenceIndent := "  " + strings.Repeat(" ", maxIcon+1) // align continuation lines
		for _, ev := range rca.Evidence {
			icon := evidenceIcon(ev.Type)
			padding := strings.Repeat(" ", maxIcon-len(icon))
			prefix := fmt.Sprintf("  %s%s ", icon, padding)
			wrapped := wrapText(ev.Content, maxLineWidth, evidenceIndent)
			// Replace first line's indent with the icon prefix
			wrapped = prefix + strings.TrimLeft(wrapped, " ")
			_, _ = bold.Fprintln(w, wrapped)
		}
		fmt.Fprintln(w)
	}

	// Remediation
	_, _ = fixColor.Fprintln(w, "  Fix")
	_, _ = dim.Fprintln(w, separator)
	fmt.Fprintln(w, wrapBullets(rca.Remediation, maxLineWidth, "  "))
}

const maxLineWidth = 76 // 78 visible minus 2-char indent

// wrapText wraps s to maxWidth visible characters, breaking at word boundaries.
// Each line is prefixed with indent. The first line also gets the indent.
func wrapText(s string, maxWidth int, indent string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}

	var lines []string
	line := indent + words[0]
	lineLen := len(indent) + len(words[0])

	for _, w := range words[1:] {
		if lineLen+1+len(w) > maxWidth {
			lines = append(lines, line)
			line = indent + w
			lineLen = len(indent) + len(w)
		} else {
			line += " " + w
			lineLen += 1 + len(w)
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

// wrapBullets wraps text that may contain numbered items (1. foo 2. bar)
// or newline-separated paragraphs from the LLM.
func wrapBullets(s string, maxWidth int, indent string) string {
	// Split on \n first (LLM may use literal newlines)
	paragraphs := strings.Split(s, "\n")
	var out []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Detect numbered items like "1. " or "- "
		bulletIndent := indent
		if len(p) >= 3 && unicode.IsDigit(rune(p[0])) && p[1] == '.' && p[2] == ' ' {
			bulletIndent = indent + "   " // continuation indent for numbered items
		} else if strings.HasPrefix(p, "- ") {
			bulletIndent = indent + "  "
		}
		wrapped := wrapText(p, maxWidth, bulletIndent)
		// Replace first line's wider indent with base indent so bullet marker is visible
		wrapped = indent + strings.TrimPrefix(wrapped, bulletIndent)
		out = append(out, wrapped)
	}
	return strings.Join(out, "\n")
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
	filled := min(confidence*barWidth/100, barWidth)

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
