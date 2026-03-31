package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gh "github.com/kamilpajak/heisenberg/pkg/github"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/trace"
)

const testResultsArtifact = "test-results"

// ToolHandler executes tool calls on behalf of the agent loop.
type ToolHandler struct {
	GitHub       *gh.Client
	Owner        string
	Repo         string
	RunID        int64
	PRNumber     int    // detected PR number (0 = none)
	HeadSHA      string // commit SHA for compare fallback
	SnapshotHTML func([]byte) ([]byte, error)
	Emitter      llm.ProgressEmitter

	artifacts           []gh.Artifact           // cached after first list
	calledTraces        bool                    // whether get_test_traces has been called
	hasReadErrorContext bool                    // set after fetching logs/artifacts/traces
	category            string                  // set by done tool
	confidence          int                     // 0-100, set by done tool
	sensitivity         string                  // "high", "medium", "low", set by done tool
	rcas                []llm.RootCauseAnalysis // structured RCAs, set by done tool
}

// Execute dispatches a function call, returning the result string and whether
// the model signalled it is done (via the "done" tool).
func (h *ToolHandler) Execute(ctx context.Context, call llm.FunctionCall) (string, bool, error) {
	switch call.Name {
	case "list_jobs":
		return h.listJobs(ctx)
	case "get_job_logs":
		return h.getJobLogs(ctx, call.Args)
	case "get_artifact":
		return h.getArtifact(ctx, call.Args)
	case "get_workflow_file":
		return h.getRepoFile(ctx, call.Args)
	case "get_repo_file":
		return h.getRepoFile(ctx, call.Args)
	case "get_test_traces":
		return h.getTestTraces(ctx, call.Args)
	case "get_pr_diff":
		return h.getPRDiff(ctx, call.Args)
	case "done":
		return h.handleDone(call.Args)
	default:
		return fmt.Sprintf("unknown tool: %s", call.Name), false, nil
	}
}

func (h *ToolHandler) handleDone(args map[string]any) (string, bool, error) {
	h.category = stringArgOrDefault(args, "category", llm.CategoryDiagnosis)
	if h.category != llm.CategoryDiagnosis && h.category != llm.CategoryNoFailures && h.category != llm.CategoryNotSupported {
		h.category = llm.CategoryDiagnosis
	}
	h.confidence = intArgOrDefault(args, "confidence", 50)
	if h.confidence < 0 {
		h.confidence = 0
	} else if h.confidence > 100 {
		h.confidence = 100
	}
	h.sensitivity = stringArgOrDefault(args, "missing_information_sensitivity", "medium")
	if h.sensitivity != "high" && h.sensitivity != "medium" && h.sensitivity != "low" {
		h.sensitivity = "medium"
	}

	// Parse structured RCAs for diagnosis category
	if h.category == llm.CategoryDiagnosis {
		h.rcas = llm.ParseRCAsFromArgs(args)
	}

	return "", true, nil
}

func (h *ToolHandler) listJobs(ctx context.Context) (string, bool, error) {
	jobs, err := h.GitHub.ListJobs(ctx, h.Owner, h.Repo, h.RunID)
	if err != nil {
		return errorResult(err), false, nil
	}

	var lines []string
	for _, j := range jobs {
		lines = append(lines, fmt.Sprintf("- %s (id=%d, status=%s, conclusion=%s)", j.Name, j.ID, j.Status, j.Conclusion))
	}

	return strings.Join(lines, "\n"), false, nil
}

func (h *ToolHandler) getJobLogs(ctx context.Context, args map[string]any) (string, bool, error) {
	h.hasReadErrorContext = true

	jobID, err := intArg(args, "job_id")
	if err != nil {
		return errorResult(err), false, nil
	}

	logs, err := h.GitHub.GetJobLogs(ctx, h.Owner, h.Repo, jobID)
	if err != nil {
		return errorResult(err), false, nil
	}

	// Truncate large logs
	const maxLen = 80000
	if len(logs) > maxLen {
		logs = logs[len(logs)-maxLen:]
	}

	return logs, false, nil
}

func (h *ToolHandler) cacheArtifacts(ctx context.Context) error {
	if h.artifacts != nil {
		return nil
	}
	artifacts, err := h.GitHub.ListArtifacts(ctx, h.Owner, h.Repo, h.RunID)
	if err != nil {
		return err
	}
	h.artifacts = artifacts
	return nil
}

func (h *ToolHandler) findArtifactByName(name string) *gh.Artifact {
	for i, a := range h.artifacts {
		if a.Name == name {
			return &h.artifacts[i]
		}
	}
	return nil
}

func (h *ToolHandler) getArtifact(ctx context.Context, args map[string]any) (string, bool, error) {
	h.hasReadErrorContext = true

	name, _ := args["artifact_name"].(string)
	if name == "" {
		return errorResult(fmt.Errorf("artifact_name is required")), false, nil
	}

	if err := h.cacheArtifacts(ctx); err != nil {
		return errorResult(err), false, nil
	}

	artifact := h.findArtifactByName(name)
	if artifact == nil {
		return errorResult(fmt.Errorf("artifact %q not found", name)), false, nil
	}

	return h.fetchArtifactContent(ctx, artifact)
}

func (h *ToolHandler) fetchArtifactContent(ctx context.Context, artifact *gh.Artifact) (string, bool, error) {
	switch gh.ClassifyArtifact(artifact.Name) {
	case gh.ArtifactHTML:
		return h.fetchHTMLArtifact(ctx, artifact.ID)
	case gh.ArtifactBlob:
		return h.fetchBlobInfo(ctx, artifact)
	default:
		return h.fetchDefaultArtifact(ctx, artifact.ID)
	}
}

func (h *ToolHandler) fetchHTMLArtifact(ctx context.Context, artifactID int64) (string, bool, error) {
	content, err := h.GitHub.DownloadAndExtract(ctx, h.Owner, h.Repo, artifactID)
	if err != nil {
		return errorResult(err), false, nil
	}
	if h.SnapshotHTML == nil {
		return errorResult(fmt.Errorf("HTML rendering not available")), false, nil
	}
	snapshot, err := h.SnapshotHTML(content)
	if err != nil {
		return errorResult(err), false, nil
	}
	return string(snapshot), false, nil
}

func (h *ToolHandler) fetchBlobInfo(ctx context.Context, artifact *gh.Artifact) (string, bool, error) {
	zipData, err := h.GitHub.DownloadRawZip(ctx, h.Owner, h.Repo, artifact.ID)
	if err != nil {
		return errorResult(err), false, nil
	}
	return fmt.Sprintf("[blob-report: %d bytes downloaded, name=%s]", len(zipData), artifact.Name), false, nil
}

func (h *ToolHandler) fetchDefaultArtifact(ctx context.Context, artifactID int64) (string, bool, error) {
	content, err := h.GitHub.DownloadAndExtract(ctx, h.Owner, h.Repo, artifactID)
	if err != nil {
		return errorResult(err), false, nil
	}
	if len(content) > 100000 {
		content = content[:100000]
	}
	return string(content), false, nil
}

func (h *ToolHandler) getRepoFile(ctx context.Context, args map[string]any) (string, bool, error) {
	// Prerequisite gating: must fetch error context before reading source files
	if !h.hasReadErrorContext && h.HasTestArtifacts() {
		return errorResult(fmt.Errorf("ACCESS_DENIED: You must fetch test failure data (job logs, artifacts, or traces) before reading source code. Use get_job_logs, get_artifact, or get_test_traces first")), false, nil
	}

	path, _ := args["path"].(string)
	if path == "" {
		return errorResult(fmt.Errorf("path is required")), false, nil
	}

	content, err := h.GitHub.GetRepoFile(ctx, h.Owner, h.Repo, path)
	if err != nil {
		// Smart 404: if file not found, return directory listing of parent
		if strings.Contains(err.Error(), "404") {
			return h.smartNotFound(ctx, path), false, nil
		}
		return errorResult(err), false, nil
	}

	return content, false, nil
}

// smartNotFound returns an error with a directory listing of the parent folder.
func (h *ToolHandler) smartNotFound(ctx context.Context, path string) string {
	dir := "."
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		dir = path[:idx]
	}

	entries, err := h.GitHub.ListDirectory(ctx, h.Owner, h.Repo, dir)
	if err != nil || len(entries) == 0 {
		return errorResult(fmt.Errorf("file not found: %s", path))
	}

	// Limit to 30 entries to avoid huge responses
	if len(entries) > 30 {
		entries = entries[:30]
	}

	result := map[string]any{
		"error":     fmt.Sprintf("file not found: %s", path),
		"directory": dir + "/",
		"contents":  entries,
	}
	b, _ := json.Marshal(result)
	return string(b)
}

func (h *ToolHandler) findTraceArtifact(name string) *gh.Artifact {
	for i, a := range h.artifacts {
		if a.Expired {
			continue
		}
		if name != "" && a.Name == name {
			return &h.artifacts[i]
		}
		if name == "" && strings.Contains(strings.ToLower(a.Name), testResultsArtifact) {
			return &h.artifacts[i]
		}
	}
	return nil
}

func (h *ToolHandler) getTestTraces(ctx context.Context, args map[string]any) (string, bool, error) {
	h.hasReadErrorContext = true
	h.calledTraces = true
	name, _ := args["artifact_name"].(string)

	if err := h.cacheArtifacts(ctx); err != nil {
		return errorResult(err), false, nil
	}

	artifact := h.findTraceArtifact(name)
	if artifact == nil {
		if name != "" {
			return errorResult(fmt.Errorf("artifact %q not found", name)), false, nil
		}
		return errorResult(fmt.Errorf("no %s artifact found", testResultsArtifact)), false, nil
	}

	zipData, err := h.GitHub.DownloadRawZip(ctx, h.Owner, h.Repo, artifact.ID)
	if err != nil {
		return errorResult(err), false, nil
	}

	traces, err := trace.ParseArtifact(zipData)
	if err != nil {
		return errorResult(err), false, nil
	}

	result := trace.FormatSummary(traces)

	const maxLen = 100000
	if len(result) > maxLen {
		result = result[:maxLen] + "\n... (truncated)"
	}

	return result, false, nil
}

// HasPendingTraces reports whether test-results artifacts exist but
// get_test_traces has not been called yet.
func (h *ToolHandler) HasPendingTraces() bool {
	if h.calledTraces {
		return false
	}
	for _, a := range h.artifacts {
		if !a.Expired && strings.Contains(strings.ToLower(a.Name), testResultsArtifact) {
			return true
		}
	}
	return false
}

// DiagnosisCategory returns the outcome category set by the done tool, or "" if done was not called.
func (h *ToolHandler) DiagnosisCategory() string { return h.category }

// DiagnosisConfidence returns the confidence score (0-100) set by the done tool.
func (h *ToolHandler) DiagnosisConfidence() int { return h.confidence }

// DiagnosisSensitivity returns the missing information sensitivity set by the done tool.
func (h *ToolHandler) DiagnosisSensitivity() string { return h.sensitivity }

// DiagnosisRCAs returns the structured root cause analyses set by the done tool.
func (h *ToolHandler) DiagnosisRCAs() []llm.RootCauseAnalysis { return h.rcas }

// GetEmitter returns the progress emitter for this handler.
func (h *ToolHandler) GetEmitter() llm.ProgressEmitter { return h.Emitter }

// HasTestArtifacts returns true if non-expired test-related artifacts exist.
func (h *ToolHandler) HasTestArtifacts() bool {
	for _, a := range h.artifacts {
		if a.Expired {
			continue
		}
		name := strings.ToLower(a.Name)
		if strings.Contains(name, "report") || strings.Contains(name, testResultsArtifact) ||
			strings.Contains(name, "blob") {
			return true
		}
	}
	return false
}

func errorResult(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}

func intArg(args map[string]any, key string) (int64, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	// JSON numbers are float64
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case json.Number:
		return n.Int64()
	default:
		return 0, fmt.Errorf("%s must be a number, got %T", key, v)
	}
}

func intArgOrDefault(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return def
		}
		return int(i)
	default:
		return def
	}
}

func stringArgOrDefault(args map[string]any, key string, def string) string {
	v, ok := args[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	return s
}

// classifyFilePath categorizes a file path as test, production, config, or other.
func classifyFilePath(path string) string {
	lower := strings.ToLower(path)

	// Ignored files → other
	for _, pattern := range []string{"lock.json", "lock.yaml", "lock.yml", ".md", ".png", ".svg", ".jpg", ".gif", "assets/", "docs/"} {
		if strings.Contains(lower, pattern) {
			return "other"
		}
	}

	// Config
	for _, pattern := range []string{".github/", "dockerfile", ".yml", ".yaml", "makefile", "docker-compose"} {
		if strings.Contains(lower, pattern) {
			return "config"
		}
	}

	// Test
	for _, pattern := range []string{"test", ".spec.", "_test.go", "fixture"} {
		if strings.Contains(lower, pattern) {
			return "test"
		}
	}

	// Production code
	for _, pattern := range []string{"src/", "app/", "pkg/", "lib/", "internal/", "cmd/", ".go", ".ts", ".js", ".py", ".rb", ".rs"} {
		if strings.Contains(lower, pattern) {
			return "production"
		}
	}

	return "other"
}

type prDiffResponse struct {
	Kind       string     `json:"kind"`
	PRNumber   int        `json:"pr_number,omitempty"`
	TotalFiles int        `json:"total_files"`
	Files      []diffFile `json:"files"`
}

type diffFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Category  string `json:"category"`
	Patch     string `json:"patch,omitempty"`
}

func (h *ToolHandler) getPRDiff(ctx context.Context, args map[string]any) (string, bool, error) {
	files, kind := h.fetchDiffFiles(ctx)

	resp := prDiffResponse{Kind: kind, PRNumber: h.PRNumber}
	suspectedFiles := parseSuspectedFiles(args)
	resp.Files = buildDiffFiles(files, suspectedFiles)
	resp.TotalFiles = len(resp.Files)

	data, _ := json.Marshal(resp)
	return string(data), false, nil
}

func (h *ToolHandler) fetchDiffFiles(ctx context.Context) ([]gh.PRFile, string) {
	if h.PRNumber > 0 {
		files, err := h.GitHub.GetPRFiles(ctx, h.Owner, h.Repo, h.PRNumber)
		if err == nil {
			return files, "pull_request"
		}
	}
	if h.HeadSHA != "" {
		files, err := h.GitHub.CompareCommits(ctx, h.Owner, h.Repo, "main", h.HeadSHA)
		if err == nil {
			return files, "commit_range"
		}
	}
	return nil, "none"
}

func parseSuspectedFiles(args map[string]any) map[string]bool {
	result := map[string]bool{}
	if raw, ok := args["suspected_files"].([]any); ok {
		for _, f := range raw {
			if s, ok := f.(string); ok {
				result[s] = true
			}
		}
	}
	return result
}

func buildDiffFiles(files []gh.PRFile, suspected map[string]bool) []diffFile {
	const maxPatchTokens = 4000
	budget := maxPatchTokens
	var result []diffFile

	for _, f := range files {
		cat := classifyFilePath(f.Path)
		if cat == "other" {
			continue
		}

		df := diffFile{
			Path: f.Path, Status: f.Status,
			Additions: f.Additions, Deletions: f.Deletions,
			Category: cat,
		}

		patchLen := len(f.Patch) / 4
		if f.Patch != "" && budget > 0 && (suspected[f.Path] || patchLen < 500) && patchLen <= budget {
			df.Patch = f.Patch
			budget -= patchLen
		}

		result = append(result, df)
	}
	return result
}

// ToolDeclarations returns the function declarations for all available tools.
func ToolDeclarations() []llm.FunctionDeclaration {
	return []llm.FunctionDeclaration{
		{
			Name:        "list_jobs",
			Description: "List all jobs in the workflow run with their status and conclusion.",
		},
		{
			Name:        "get_job_logs",
			Description: "Fetch the plain-text log output for a specific job. Use this to see error messages, stack traces, and test output.",
			Parameters: &llm.Schema{
				Type: "object",
				Properties: map[string]llm.Schema{
					"job_id": {Type: "number"},
				},
				Required: []string{"job_id"},
			},
		},
		{
			Name:        "get_artifact",
			Description: "Download and extract a test artifact by name. For HTML reports, returns the rendered page text. For JSON, returns the raw content.",
			Parameters: &llm.Schema{
				Type: "object",
				Properties: map[string]llm.Schema{
					"artifact_name": {Type: "string"},
				},
				Required: []string{"artifact_name"},
			},
		},
		{
			Name:        "get_workflow_file",
			Description: "Fetch a workflow YAML file from the repository (e.g. .github/workflows/ci.yml).",
			Parameters: &llm.Schema{
				Type: "object",
				Properties: map[string]llm.Schema{
					"path": {Type: "string"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        "get_repo_file",
			Description: "Fetch any file from the repository by path (e.g. package.json, playwright.config.ts).",
			Parameters: &llm.Schema{
				Type: "object",
				Properties: map[string]llm.Schema{
					"path": {Type: "string"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        "get_test_traces",
			Description: "Download a Playwright test-results artifact and extract trace data: browser action sequence, console errors, failed HTTP requests, and error context snapshots. Use this for detailed failure analysis.",
			Parameters: &llm.Schema{
				Type: "object",
				Properties: map[string]llm.Schema{
					"artifact_name": {Type: "string"},
				},
			},
		},
		{
			Name:        "get_pr_diff",
			Description: "Get the diff of changed files for this PR/commit. Returns file list with categories (test/production/config) and optional patch hunks. Use this before determining bug_location. Pass suspected_files from stack traces to prioritize their patches.",
			Parameters: &llm.Schema{
				Type: "object",
				Properties: map[string]llm.Schema{
					"suspected_files": {
						Type:        "array",
						Description: "File paths from stack traces or logs to prioritize in the diff.",
						Items:       &llm.Schema{Type: "string"},
					},
				},
			},
		},
		{
			Name:        "done",
			Description: "Signal that you have gathered enough information and provide structured Root Cause Analysis. Provide one entry in 'analyses' for each distinct failing test. After calling this, provide your final analysis as text.",
			Parameters: &llm.Schema{
				Type: "object",
				Properties: map[string]llm.Schema{
					"category": {
						Type:        "string",
						Description: "The type of conclusion reached. diagnosis: a specific failure root cause was identified. no_failures: all tests are passing, no failures to diagnose. not_supported: the test framework or artifact format is not supported for analysis.",
						Enum:        []string{"diagnosis", "no_failures", "not_supported"},
					},
					"analyses": {
						Type:        "array",
						Description: "One structured RCA per distinct failing test. Required for diagnosis category. Group tests that share the same root cause into one entry.",
						Items: &llm.Schema{
							Type: "object",
							Properties: map[string]llm.Schema{
								"title": {
									Type:        "string",
									Description: "Short summary of the failure (e.g., 'Timeout waiting for Submit Button').",
								},
								"failure_type": {
									Type:        "string",
									Description: "Type of failure: timeout (test timed out), assertion (assertion failed), network (HTTP/API error), infra (CI/environment issue), flake (intermittent/race condition).",
									Enum:        []string{"timeout", "assertion", "network", "infra", "flake"},
								},
								"file_path": {
									Type:        "string",
									Description: "Path to the test file where failure occurred (e.g., 'tests/checkout.spec.ts').",
								},
								"line_number": {
									Type:        "number",
									Description: "Line number in the test file where failure occurred.",
								},
								"bug_location": {
									Type:        "string",
									Description: "Where the underlying defect lives. 'test': bug in test code/fixtures/mocks. 'production': test correctly detected a regression in application code. 'infrastructure': CI environment issue. 'unknown': not enough evidence.",
									Enum:        []string{"test", "production", "infrastructure", "unknown"},
								},
								"bug_location_confidence": {
									Type:        "string",
									Description: "Confidence in bug_location. 'high': strong evidence. 'medium': probable. 'low': mostly guessing.",
									Enum:        []string{"high", "medium", "low"},
								},
								"bug_code_file_path": {
									Type:        "string",
									Description: "When bug_location is 'production' or 'infrastructure', the suspected defect file path (e.g., 'src/pricing.ts'). Leave empty if unclear.",
								},
								"bug_code_line_number": {
									Type:        "number",
									Description: "Line number in the suspected bug file. Leave empty if unclear.",
								},
								"symptom": {
									Type:        "string",
									Description: "What failed - the observable error message or behavior.",
								},
								"root_cause": {
									Type:        "string",
									Description: "Why it failed - the underlying issue that caused the failure.",
								},
								"evidence": {
									Type:        "array",
									Description: "Supporting data points. Each item has 'type' (screenshot/trace/log/network/code) and 'content' (description).",
									Items: &llm.Schema{
										Type: "object",
										Properties: map[string]llm.Schema{
											"type":    {Type: "string", Description: "Evidence type: screenshot, trace, log, network, or code"},
											"content": {Type: "string", Description: "Description of the evidence"},
										},
									},
								},
								"remediation": {
									Type:        "string",
									Description: "How to fix it - actionable guidance for resolving the issue.",
								},
								"fix_confidence": {
									Type:        "string",
									Description: "How actionable the remediation is. high: inspected source, specific fix. medium: correct direction, details may vary. low: general suggestion without source inspection.",
									Enum:        []string{"high", "medium", "low"},
								},
							},
						},
					},
					"confidence": {
						Type:        "number",
						Description: "Diagnosis confidence score from 0 to 100. 80-100: clear root cause. 40-79: likely cause with ambiguity. 0-39: uncertain.",
					},
					"missing_information_sensitivity": {
						Type:        "string",
						Description: "How much additional data would improve the diagnosis. high: backend logs would help. medium: might help. low: sufficient evidence.",
						Enum:        []string{"high", "medium", "low"},
					},
				},
				Required: []string{"category"},
			},
		},
	}
}
