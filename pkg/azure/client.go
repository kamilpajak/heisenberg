// Package azure implements the ci.Provider interface for Azure DevOps Pipelines.
package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/kamilpajak/heisenberg/pkg/ci"
)

const (
	apiVersionKey   = "api-version"
	apiVersionValue = "7.1"
)

// Client handles Azure DevOps API interactions and implements ci.Provider
// and ci.TestResultsProvider.
type Client struct {
	org        string
	project    string
	pat        string
	httpClient *http.Client
	baseURL    string

	// Cached artifact download URLs, populated by ListArtifacts.
	// Key: artifact ID, Value: download URL.
	artifactURLs map[int64]string

	// Cached discovered repos from pipeline definition.
	discoveredRepos []ci.RepoRef
	reposDiscovered bool

	// Extra repos pre-seeded via --test-repo flag.
	ExtraRepos []ci.RepoRef
}

// NewClient creates a new Azure DevOps client bound to a specific org/project.
// If pat is empty, falls back to the AZURE_DEVOPS_PAT environment variable.
func NewClient(org, project, pat string) *Client {
	if pat == "" {
		pat = os.Getenv("AZURE_DEVOPS_PAT")
	}
	return &Client{
		org:        org,
		project:    project,
		pat:        pat,
		httpClient: &http.Client{},
		baseURL:    "https://dev.azure.com/" + org,
	}
}

// NewTestClient creates a client for testing with custom baseURL and httpClient.
func NewTestClient(org, project, baseURL string, httpClient *http.Client) *Client {
	return &Client{
		org:        org,
		project:    project,
		pat:        "test-token",
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// Name returns the provider identifier.
func (c *Client) Name() string { return "azure" }

// AnalysisHints returns Azure-specific strategy hints for the LLM.
func (c *Client) AnalysisHints() string {
	return `Azure Pipelines specific notes:
- IMPORTANT: Call get_test_results FIRST before reading any logs. It returns structured test failures with error messages and stack traces directly from Azure's Test Results API.
- Use job logs only for additional context after reviewing structured test results.
- Pipeline definition files are typically in the repo root or a pipelines/ directory.
- If a test file path from stack traces is not found in this repository, the tool will automatically search additional repositories used by this pipeline.`
}

// Internal types for JSON deserialization of Azure DevOps API responses.

type build struct {
	ID            int    `json:"id"`
	BuildNumber   string `json:"buildNumber"`
	Status        string `json:"status"`
	Result        string `json:"result"`
	SourceBranch  string `json:"sourceBranch"`
	SourceVersion string `json:"sourceVersion"`
	Reason        string `json:"reason"`
	QueueTime     string `json:"queueTime"`
	Definition    struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"definition"`
}

var prBranchRe = regexp.MustCompile(`^refs/pull/(\d+)/merge$`)

func (b *build) toCIRun() ci.Run {
	run := ci.Run{
		ID:           int64(b.ID),
		Name:         b.Definition.Name,
		Conclusion:   mapResult(b.Result),
		Branch:       stripBranchPrefix(b.SourceBranch),
		CommitSHA:    b.SourceVersion,
		Event:        b.Reason,
		Path:         b.Definition.Path,
		DisplayTitle: b.BuildNumber,
		CreatedAt:    b.QueueTime,
	}

	if m := prBranchRe.FindStringSubmatch(b.SourceBranch); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			run.PRNumbers = []int{n}
		}
	}

	return run
}

func mapResult(result string) string {
	switch result {
	case "succeeded":
		return "success"
	case "failed", "partiallySucceeded":
		return "failure"
	case "canceled":
		return "cancelled"
	default:
		return result
	}
}

func stripBranchPrefix(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/tags/")
	ref = strings.TrimPrefix(ref, "refs/")
	ref = strings.TrimSuffix(ref, "/merge")
	return ref
}

type timelineRecord struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Result   string `json:"result"`
	Log      *struct {
		ID int `json:"id"`
	} `json:"log"`
}

type buildArtifact struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Resource struct {
		DownloadURL string `json:"downloadUrl"`
	} `json:"resource"`
}

type gitItem struct {
	Path     string `json:"path"`
	IsFolder bool   `json:"isFolder"`
}

type diffChange struct {
	Item struct {
		Path string `json:"path"`
	} `json:"item"`
	ChangeType string `json:"changeType"`
}

type testRun struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	TotalTests      int    `json:"totalTests"`
	PassedTests     int    `json:"passedTests"`
	UnanalyzedTests int    `json:"unanalyzedTests"`
}

type testResult struct {
	ID            int     `json:"id"`
	TestCaseTitle string  `json:"testCaseTitle"`
	Outcome       string  `json:"outcome"`
	ErrorMessage  string  `json:"errorMessage"`
	StackTrace    string  `json:"stackTrace"`
	DurationInMs  float64 `json:"durationInMs"`
}

// Provider interface implementation

func (c *Client) ListRuns(ctx context.Context, filter ci.RunFilter) ([]ci.Run, error) {
	status := filter.Status
	if status == "" {
		status = "completed"
	}
	params := url.Values{
		apiVersionKey:  {apiVersionValue},
		"statusFilter": {status},
	}
	if filter.PerPage > 0 {
		params.Set("$top", strconv.Itoa(filter.PerPage))
	}

	apiURL := fmt.Sprintf("%s/%s/_apis/build/builds?%s", c.baseURL, c.project, params.Encode())

	var result struct {
		Value []build `json:"value"`
	}
	if err := c.doRequest(ctx, apiURL, &result); err != nil {
		return nil, err
	}

	runs := make([]ci.Run, len(result.Value))
	for i := range result.Value {
		runs[i] = result.Value[i].toCIRun()
	}
	return runs, nil
}

func (c *Client) GetRun(ctx context.Context, runID int64) (*ci.Run, error) {
	apiURL := fmt.Sprintf("%s/%s/_apis/build/builds/%d?%s=%s", c.baseURL, c.project, runID, apiVersionKey, apiVersionValue)

	var b build
	if err := c.doRequest(ctx, apiURL, &b); err != nil {
		return nil, err
	}

	run := b.toCIRun()
	return &run, nil
}

// encodeJobID packs a build ID and log ID into a single int64.
// buildID occupies the upper 32 bits, logID the lower 32 bits.
func encodeJobID(buildID int64, logID int) int64 {
	return (buildID << 32) | int64(logID)
}

// decodeJobID extracts build ID and log ID from a packed int64.
func decodeJobID(jobID int64) (buildID int64, logID int) {
	return jobID >> 32, int(jobID & 0xFFFFFFFF)
}

func (c *Client) ListJobs(ctx context.Context, runID int64) ([]ci.Job, error) {
	apiURL := fmt.Sprintf("%s/%s/_apis/build/builds/%d/timeline?%s=%s", c.baseURL, c.project, runID, apiVersionKey, apiVersionValue)

	var result struct {
		Records []timelineRecord `json:"records"`
	}
	if err := c.doRequest(ctx, apiURL, &result); err != nil {
		return nil, err
	}

	var jobs []ci.Job
	for _, r := range result.Records {
		if r.Type != "Job" {
			continue
		}

		var logID int
		if r.Log != nil {
			logID = r.Log.ID
		}

		jobs = append(jobs, ci.Job{
			ID:         encodeJobID(runID, logID),
			Name:       r.Name,
			Status:     r.State,
			Conclusion: mapResult(r.Result),
		})
	}
	return jobs, nil
}

func (c *Client) GetJobLogs(ctx context.Context, jobID int64) (string, error) {
	buildID, logID := decodeJobID(jobID)
	if logID == 0 {
		return "", fmt.Errorf("no logs available for this job")
	}

	apiURL := fmt.Sprintf("%s/%s/_apis/build/builds/%d/logs/%d?%s=%s", c.baseURL, c.project, buildID, logID, apiVersionKey, apiVersionValue)

	req, err := c.newAPIRequest(ctx, apiURL)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("build logs: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *Client) ListArtifacts(ctx context.Context, runID int64) ([]ci.Artifact, error) {
	apiURL := fmt.Sprintf("%s/%s/_apis/build/builds/%d/artifacts?%s=%s", c.baseURL, c.project, runID, apiVersionKey, apiVersionValue)

	var result struct {
		Value []buildArtifact `json:"value"`
	}
	if err := c.doRequest(ctx, apiURL, &result); err != nil {
		return nil, err
	}

	// Cache download URLs for DownloadArtifact
	c.artifactURLs = make(map[int64]string, len(result.Value))

	artifacts := make([]ci.Artifact, len(result.Value))
	for i, a := range result.Value {
		id := int64(a.ID)
		artifacts[i] = ci.Artifact{
			ID:   id,
			Name: a.Name,
		}
		c.artifactURLs[id] = a.Resource.DownloadURL
	}
	return artifacts, nil
}

func (c *Client) DownloadArtifact(ctx context.Context, artifactID int64) ([]byte, error) {
	dlURL, ok := c.artifactURLs[artifactID]
	if !ok {
		return nil, fmt.Errorf("artifact %d not found in cache; call ListArtifacts first", artifactID)
	}

	req, err := c.newAPIRequest(ctx, dlURL)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artifact download failed: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) DownloadAndExtract(ctx context.Context, artifactID int64) ([]byte, error) {
	zipData, err := c.DownloadArtifact(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	return ci.ExtractFirstFile(zipData)
}

func (c *Client) GetRepoFile(ctx context.Context, filePath string) (string, error) {
	params := url.Values{
		"path":        {filePath},
		"$format":     {"text"},
		apiVersionKey: {apiVersionValue},
	}
	apiURL := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/items?%s", c.baseURL, c.project, c.project, params.Encode())

	req, err := c.newAPIRequest(ctx, apiURL)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("file not found: %d %s", resp.StatusCode, filePath)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *Client) ListDirectory(ctx context.Context, dirPath string) ([]string, error) {
	params := url.Values{
		"scopePath":      {dirPath},
		"recursionLevel": {"OneLevel"},
		apiVersionKey:    {apiVersionValue},
	}
	apiURL := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/items?%s", c.baseURL, c.project, c.project, params.Encode())

	var result struct {
		Value []gitItem `json:"value"`
	}
	if err := c.doRequest(ctx, apiURL, &result); err != nil {
		return nil, err
	}

	var entries []string
	for _, item := range result.Value {
		// Skip the root directory entry itself
		name := path.Base(item.Path)
		if item.Path == "/"+dirPath || item.Path == dirPath {
			continue
		}
		if item.IsFolder {
			entries = append(entries, name+"/")
		} else {
			entries = append(entries, name)
		}
	}
	return entries, nil
}

func (c *Client) GetChangedFiles(ctx context.Context, ref ci.ChangeRef) ([]ci.ChangedFile, error) {
	if ref.PRNumber > 0 {
		return c.getPRChangedFiles(ctx, ref.PRNumber)
	}

	if ref.HeadSHA == "" {
		return nil, fmt.Errorf("no commit SHA or PR number provided")
	}

	return c.getCommitDiff(ctx, ref.BaseBranch, ref.HeadSHA)
}

func (c *Client) getPRChangedFiles(ctx context.Context, prID int) ([]ci.ChangedFile, error) {
	// Get the latest iteration for the PR
	iterURL := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/pullRequests/%d/iterations?%s",
		c.baseURL, c.project, c.project, prID, url.Values{apiVersionKey: {apiVersionValue}}.Encode())

	var iterResult struct {
		Value []struct {
			ID int `json:"id"`
		} `json:"value"`
	}
	if err := c.doRequest(ctx, iterURL, &iterResult); err != nil {
		return nil, err
	}
	if len(iterResult.Value) == 0 {
		return nil, fmt.Errorf("no iterations found for PR %d", prID)
	}

	latestIter := iterResult.Value[len(iterResult.Value)-1].ID

	// Get changes for that iteration
	changesURL := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/pullRequests/%d/iterations/%d/changes?%s",
		c.baseURL, c.project, c.project, prID, latestIter, url.Values{apiVersionKey: {apiVersionValue}}.Encode())

	var changesResult struct {
		ChangeEntries []struct {
			ChangeType string `json:"changeType"`
			Item       struct {
				Path string `json:"path"`
			} `json:"item"`
		} `json:"changeEntries"`
	}
	if err := c.doRequest(ctx, changesURL, &changesResult); err != nil {
		return nil, err
	}

	files := make([]ci.ChangedFile, len(changesResult.ChangeEntries))
	for i, ch := range changesResult.ChangeEntries {
		files[i] = ci.ChangedFile{
			Path:   strings.TrimPrefix(ch.Item.Path, "/"),
			Status: mapChangeType(ch.ChangeType),
		}
	}
	return files, nil
}

func (c *Client) getCommitDiff(ctx context.Context, baseBranch, headSHA string) ([]ci.ChangedFile, error) {
	base := baseBranch
	if base == "" {
		base = "main"
	}

	params := url.Values{
		"baseVersion":   {base},
		"targetVersion": {headSHA},
		apiVersionKey:   {apiVersionValue},
	}
	apiURL := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/diffs/commits?%s", c.baseURL, c.project, c.project, params.Encode())

	var result struct {
		Changes []diffChange `json:"changes"`
	}
	if err := c.doRequest(ctx, apiURL, &result); err != nil {
		return nil, err
	}

	files := make([]ci.ChangedFile, len(result.Changes))
	for i, ch := range result.Changes {
		files[i] = ci.ChangedFile{
			Path:   strings.TrimPrefix(ch.Item.Path, "/"),
			Status: mapChangeType(ch.ChangeType),
		}
	}
	return files, nil
}

func mapChangeType(ct string) string {
	switch strings.ToLower(ct) {
	case "add":
		return "added"
	case "edit":
		return "modified"
	case "delete":
		return "removed"
	case "rename":
		return "renamed"
	default:
		return ct
	}
}

// TestResultsProvider implementation

func (c *Client) GetTestRuns(ctx context.Context, buildID int64) ([]ci.TestRun, error) {
	buildURI := fmt.Sprintf("vstfs:///Build/Build/%d", buildID)
	params := url.Values{
		"buildUri":    {buildURI},
		apiVersionKey: {apiVersionValue},
	}
	apiURL := fmt.Sprintf("%s/%s/_apis/test/runs?%s", c.baseURL, c.project, params.Encode())

	var result struct {
		Value []testRun `json:"value"`
	}
	if err := c.doRequest(ctx, apiURL, &result); err != nil {
		return nil, err
	}

	runs := make([]ci.TestRun, len(result.Value))
	for i, r := range result.Value {
		runs[i] = ci.TestRun{
			ID:          int64(r.ID),
			Name:        r.Name,
			TotalTests:  r.TotalTests,
			PassedTests: r.PassedTests,
			FailedTests: r.UnanalyzedTests,
		}
	}
	return runs, nil
}

func (c *Client) GetTestResults(ctx context.Context, testRunID int64) ([]ci.TestResult, error) {
	params := url.Values{
		"outcomes":    {"Failed"},
		apiVersionKey: {apiVersionValue},
	}
	apiURL := fmt.Sprintf("%s/%s/_apis/test/runs/%d/results?%s", c.baseURL, c.project, testRunID, params.Encode())

	var result struct {
		Value []testResult `json:"value"`
	}
	if err := c.doRequest(ctx, apiURL, &result); err != nil {
		return nil, err
	}

	results := make([]ci.TestResult, len(result.Value))
	for i, r := range result.Value {
		results[i] = ci.TestResult{
			ID:           int64(r.ID),
			TestName:     r.TestCaseTitle,
			Outcome:      r.Outcome,
			ErrorMessage: r.ErrorMessage,
			StackTrace:   r.StackTrace,
			DurationMs:   r.DurationInMs,
		}
	}
	return results, nil
}

// HTTP helpers

func (c *Client) authHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+c.pat))
}

func (c *Client) newAPIRequest(ctx context.Context, reqURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) doRequest(ctx context.Context, reqURL string, result interface{}) error {
	req, err := c.newAPIRequest(ctx, reqURL)
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
		return fmt.Errorf("azure DevOps API error: %s - %s", resp.Status, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

// CrossRepoProvider implementation

// GetFileFromRepo fetches a file from a specific repository, potentially
// in a different project than the client's primary project.
func (c *Client) GetFileFromRepo(ctx context.Context, repo ci.RepoRef, filePath string) (string, error) {
	project := repo.Project
	if project == "" {
		project = c.project
	}
	repoName := repo.Repo
	if repoName == "" {
		repoName = project
	}

	params := url.Values{
		"path":        {filePath},
		"$format":     {"text"},
		apiVersionKey: {apiVersionValue},
	}
	apiURL := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/items?%s",
		c.baseURL, project, repoName, params.Encode())

	req, err := c.newAPIRequest(ctx, apiURL)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("file not found: %d %s (repo: %s/%s)", resp.StatusCode, filePath, project, repoName)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// DiscoverRepos returns additional repositories used by the build pipeline.
// Results are cached — subsequent calls for any buildID return the cached list.
func (c *Client) DiscoverRepos(ctx context.Context, buildID int64) ([]ci.RepoRef, error) {
	if c.reposDiscovered {
		return c.discoveredRepos, nil
	}
	c.reposDiscovered = true

	// Start with any pre-seeded repos from --test-repo flag
	c.discoveredRepos = append([]ci.RepoRef{}, c.ExtraRepos...)

	// Get build to find definition ID
	apiURL := fmt.Sprintf("%s/%s/_apis/build/builds/%d?%s=%s", c.baseURL, c.project, buildID, apiVersionKey, apiVersionValue)
	var b build
	if err := c.doRequest(ctx, apiURL, &b); err != nil {
		return c.discoveredRepos, nil // non-fatal: return what we have
	}

	if b.Definition.ID == 0 {
		return c.discoveredRepos, nil
	}

	// Get definition resources
	resURL := fmt.Sprintf("%s/%s/_apis/build/definitions/%d/resources?%s=%s",
		c.baseURL, c.project, b.Definition.ID, apiVersionKey, apiVersionValue+"-preview.1")

	var resources struct {
		Resources []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"resources"`
	}
	if err := c.doRequest(ctx, resURL, &resources); err != nil {
		return c.discoveredRepos, nil // non-fatal
	}

	seen := make(map[string]bool)
	for _, r := range c.discoveredRepos {
		seen[r.Project+"/"+r.Repo] = true
	}

	for _, r := range resources.Resources {
		if r.Type != "repository" {
			continue
		}
		// Resource ID format: "project/repo" or just "repo" (same project)
		project, repo := c.project, r.ID
		if parts := strings.SplitN(r.ID, "/", 2); len(parts) == 2 {
			project = parts[0]
			repo = parts[1]
		}
		// Skip primary repo
		if project == c.project && repo == c.project {
			continue
		}
		key := project + "/" + repo
		if seen[key] {
			continue
		}
		seen[key] = true
		c.discoveredRepos = append(c.discoveredRepos, ci.RepoRef{Project: project, Repo: repo})
	}

	return c.discoveredRepos, nil
}
