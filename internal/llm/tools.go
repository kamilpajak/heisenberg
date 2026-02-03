package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gh "github.com/kamilpajak/heisenberg/internal/github"
	"github.com/kamilpajak/heisenberg/internal/trace"
)

// ToolHandler executes tool calls on behalf of the agent loop.
type ToolHandler struct {
	GitHub       *gh.Client
	Owner        string
	Repo         string
	RunID        int64
	SnapshotHTML func([]byte) ([]byte, error)

	artifacts      []gh.Artifact // cached after first list
	calledTraces   bool          // whether get_test_traces has been called
	category       string        // set by done tool
	confidence     int           // 0-100, set by done tool
	sensitivity    string        // "high", "medium", "low", set by done tool
}

// Execute dispatches a function call, returning the result string and whether
// the model signalled it is done (via the "done" tool).
func (h *ToolHandler) Execute(ctx context.Context, call FunctionCall) (string, bool, error) {
	switch call.Name {
	case "list_jobs":
		return h.listJobs(ctx)
	case "get_job_logs":
		return h.getJobLogs(ctx, call.Args)
	case "get_artifact":
		return h.getArtifact(ctx, call.Args)
	case "get_workflow_file":
		return h.getWorkflowFile(ctx, call.Args)
	case "get_repo_file":
		return h.getRepoFile(ctx, call.Args)
	case "get_test_traces":
		return h.getTestTraces(ctx, call.Args)
	case "done":
		h.category = stringArgOrDefault(call.Args, "category", CategoryDiagnosis)
		if h.category != CategoryDiagnosis && h.category != CategoryNoFailures && h.category != CategoryNotSupported {
			h.category = CategoryDiagnosis
		}
		h.confidence = intArgOrDefault(call.Args, "confidence", 50)
		h.sensitivity = stringArgOrDefault(call.Args, "missing_information_sensitivity", "medium")
		if h.sensitivity != "high" && h.sensitivity != "medium" && h.sensitivity != "low" {
			h.sensitivity = "medium"
		}
		return "", true, nil
	default:
		return fmt.Sprintf("unknown tool: %s", call.Name), false, nil
	}
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

func (h *ToolHandler) getArtifact(ctx context.Context, args map[string]any) (string, bool, error) {
	name, _ := args["artifact_name"].(string)
	if name == "" {
		return errorResult(fmt.Errorf("artifact_name is required")), false, nil
	}

	// Cache artifacts list
	if h.artifacts == nil {
		artifacts, err := h.GitHub.ListArtifacts(ctx, h.Owner, h.Repo, h.RunID)
		if err != nil {
			return errorResult(err), false, nil
		}
		h.artifacts = artifacts
	}

	// Find matching artifact
	var artifact *gh.Artifact
	for i, a := range h.artifacts {
		if a.Name == name {
			artifact = &h.artifacts[i]
			break
		}
	}
	if artifact == nil {
		return errorResult(fmt.Errorf("artifact %q not found", name)), false, nil
	}

	artifactType := gh.ClassifyArtifact(artifact.Name)

	switch artifactType {
	case gh.ArtifactHTML:
		content, err := h.GitHub.DownloadAndExtract(ctx, h.Owner, h.Repo, artifact.ID)
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

	case gh.ArtifactBlob:
		zipData, err := h.GitHub.DownloadRawZip(ctx, h.Owner, h.Repo, artifact.ID)
		if err != nil {
			return errorResult(err), false, nil
		}
		return fmt.Sprintf("[blob-report: %d bytes downloaded, name=%s]", len(zipData), artifact.Name), false, nil

	default:
		content, err := h.GitHub.DownloadAndExtract(ctx, h.Owner, h.Repo, artifact.ID)
		if err != nil {
			return errorResult(err), false, nil
		}
		// Truncate large content
		if len(content) > 100000 {
			content = content[:100000]
		}
		return string(content), false, nil
	}
}

func (h *ToolHandler) getWorkflowFile(ctx context.Context, args map[string]any) (string, bool, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult(fmt.Errorf("path is required")), false, nil
	}

	content, err := h.GitHub.GetRepoFile(ctx, h.Owner, h.Repo, path)
	if err != nil {
		return errorResult(err), false, nil
	}

	return content, false, nil
}

func (h *ToolHandler) getRepoFile(ctx context.Context, args map[string]any) (string, bool, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult(fmt.Errorf("path is required")), false, nil
	}

	content, err := h.GitHub.GetRepoFile(ctx, h.Owner, h.Repo, path)
	if err != nil {
		return errorResult(err), false, nil
	}

	return content, false, nil
}

func (h *ToolHandler) getTestTraces(ctx context.Context, args map[string]any) (string, bool, error) {
	h.calledTraces = true
	name, _ := args["artifact_name"].(string)

	// Cache artifacts list
	if h.artifacts == nil {
		artifacts, err := h.GitHub.ListArtifacts(ctx, h.Owner, h.Repo, h.RunID)
		if err != nil {
			return errorResult(err), false, nil
		}
		h.artifacts = artifacts
	}

	// Find matching artifact: exact name or first "test-results*"
	var artifact *gh.Artifact
	for i, a := range h.artifacts {
		if a.Expired {
			continue
		}
		if name != "" && a.Name == name {
			artifact = &h.artifacts[i]
			break
		}
		if name == "" && strings.Contains(strings.ToLower(a.Name), "test-results") {
			artifact = &h.artifacts[i]
			break
		}
	}
	if artifact == nil {
		if name != "" {
			return errorResult(fmt.Errorf("artifact %q not found", name)), false, nil
		}
		return errorResult(fmt.Errorf("no test-results artifact found")), false, nil
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
		if !a.Expired && strings.Contains(strings.ToLower(a.Name), "test-results") {
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

// ToolDeclarations returns the function declarations for all available tools.
func ToolDeclarations() []FunctionDeclaration {
	return []FunctionDeclaration{
		{
			Name:        "list_jobs",
			Description: "List all jobs in the workflow run with their status and conclusion.",
		},
		{
			Name:        "get_job_logs",
			Description: "Fetch the plain-text log output for a specific job. Use this to see error messages, stack traces, and test output.",
			Parameters: &Schema{
				Type: "object",
				Properties: map[string]Schema{
					"job_id": {Type: "number"},
				},
				Required: []string{"job_id"},
			},
		},
		{
			Name:        "get_artifact",
			Description: "Download and extract a test artifact by name. For HTML reports, returns the rendered page text. For JSON, returns the raw content.",
			Parameters: &Schema{
				Type: "object",
				Properties: map[string]Schema{
					"artifact_name": {Type: "string"},
				},
				Required: []string{"artifact_name"},
			},
		},
		{
			Name:        "get_workflow_file",
			Description: "Fetch a workflow YAML file from the repository (e.g. .github/workflows/ci.yml).",
			Parameters: &Schema{
				Type: "object",
				Properties: map[string]Schema{
					"path": {Type: "string"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        "get_repo_file",
			Description: "Fetch any file from the repository by path (e.g. package.json, playwright.config.ts).",
			Parameters: &Schema{
				Type: "object",
				Properties: map[string]Schema{
					"path": {Type: "string"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        "get_test_traces",
			Description: "Download a Playwright test-results artifact and extract trace data: browser action sequence, console errors, failed HTTP requests, and error context snapshots. Use this for detailed failure analysis.",
			Parameters: &Schema{
				Type: "object",
				Properties: map[string]Schema{
					"artifact_name": {Type: "string"},
				},
			},
		},
		{
			Name:        "done",
			Description: "Signal that you have gathered enough information. After calling this, provide your final analysis as text.",
			Parameters: &Schema{
				Type: "object",
				Properties: map[string]Schema{
					"category": {
						Type:        "string",
						Description: "The type of conclusion reached. diagnosis: a specific failure root cause was identified. no_failures: all tests are passing, no failures to diagnose. not_supported: the test framework or artifact format is not supported for analysis.",
						Enum:        []string{"diagnosis", "no_failures", "not_supported"},
					},
					"confidence": {
						Type:        "number",
						Description: "Diagnosis confidence score from 0 to 100. Only meaningful when category is 'diagnosis'. 80-100: clear root cause identified with strong evidence. 40-79: likely cause identified but some ambiguity remains. 0-39: uncertain, multiple possible causes or insufficient evidence.",
					},
					"missing_information_sensitivity": {
						Type:        "string",
						Description: "How much additional data (backend logs, Docker containers, CI environment) would improve the diagnosis. Only meaningful when category is 'diagnosis'. high: additional data would likely reveal the root cause. medium: additional data might help but current evidence is reasonable. low: diagnosis is based on sufficient frontend/test evidence.",
						Enum:        []string{"high", "medium", "low"},
					},
				},
				Required: []string{"category"},
			},
		},
	}
}
