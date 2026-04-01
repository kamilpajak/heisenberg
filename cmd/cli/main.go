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

	"net/url"

	"github.com/fatih/color"
	"github.com/kamilpajak/heisenberg/internal/dashboard"
	"github.com/kamilpajak/heisenberg/pkg/analysis"
	"github.com/kamilpajak/heisenberg/pkg/azure"
	"github.com/kamilpajak/heisenberg/pkg/ci"
	"github.com/kamilpajak/heisenberg/pkg/config"
	"github.com/kamilpajak/heisenberg/pkg/github"
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
	verbose      bool
	jsonOutput   bool
	format       string
	fromEnv      bool
	runURL       string
	runID        int64
	modelName    string
	port         int
	providerFlag string
	azureOrg     string
	azureProject string
	testRepo     string
	debug        bool
)

var rootCmd = &cobra.Command{
	Use:   "heisenberg",
	Short: "AI-powered test failure analysis",
	Long:  `Analyzes CI test failures using AI to identify root causes. Supports GitHub Actions and Azure Pipelines.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && !fromEnv && runURL == "" && azureOrg == "" && azureProject == "" && providerFlag == "" {
			return cmd.Help()
		}
		return run(cmd, args)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

var analyzeCmd = &cobra.Command{
	Use:   "analyze [owner/repo]",
	Short: "Analyze CI test failures",
	Long: `Analyzes CI test failures using AI to identify root causes.
Supports GitHub Actions and Azure Pipelines. Auto-detects provider from
repository URL or CI environment variables.`,
	Args:          cobra.MaximumNArgs(1),
	RunE:          run,
	SilenceUsage:  true,
	SilenceErrors: true,
	Example: `  # Analyze the latest failing run
  $ heisenberg analyze owner/repo

  # Analyze a specific CI run by URL
  $ heisenberg analyze owner/repo -r "https://github.com/owner/repo/actions/runs/123"

  # Auto-detect from CI environment
  $ heisenberg analyze --from-env

  # JSON output for CI integration
  $ heisenberg analyze --from-env -f json`,
}

var serveCmd = &cobra.Command{
	Use:           "serve",
	Short:         "Start the local web dashboard",
	RunE:          serve,
	SilenceUsage:  true,
	SilenceErrors: true,
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
	// Global flags (available to all subcommands via PersistentFlags)
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed tool call info")
	rootCmd.PersistentFlags().StringVarP(&format, "format", "f", "", "Output format: human or json (default: auto-detect from TTY)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output result as JSON (alias for --format json)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Write full agent conversation trace to a debug file")
	_ = rootCmd.PersistentFlags().MarkHidden("json") // backward compat alias

	// Analyze-specific flags (on analyzeCmd)
	analyzeCmd.Flags().BoolVar(&fromEnv, "from-env", false, "Read repo/run from CI environment variables (GitHub Actions or Azure Pipelines)")
	analyzeCmd.Flags().StringVarP(&runURL, "run", "r", "", "CI run URL (GitHub Actions or Azure Pipelines)")
	analyzeCmd.Flags().Int64Var(&runID, "run-id", 0, "Specific workflow run ID to analyze")
	analyzeCmd.Flags().StringVar(&modelName, "model", "", "Gemini model name (env: HEISENBERG_MODEL, default: "+llm.DefaultModel+")")
	analyzeCmd.Flags().StringVar(&providerFlag, "provider", "", "CI provider: github or azure (default: auto-detect)")
	analyzeCmd.Flags().StringVar(&azureOrg, "azure-org", "", "Azure DevOps organization")
	analyzeCmd.Flags().StringVar(&azureProject, "azure-project", "", "Azure DevOps project")
	analyzeCmd.Flags().StringVar(&testRepo, "azure-test-repo", "", "Additional repository for test code (project/repo)")

	// Deprecated aliases for renamed flags (on analyzeCmd)
	analyzeCmd.Flags().StringVar(&azureOrg, "org", "", "Azure DevOps organization")
	analyzeCmd.Flags().StringVar(&azureProject, "project", "", "Azure DevOps project")
	analyzeCmd.Flags().StringVar(&testRepo, "test-repo", "", "Additional repository for test code (project/repo)")
	_ = analyzeCmd.Flags().MarkDeprecated("org", "use --azure-org instead")
	_ = analyzeCmd.Flags().MarkDeprecated("project", "use --azure-project instead")
	_ = analyzeCmd.Flags().MarkDeprecated("test-repo", "use --azure-test-repo instead")

	// Hidden copies on rootCmd for backward compat (heisenberg owner/repo --from-env etc.)
	rootCmd.Flags().BoolVar(&fromEnv, "from-env", false, "")
	rootCmd.Flags().StringVarP(&runURL, "run", "r", "", "")
	rootCmd.Flags().Int64Var(&runID, "run-id", 0, "")
	rootCmd.Flags().StringVar(&modelName, "model", "", "")
	rootCmd.Flags().StringVar(&providerFlag, "provider", "", "")
	rootCmd.Flags().StringVar(&azureOrg, "azure-org", "", "")
	rootCmd.Flags().StringVar(&azureProject, "azure-project", "", "")
	rootCmd.Flags().StringVar(&testRepo, "azure-test-repo", "", "")
	rootCmd.Flags().StringVar(&azureOrg, "org", "", "")
	rootCmd.Flags().StringVar(&azureProject, "project", "", "")
	rootCmd.Flags().StringVar(&testRepo, "test-repo", "", "")
	_ = rootCmd.Flags().MarkDeprecated("org", "use --azure-org instead")
	_ = rootCmd.Flags().MarkDeprecated("project", "use --azure-project instead")
	_ = rootCmd.Flags().MarkDeprecated("test-repo", "use --azure-test-repo instead")
	hideRootFlags("from-env", "run", "run-id", "model", "provider",
		"azure-org", "azure-project", "azure-test-repo")

	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(doctorCmd)
}

// hideRootFlags marks flags as hidden on rootCmd (backward compat only, not shown in help).
func hideRootFlags(names ...string) {
	for _, name := range names {
		_ = rootCmd.Flags().MarkHidden(name)
	}
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
	format = resolveFormat(format, jsonOutput, isTerminal(os.Stdout))

	target, err := resolveTarget(args, runID)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v (using defaults)\n", err)
		cfg = &config.Config{}
	}

	modelName = resolveModel(modelName, cfg.Model)

	var emitter llm.ProgressEmitter
	textEmitter := llm.NewTextEmitter(os.Stderr, verbose)
	if debug {
		debugEmitter, debugErr := llm.NewDebugEmitter(textEmitter)
		if debugErr != nil {
			return debugErr
		}
		defer func() {
			debugEmitter.Close()
			fmt.Fprintf(os.Stderr, "  Debug log: %s\n", debugEmitter.Path())
		}()
		emitter = debugEmitter
	} else {
		emitter = textEmitter
	}

	ciProvider := buildProvider(target, cfg)

	result, err := analysis.Run(context.Background(), analysis.Params{
		Owner:        target.owner,
		Repo:         target.repo,
		RunID:        target.runID,
		Verbose:      verbose,
		Emitter:      emitter,
		SnapshotHTML: trace.SnapshotHTML,
		Model:        modelName,
		CI:           ciProvider,
		GoogleAPIKey: cfg.GoogleAPIKey,
	})
	if err != nil {
		textEmitter.MarkFailed()
		textEmitter.Close()
		return err
	}

	textEmitter.Close()

	// Persist to SaaS dashboard (if configured)
	if client := saas.NewClient(); client != nil {
		id, err := client.SubmitAnalysis(context.Background(), saas.SubmitParams{
			OrgID:     os.Getenv("HEISENBERG_ORG_ID"),
			Owner:     target.owner,
			Repo:      target.repo,
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

// targetInfo holds the resolved CI target (provider, identifiers, run ID).
type targetInfo struct {
	provider string // "github" or "azure"
	owner    string // GitHub owner or Azure org
	repo     string // GitHub repo or Azure project
	runID    int64
}

// resolveTarget determines the CI target from args, --from-env, --run URL, or explicit flags.
// flagRunID is the value of the --run-id flag.
func resolveTarget(args []string, flagRunID int64) (*targetInfo, error) {
	// 1. --run URL (auto-detects provider)
	if runURL != "" {
		return parseRunURL(runURL)
	}

	// 2. --from-env (detect from CI environment variables)
	if fromEnv {
		return resolveFromEnv(flagRunID)
	}

	// 3. Explicit Azure flags
	if providerFlag == "azure" || azureOrg != "" || azureProject != "" {
		if azureOrg == "" || azureProject == "" {
			return nil, fmt.Errorf("azure requires both --azure-org and --azure-project flags")
		}
		return &targetInfo{
			provider: "azure",
			owner:    azureOrg,
			repo:     azureProject,
			runID:    flagRunID,
		}, nil
	}

	// 4. Positional arg: owner/repo → GitHub
	if len(args) > 0 {
		parts := strings.SplitN(args[0], "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid repo format, use: owner/repo")
		}
		return &targetInfo{
			provider: "github",
			owner:    parts[0],
			repo:     parts[1],
			runID:    flagRunID,
		}, nil
	}

	return nil, fmt.Errorf("provide owner/repo, --from-env, or --run <URL>")
}

func resolveFromEnv(flagRunID int64) (*targetInfo, error) {
	hasGitHub := os.Getenv("GITHUB_REPOSITORY") != ""
	hasAzure := os.Getenv("SYSTEM_TEAMPROJECT") != ""

	// Ambiguous: both present without explicit --provider
	if hasGitHub && hasAzure && providerFlag == "" {
		return nil, fmt.Errorf("--from-env: ambiguous environment — both GitHub and Azure variables found. Use --provider to specify")
	}

	// Explicit provider override or single env
	if providerFlag == "azure" || (hasAzure && !hasGitHub) {
		return resolveAzureEnv(flagRunID)
	}

	return resolveGitHubEnv(flagRunID)
}

func resolveGitHubEnv(flagRunID int64) (*targetInfo, error) {
	ghRepo := os.Getenv("GITHUB_REPOSITORY")
	if ghRepo == "" {
		return nil, fmt.Errorf("--from-env: GITHUB_REPOSITORY not set")
	}
	parts := strings.SplitN(ghRepo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("--from-env: invalid GITHUB_REPOSITORY: %s", ghRepo)
	}

	target := &targetInfo{
		provider: "github",
		owner:    parts[0],
		repo:     parts[1],
		runID:    flagRunID,
	}

	if ghRunID := os.Getenv("GITHUB_RUN_ID"); ghRunID != "" && target.runID == 0 {
		if id, err := strconv.ParseInt(ghRunID, 10, 64); err == nil {
			target.runID = id
		}
	}
	return target, nil
}

func resolveAzureEnv(flagRunID int64) (*targetInfo, error) {
	project := os.Getenv("SYSTEM_TEAMPROJECT")
	if project == "" {
		return nil, fmt.Errorf("--from-env: SYSTEM_TEAMPROJECT not set")
	}

	collectionURI := os.Getenv("SYSTEM_TEAMFOUNDATIONCOLLECTIONURI")
	org := extractAzureOrg(collectionURI)
	if org == "" {
		return nil, fmt.Errorf("--from-env: could not extract organization from SYSTEM_TEAMFOUNDATIONCOLLECTIONURI: %s", collectionURI)
	}

	target := &targetInfo{
		provider: "azure",
		owner:    org,
		repo:     project,
		runID:    flagRunID,
	}

	if buildID := os.Getenv("BUILD_BUILDID"); buildID != "" && target.runID == 0 {
		if id, err := strconv.ParseInt(buildID, 10, 64); err == nil {
			target.runID = id
		}
	}
	return target, nil
}

// extractAzureOrg extracts the organization name from an Azure DevOps collection URI.
// e.g., "https://dev.azure.com/myorg/" → "myorg"
func extractAzureOrg(uri string) string {
	u, err := url.Parse(strings.TrimRight(uri, "/"))
	if err != nil || u.Host == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// runURLRe matches GitHub Actions run URLs.
var runURLRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/actions/runs/(\d+)`)

// parseRunURL extracts target info from a GitHub Actions or Azure Pipelines URL.
func parseRunURL(rawURL string) (*targetInfo, error) {
	// Try GitHub regex first (path-based URL)
	if m := runURLRe.FindStringSubmatch(rawURL); m != nil {
		target := &targetInfo{provider: "github", owner: m[1], repo: m[2]}
		if id, err := strconv.ParseInt(m[3], 10, 64); err == nil {
			target.runID = id
		}
		return target, nil
	}

	// Try Azure DevOps URL parsing
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %s", rawURL)
	}

	if strings.Contains(u.Host, "dev.azure.com") {
		return parseAzureDevURL(u)
	}
	if strings.HasSuffix(u.Host, ".visualstudio.com") {
		return parseAzureVSURL(u)
	}

	return nil, fmt.Errorf("unrecognized CI URL: %s\nSupported: GitHub Actions (github.com) or Azure Pipelines (dev.azure.com)", rawURL)
}

// parseAzureDevURL parses https://dev.azure.com/{org}/{project}/_build/results?buildId=123
func parseAzureDevURL(u *url.URL) (*targetInfo, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid Azure DevOps URL: expected dev.azure.com/{org}/{project}")
	}
	buildID := u.Query().Get("buildId")
	if buildID == "" {
		return nil, fmt.Errorf("invalid Azure DevOps URL: missing buildId parameter")
	}
	target := &targetInfo{provider: "azure", owner: parts[0], repo: parts[1]}
	if id, err := strconv.ParseInt(buildID, 10, 64); err == nil {
		target.runID = id
	}
	return target, nil
}

// parseAzureVSURL parses https://{org}.visualstudio.com/{project}/_build/results?buildId=123
func parseAzureVSURL(u *url.URL) (*targetInfo, error) {
	org := strings.TrimSuffix(u.Host, ".visualstudio.com")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		return nil, fmt.Errorf("invalid Azure DevOps URL: expected {org}.visualstudio.com/{project}")
	}
	buildID := u.Query().Get("buildId")
	if buildID == "" {
		return nil, fmt.Errorf("invalid Azure DevOps URL: missing buildId parameter")
	}
	target := &targetInfo{provider: "azure", owner: org, repo: parts[0]}
	if id, err := strconv.ParseInt(buildID, 10, 64); err == nil {
		target.runID = id
	}
	return target, nil
}

// buildProvider constructs the appropriate CI provider from target info and config.
func buildProvider(target *targetInfo, cfg *config.Config) ci.Provider {
	switch target.provider {
	case "azure":
		pat := os.Getenv("AZURE_DEVOPS_PAT")
		if pat == "" && cfg != nil {
			pat = cfg.AzureDevOpsPAT
		}
		client := azure.NewClient(target.owner, target.repo, pat)
		if testRepo != "" {
			parts := strings.SplitN(testRepo, "/", 2)
			var project, repo string
			if len(parts) == 2 {
				project = parts[0]
				repo = parts[1]
			} else {
				// Single name: use as both project and repo (Azure DevOps convention)
				project = parts[0]
				repo = parts[0]
			}
			client.ExtraRepos = []ci.RepoRef{{Project: project, Repo: repo}}
		}
		return client
	default:
		token := ""
		if cfg != nil {
			token = cfg.GitHubToken
		}
		return github.NewClient(target.owner, target.repo, token)
	}
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
	fixLabel := "  Fix"
	switch rca.FixConfidence {
	case "high":
		fixLabel = "  Fix (high confidence)"
	case "medium":
		fixLabel = "  Fix (medium confidence)"
	case "low":
		fixLabel = "  Fix (suggested direction)"
	}
	_, _ = fixColor.Fprintln(w, fixLabel)
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
