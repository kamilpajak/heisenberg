package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/kamilpajak/heisenberg/pkg/ci"
)

const (
	authHeaderPrefix = "Bearer "
	githubAcceptType = "application/vnd.github+json"
)

// Client handles GitHub API interactions and implements ci.Provider.
type Client struct {
	owner      string
	repo       string
	token      string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new GitHub client bound to a specific repository.
// If token is empty, falls back to the GITHUB_TOKEN environment variable.
func NewClient(owner, repo, token string) *Client {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return &Client{
		owner:      owner,
		repo:       repo,
		token:      token,
		httpClient: &http.Client{},
		baseURL:    "https://api.github.com",
	}
}

// NewTestClient creates a client for testing with custom baseURL and httpClient.
func NewTestClient(owner, repo, baseURL string, httpClient *http.Client) *Client {
	return &Client{
		owner:      owner,
		repo:       repo,
		token:      "test-token",
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// Name returns the provider identifier.
func (c *Client) Name() string { return "github" }

// AnalysisHints returns GitHub-specific strategy hints for the LLM.
func (c *Client) AnalysisHints() string {
	return `GitHub Actions specific notes:
- For Playwright test failures, fetch the HTML report artifact first using get_artifact
- Use get_test_traces on test-results artifacts to get browser actions, console errors, and failed HTTP requests from Playwright trace recordings
- Workflow files are in .github/workflows/`
}

// ClassifyArtifact returns the artifact type based on its name.
func ClassifyArtifact(name string) ci.ArtifactType {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "html-report") || n == "playwright-report":
		return ci.ArtifactHTML
	case strings.HasSuffix(n, ".json") || strings.Contains(n, "json"):
		return ci.ArtifactJSON
	case strings.Contains(n, "blob-report"):
		return ci.ArtifactBlob
	default:
		return ""
	}
}

// Internal types for JSON deserialization of GitHub API responses.

type workflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	HeadBranch   string `json:"head_branch"`
	HeadSHA      string `json:"head_sha"`
	Event        string `json:"event"`
	Path         string `json:"path"`
	DisplayTitle string `json:"display_title"`
	CreatedAt    string `json:"created_at"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

// mapConclusion normalizes GitHub conclusion values. GitHub uses "timed_out"
// for jobs that exceed their timeout, which should be treated as a failure.
func mapConclusion(conclusion string) string {
	switch conclusion {
	case "success":
		return ci.ConclusionSuccess
	case "failure", "timed_out":
		return ci.ConclusionFailure
	default:
		return conclusion
	}
}

func (r *workflowRun) toCIRun() ci.Run {
	run := ci.Run{
		ID:           r.ID,
		Name:         r.Name,
		Status:       r.Status,
		Conclusion:   mapConclusion(r.Conclusion),
		Branch:       r.HeadBranch,
		CommitSHA:    r.HeadSHA,
		Event:        r.Event,
		Path:         r.Path,
		DisplayTitle: r.DisplayTitle,
		CreatedAt:    r.CreatedAt,
	}
	for _, pr := range r.PullRequests {
		run.PRNumbers = append(run.PRNumbers, pr.Number)
	}
	return run
}

type job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

func (j *job) toCIJob() ci.Job {
	return ci.Job{
		ID:         j.ID,
		Name:       j.Name,
		Status:     j.Status,
		Conclusion: mapConclusion(j.Conclusion),
	}
}

type artifact struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_in_bytes"`
	Expired   bool   `json:"expired"`
}

func (a *artifact) toCIArtifact() ci.Artifact {
	return ci.Artifact{
		ID:        a.ID,
		Name:      a.Name,
		SizeBytes: a.SizeBytes,
		Expired:   a.Expired,
	}
}

type prFile struct {
	Path      string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

func (f *prFile) toCIChangedFile() ci.ChangedFile {
	return ci.ChangedFile{
		Path:      f.Path,
		Status:    f.Status,
		Additions: f.Additions,
		Deletions: f.Deletions,
		Patch:     f.Patch,
	}
}

// ListRuns returns recent completed workflow runs.
func (c *Client) ListRuns(ctx context.Context, filter ci.RunFilter) ([]ci.Run, error) {
	status := filter.Status
	if status == "" {
		status = "completed"
	}
	perPage := filter.PerPage
	if perPage <= 0 {
		perPage = 10
	}
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=%d&status=%s", c.baseURL, c.owner, c.repo, perPage, status)

	var result struct {
		WorkflowRuns []workflowRun `json:"workflow_runs"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	runs := make([]ci.Run, len(result.WorkflowRuns))
	for i := range result.WorkflowRuns {
		runs[i] = result.WorkflowRuns[i].toCIRun()
	}
	return runs, nil
}

// GetRun fetches a single workflow run by ID.
func (c *Client) GetRun(ctx context.Context, runID int64) (*ci.Run, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", c.baseURL, c.owner, c.repo, runID)

	var run workflowRun
	if err := c.doRequest(ctx, url, &run); err != nil {
		return nil, err
	}

	r := run.toCIRun()
	return &r, nil
}

// ListJobs returns jobs for a workflow run.
func (c *Client) ListJobs(ctx context.Context, runID int64) ([]ci.Job, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/jobs", c.baseURL, c.owner, c.repo, runID)

	var result struct {
		Jobs []job `json:"jobs"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	jobs := make([]ci.Job, len(result.Jobs))
	for i := range result.Jobs {
		jobs[i] = result.Jobs[i].toCIJob()
	}
	return jobs, nil
}

// ListArtifacts returns artifacts for a workflow run.
func (c *Client) ListArtifacts(ctx context.Context, runID int64) ([]ci.Artifact, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/artifacts", c.baseURL, c.owner, c.repo, runID)

	var result struct {
		Artifacts []artifact `json:"artifacts"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	artifacts := make([]ci.Artifact, len(result.Artifacts))
	for i := range result.Artifacts {
		artifacts[i] = result.Artifacts[i].toCIArtifact()
	}
	return artifacts, nil
}

// GetJobLogs fetches the plain-text logs for a job.
func (c *Client) GetJobLogs(ctx context.Context, jobID int64) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", c.baseURL, c.owner, c.repo, jobID)

	req, err := c.newAPIRequest(ctx, url)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("job logs: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// DownloadArtifact downloads an artifact as a raw ZIP.
func (c *Client) DownloadArtifact(ctx context.Context, artifactID int64) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/artifacts/%d/zip", c.baseURL, c.owner, c.repo, artifactID)

	req, err := c.newAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// DownloadAndExtract downloads an artifact ZIP and extracts its first file.
func (c *Client) DownloadAndExtract(ctx context.Context, artifactID int64) ([]byte, error) {
	zipData, err := c.DownloadArtifact(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	return ci.ExtractFirstFile(zipData)
}

// GetRepoFile fetches a file from the repo via the contents API.
// When ref is non-empty, the file is fetched at that specific commit SHA.
func (c *Client) GetRepoFile(ctx context.Context, path, ref string) (string, error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, c.owner, c.repo, path)
	if ref != "" {
		reqURL += "?ref=" + url.QueryEscape(ref)
	}

	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}

	if err := c.doRequest(ctx, reqURL, &result); err != nil {
		return "", err
	}

	if result.Encoding != "base64" {
		return result.Content, nil
	}

	decoded, err := base64Decode(result.Content)
	if err != nil {
		return "", fmt.Errorf("failed to decode file: %w", err)
	}

	return string(decoded), nil
}

// ListDirectory returns the names of files and directories at the given path.
// Directory names have a trailing "/".
func (c *Client) ListDirectory(ctx context.Context, path string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, c.owner, c.repo, path)

	req, err := c.newAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("directory not found: %s", path)
	}

	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"` // "file" or "dir"
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to parse directory listing: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type == "dir" {
			names = append(names, e.Name+"/")
		} else {
			names = append(names, e.Name)
		}
	}
	return names, nil
}

// GetChangedFiles returns files changed in a PR or commit comparison.
func (c *Client) GetChangedFiles(ctx context.Context, ref ci.ChangeRef) ([]ci.ChangedFile, error) {
	if ref.PRNumber > 0 {
		files, err := c.getPRFiles(ctx, ref.PRNumber)
		if err == nil {
			return files, nil
		}
	}
	if ref.HeadSHA != "" {
		base := ref.BaseBranch
		if base == "" {
			base = "main"
		}
		return c.compareCommits(ctx, base, ref.HeadSHA)
	}
	return nil, fmt.Errorf("no PR number or commit SHA provided")
}

func (c *Client) getPRFiles(ctx context.Context, prNumber int) ([]ci.ChangedFile, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files", c.baseURL, c.owner, c.repo, prNumber)
	var files []prFile
	if err := c.doRequest(ctx, url, &files); err != nil {
		return nil, err
	}
	result := make([]ci.ChangedFile, len(files))
	for i := range files {
		result[i] = files[i].toCIChangedFile()
	}
	return result, nil
}

func (c *Client) compareCommits(ctx context.Context, base, head string) ([]ci.ChangedFile, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/compare/%s...%s", c.baseURL, c.owner, c.repo, base, head)
	var result struct {
		Files []prFile `json:"files"`
	}
	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}
	files := make([]ci.ChangedFile, len(result.Files))
	for i := range result.Files {
		files[i] = result.Files[i].toCIChangedFile()
	}
	return files, nil
}

// HTTP helpers

func (c *Client) newAPIRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", authHeaderPrefix+c.token)
	}
	req.Header.Set("Accept", githubAcceptType)
	return req, nil
}

func (c *Client) doRequest(ctx context.Context, url string, result interface{}) error {
	req, err := c.newAPIRequest(ctx, url)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error: %s - %s", resp.Status, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func base64Decode(s string) ([]byte, error) {
	// GitHub returns base64 with newlines
	s = strings.ReplaceAll(s, "\n", "")
	return base64.StdEncoding.DecodeString(s)
}
