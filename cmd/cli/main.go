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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/kamilpajak/heisenberg/internal/dashboard"
	"github.com/kamilpajak/heisenberg/pkg/analysis"
	"github.com/kamilpajak/heisenberg/pkg/config"
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

// Exit codes for scriptability and CI integration.
const (
	exitGeneral     = 1 // runtime error
	exitUsage       = 2 // bad arguments (reserved, handled by cobra)
	exitAPIError    = 3 // external AI/API error
	exitConfigError = 4 // missing or invalid configuration
)

var (
	verbose    bool
	jsonOutput bool
	format     string
	fromEnv    bool
	runURL     string
	runID      int64
	modelName  string
	port       int
)

var rootCmd = &cobra.Command{
	Use:           "heisenberg <owner/repo>",
	Short:         "AI-powered test failure analysis",
	Long:          `Analyzes test artifacts from GitHub repos using AI to identify root causes.`,
	Args:          cobra.MaximumNArgs(1),
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
	rootCmd.Flags().StringVar(&format, "format", "", "Output format: human or json (default: auto-detect from TTY)")
	rootCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output result as JSON (alias for --format json)")
	rootCmd.Flags().BoolVar(&fromEnv, "from-env", false, "Read owner/repo from GITHUB_REPOSITORY and run ID from GITHUB_RUN_ID")
	rootCmd.Flags().StringVar(&runURL, "run", "", "GitHub Actions run URL (e.g. https://github.com/org/repo/actions/runs/123)")
	rootCmd.Flags().Int64Var(&runID, "run-id", 0, "Specific workflow run ID to analyze")
	rootCmd.Flags().StringVar(&modelName, "model", "", "Gemini model name (env: HEISENBERG_MODEL, default: "+llm.DefaultModel+")")
	_ = rootCmd.Flags().MarkHidden("json") // backward compat alias

	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		code := printError(os.Stderr, err)
		os.Exit(code)
	}
}

const errorFmt = "  Error: %s\n"

// exitCodeLabel maps exit codes to human-readable descriptions.
var exitCodeLabel = map[int]string{
	exitGeneral:     "runtime error",
	exitUsage:       "bad arguments",
	exitAPIError:    "external API error",
	exitConfigError: "configuration error",
}

func printError(w io.Writer, err error) int {
	code := exitGeneral

	var apiErr *llm.APIError
	var cfgErr *llm.ConfigError

	switch {
	case errors.As(err, &apiErr):
		code = exitAPIError
	case errors.As(err, &cfgErr):
		code = exitConfigError
	}

	if format == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Error         string `json:"error"`
			ExitCode      int    `json:"exit_code"`
		}{SchemaVersion: llm.SchemaV1, Error: err.Error(), ExitCode: code})
		return code
	}

	red := color.New(color.FgRed, color.Bold)
	dim := color.New(color.FgHiBlack)

	switch {
	case apiErr != nil:
		fmt.Fprintln(w)
		_, _ = red.Fprintf(w, errorFmt, apiErr)
		fmt.Fprintln(w)
		_, _ = dim.Fprintf(w, "  Hint: %s\n", apiErr.Hint())
		if verbose {
			fmt.Fprintln(w)
			_, _ = dim.Fprintf(w, "  Raw response:\n  %s\n", apiErr.RawBody)
		}

	case cfgErr != nil:
		fmt.Fprintln(w)
		_, _ = red.Fprintf(w, errorFmt, cfgErr)
		fmt.Fprintln(w)
		_, _ = dim.Fprintln(w, "  Hint: Check your environment variables and configuration")

	default:
		fmt.Fprintln(w)
		_, _ = red.Fprintf(w, errorFmt, err)
	}

	fmt.Fprintln(w)
	_, _ = dim.Fprintf(w, "  Exit code: %d  (%s)\n", code, exitCodeLabel[code])
	return code
}

func run(cmd *cobra.Command, args []string) error {
	owner, repoName, err := resolveRepo(args)
	if err != nil {
		return err
	}

	format = resolveFormat(format, jsonOutput, isTerminal(os.Stdout))

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v (using defaults)\n", err)
		cfg = &config.Config{}
	}

	modelName = resolveModel(modelName, cfg.Model)

	emitter := llm.NewTextEmitter(os.Stderr, verbose)

	result, err := analysis.Run(context.Background(), analysis.Params{
		Owner:        owner,
		Repo:         repoName,
		RunID:        runID,
		Verbose:      verbose,
		Emitter:      emitter,
		SnapshotHTML: trace.SnapshotHTML,
		Model:        modelName,
		GitHubToken:  cfg.GitHubToken,
		GoogleAPIKey: cfg.GoogleAPIKey,
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

	if format == "json" {
		return json.NewEncoder(os.Stdout).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			*llm.AnalysisResult
		}{SchemaVersion: llm.SchemaV1, AnalysisResult: result})
	}

	printResult(os.Stderr, os.Stdout, result)
	return nil
}

// resolveRepo determines owner/repo from args, --from-env, or --run URL.
func resolveRepo(args []string) (owner, repo string, err error) {
	switch {
	case runURL != "":
		return parseRunURL(runURL)
	case fromEnv:
		ghRepo := os.Getenv("GITHUB_REPOSITORY")
		if ghRepo == "" {
			return "", "", fmt.Errorf("--from-env: GITHUB_REPOSITORY not set")
		}
		parts := strings.SplitN(ghRepo, "/", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("--from-env: invalid GITHUB_REPOSITORY: %s", ghRepo)
		}
		if ghRunID := os.Getenv("GITHUB_RUN_ID"); ghRunID != "" && runID == 0 {
			if id, err := strconv.ParseInt(ghRunID, 10, 64); err == nil {
				runID = id
			}
		}
		return parts[0], parts[1], nil
	case len(args) > 0:
		parts := strings.SplitN(args[0], "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid repo format, use: owner/repo")
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("provide owner/repo, --from-env, or --run <URL>")
	}
}

// runURLRe matches GitHub Actions run URLs.
var runURLRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/actions/runs/(\d+)`)

// parseRunURL extracts owner, repo, and run ID from a GitHub Actions URL.
func parseRunURL(url string) (owner, repo string, err error) {
	m := runURLRe.FindStringSubmatch(url)
	if m == nil {
		return "", "", fmt.Errorf("invalid GitHub Actions URL: %s\nExpected: https://github.com/owner/repo/actions/runs/123", url)
	}
	if id, parseErr := strconv.ParseInt(m[3], 10, 64); parseErr == nil {
		runID = id
	}
	return m[1], m[2], nil
}

// isTerminal returns true if the file is a TTY.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// resolveFormat determines output format from flag, --json alias, and TTY detection.
func resolveFormat(formatFlag string, jsonFlag bool, isTTY bool) string {
	if jsonFlag {
		return "json"
	}
	if formatFlag != "" {
		return formatFlag
	}
	if isTTY {
		return "human"
	}
	return "json"
}

// resolveModel determines model name from flag and environment variable.
func resolveModel(flag, configValue string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("HEISENBERG_MODEL"); env != "" {
		return env
	}
	return configValue
}

func printResult(stderr, stdout io.Writer, r *llm.AnalysisResult) {
	fmt.Fprintln(stderr)

	// Run-level header
	printRunHeader(stderr, r)

	if r.Category == llm.CategoryDiagnosis {
		printConfidenceBar(stderr, r.Confidence, r.Sensitivity)
		fmt.Fprintln(stderr)
	}

	// RCA output
	if len(r.RCAs) > 1 {
		printClusterSummary(stdout, r.RCAs)
		for i := range r.RCAs {
			fmt.Fprintln(stdout)
			printClusterCard(stdout, &r.RCAs[i], i+1, len(r.RCAs))
		}
	} else if len(r.RCAs) == 1 && r.RCAs[0].Title != "" {
		printStructuredRCA(stdout, &r.RCAs[0])
	} else {
		fmt.Fprintln(stdout, r.Text)
	}

	if r.Category == llm.CategoryDiagnosis && r.Confidence < 70 && r.Sensitivity == "high" {
		fmt.Fprintln(stderr)
		yellow := color.New(color.FgYellow)
		_, _ = yellow.Fprintln(stderr, "  Tip: Additional data sources (backend logs, Docker state) may improve this diagnosis.")
	}
}

// printRunHeader renders the top-level run summary.
func printRunHeader(w io.Writer, r *llm.AnalysisResult) {
	bold := color.New(color.Bold)
	dim := color.New(color.FgHiBlack)

	// Title line
	title := "Heisenberg"
	if r.Owner != "" && r.Repo != "" {
		title = fmt.Sprintf("Heisenberg — %s/%s", r.Owner, r.Repo)
	}
	if r.RunID > 0 {
		title += fmt.Sprintf(" #%d", r.RunID)
	}
	_, _ = bold.Fprintln(w, "  "+title)

	// Metadata line
	var meta []string
	if r.Branch != "" {
		meta = append(meta, "Branch: "+r.Branch)
	}
	if r.Event != "" {
		meta = append(meta, "Event: "+r.Event)
	}
	meta = append(meta, "Status: "+r.Category)
	_, _ = dim.Fprintf(w, "  %s\n", strings.Join(meta, "   "))

	_, _ = dim.Fprintln(w, "  "+strings.Repeat("━", 50))
}

// printClusterSummary shows a grouped overview of RCAs by bug_location.
func printClusterSummary(w io.Writer, rcas []llm.RootCauseAnalysis) {
	bold := color.New(color.Bold)
	dim := color.New(color.FgHiBlack)

	_, _ = bold.Fprintf(w, "\n  %d root causes found:\n\n", len(rcas))

	for i, rca := range rcas {
		tag := bugLocationLabel(rca.BugLocation)
		failureType := strings.ToUpper(string(rca.FailureType))
		if failureType == "" {
			failureType = "ERROR"
		}
		loc := ""
		if rca.Location != nil && rca.Location.FilePath != "" {
			loc = " in " + formatCodeLocation(rca.Location)
		}
		_, _ = dim.Fprintf(w, "  %d. %-16s %s%s\n", i+1, tag, failureType, loc)
		if rca.Title != "" {
			_, _ = dim.Fprintf(w, "     %s\n", rca.Title)
		}
	}
}

// bugLocationLabel returns a colored tag for the bug location.
func bugLocationLabel(loc llm.BugLocation) string {
	switch loc {
	case llm.BugLocationProduction:
		return color.RedString("[production]")
	case llm.BugLocationInfrastructure:
		return color.YellowString("[infra]")
	case llm.BugLocationTest:
		return color.CyanString("[test]")
	default:
		return color.HiBlackString("[unknown]")
	}
}

// printClusterCard renders a single RCA as a cluster card with header.
func printClusterCard(w io.Writer, rca *llm.RootCauseAnalysis, num, total int) {
	dim := color.New(color.FgHiBlack)
	bold := color.New(color.Bold)

	// Cluster header
	tag := bugLocationLabel(rca.BugLocation)
	conf := ""
	if rca.BugLocationConfidence != "" {
		conf = ", " + rca.BugLocationConfidence + " confidence"
	}
	_, _ = dim.Fprintln(w, "  "+strings.Repeat("━", 50))
	_, _ = bold.Fprintf(w, "  Cluster %d/%d %s%s\n", num, total, tag, conf)

	// Delegate to existing structured RCA renderer
	printStructuredRCA(w, rca)
}

// renderRCAHeader prints the failure type, location, bug location tag, and suspected bug file.
func renderRCAHeader(w io.Writer, rca *llm.RootCauseAnalysis) {
	headerColor := color.New(color.Bold, color.FgRed)
	dim := color.New(color.FgHiBlack)

	failureType := strings.ToUpper(rca.FailureType)
	if failureType == "" {
		failureType = "ERROR"
	}

	header := failureType
	if rca.Location != nil && rca.Location.FilePath != "" {
		loc := formatCodeLocation(rca.Location)
		header = fmt.Sprintf("%s in %s", header, loc)
	}
	if tag := bugLocationTag(rca.BugLocation, rca.BugLocationConfidence); tag != "" {
		header += "  " + tag
	}
	_, _ = headerColor.Fprintln(w, "  "+header)

	if rca.BugCodeLocation != nil && rca.BugCodeLocation.FilePath != "" {
		_, _ = dim.Fprintf(w, "  Bug location: %s\n", formatCodeLocation(rca.BugCodeLocation))
	}
	fmt.Fprintln(w)
}

func formatCodeLocation(loc *llm.CodeLocation) string {
	if loc.LineNumber > 0 {
		return fmt.Sprintf("%s:%d", loc.FilePath, loc.LineNumber)
	}
	return loc.FilePath
}

func printStructuredRCA(w io.Writer, rca *llm.RootCauseAnalysis) {
	bold := color.New(color.Bold)
	dim := color.New(color.FgHiBlack)
	sectionColor := color.New(color.Bold, color.FgWhite)
	fixColor := color.New(color.Bold, color.FgGreen)
	separator := "  " + strings.Repeat("─", 40)

	renderRCAHeader(w, rca)

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

var numberedItemRe = regexp.MustCompile(`^\d+\.\s`)

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
		// Detect numbered items like "1. ", "10. " or "- "
		bulletIndent := indent
		if loc := numberedItemRe.FindStringIndex(p); loc != nil {
			bulletIndent = indent + strings.Repeat(" ", loc[1]) // align past "N. "
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

func bugLocationTag(loc llm.BugLocation, confidence string) string {
	switch loc {
	case llm.BugLocationProduction:
		if confidence == "low" {
			return "[production bug?]"
		}
		return "[production bug]"
	case llm.BugLocationInfrastructure:
		if confidence == "low" {
			return "[infrastructure?]"
		}
		return "[infrastructure]"
	default:
		return ""
	}
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
