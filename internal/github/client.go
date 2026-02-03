package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Client handles GitHub API interactions
type Client struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new GitHub client
func NewClient() *Client {
	return &Client{
		token:      os.Getenv("GITHUB_TOKEN"),
		httpClient: &http.Client{},
		baseURL:    "https://api.github.com",
	}
}

// ArtifactType represents the format of a test artifact
type ArtifactType string

const (
	ArtifactHTML ArtifactType = "html"
	ArtifactJSON ArtifactType = "json"
	ArtifactBlob ArtifactType = "blob"
)

// ArtifactResult contains fetched artifact data
type ArtifactResult struct {
	Type    ArtifactType
	Content []byte   // Extracted content for html/json
	Blobs   [][]byte // Raw zips for blob reports (need merging)
	Name    string
}

// Artifact represents a GitHub Actions artifact
type Artifact struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_in_bytes"`
	Expired   bool   `json:"expired"`
}

// WorkflowRun represents a GitHub Actions workflow run
type WorkflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Conclusion   string `json:"conclusion"`
	HeadBranch   string `json:"head_branch"`
	HeadSHA      string `json:"head_sha"`
	Event        string `json:"event"`
	Path         string `json:"path"`
	DisplayTitle string `json:"display_title"`
}

// Job represents a GitHub Actions job within a workflow run
type Job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// FetchTestArtifact finds and downloads test artifacts from a repo.
// If runID > 0, fetches from that specific run. Otherwise scans recent runs.
// Priority: html-report > json > blob-report.
func (c *Client) FetchTestArtifact(ctx context.Context, owner, repo string, runID int64) (*ArtifactResult, error) {
	if runID > 0 {
		return c.fetchFromRun(ctx, owner, repo, runID)
	}

	runs, err := c.ListWorkflowRuns(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflow runs: %w", err)
	}

	for _, run := range runs {
		artifacts, err := c.ListArtifacts(ctx, owner, repo, run.ID)
		if err != nil {
			continue
		}

		result := c.selectAndFetch(ctx, owner, repo, artifacts)
		if result != nil {
			return result, nil
		}
	}

	return nil, fmt.Errorf("no test artifacts found")
}

func (c *Client) fetchFromRun(ctx context.Context, owner, repo string, runID int64) (*ArtifactResult, error) {
	artifacts, err := c.ListArtifacts(ctx, owner, repo, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts for run %d: %w", runID, err)
	}

	result := c.selectAndFetch(ctx, owner, repo, artifacts)
	if result != nil {
		return result, nil
	}

	// Found artifacts but none are Playwright reports
	var names []string
	for _, a := range artifacts {
		if !a.Expired {
			names = append(names, a.Name)
		}
	}
	if len(names) > 0 {
		return nil, fmt.Errorf("no Playwright reports found in run %d (found: %s)", runID, strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("no artifacts found in run %d", runID)
}

// ClassifyArtifact returns the artifact type based on its name.
func ClassifyArtifact(name string) ArtifactType {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "html-report") || n == "playwright-report":
		return ArtifactHTML
	case strings.HasSuffix(n, ".json") || strings.Contains(n, "json"):
		return ArtifactJSON
	case strings.Contains(n, "blob-report"):
		return ArtifactBlob
	default:
		return ""
	}
}

func (c *Client) selectAndFetch(ctx context.Context, owner, repo string, artifacts []Artifact) *ArtifactResult {
	var htmlArtifacts, jsonArtifacts, blobArtifacts []Artifact

	for _, a := range artifacts {
		if a.Expired {
			continue
		}
		switch ClassifyArtifact(a.Name) {
		case ArtifactHTML:
			htmlArtifacts = append(htmlArtifacts, a)
		case ArtifactJSON:
			jsonArtifacts = append(jsonArtifacts, a)
		case ArtifactBlob:
			blobArtifacts = append(blobArtifacts, a)
		}
	}

	// Priority 1: HTML report
	for _, a := range htmlArtifacts {
		content, err := c.DownloadAndExtract(ctx, owner, repo, a.ID)
		if err == nil && len(content) > 0 {
			return &ArtifactResult{Type: ArtifactHTML, Content: content, Name: a.Name}
		}
	}

	// Priority 2: JSON report
	for _, a := range jsonArtifacts {
		content, err := c.DownloadAndExtract(ctx, owner, repo, a.ID)
		if err == nil && len(content) > 0 {
			return &ArtifactResult{Type: ArtifactJSON, Content: content, Name: a.Name}
		}
	}

	// Priority 3: Blob reports (download all shards as raw zips)
	if len(blobArtifacts) > 0 {
		var blobs [][]byte
		for _, a := range blobArtifacts {
			zipData, err := c.DownloadRawZip(ctx, owner, repo, a.ID)
			if err == nil && len(zipData) > 0 {
				blobs = append(blobs, zipData)
			}
		}
		if len(blobs) > 0 {
			return &ArtifactResult{
				Type:  ArtifactBlob,
				Blobs: blobs,
				Name:  fmt.Sprintf("%d blob-report(s)", len(blobs)),
			}
		}
	}

	return nil
}

// ListWorkflowRuns returns recent completed workflow runs.
func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo string) ([]WorkflowRun, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=10&status=completed", c.baseURL, owner, repo)

	var result struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return result.WorkflowRuns, nil
}

// ListArtifacts returns artifacts for a workflow run.
func (c *Client) ListArtifacts(ctx context.Context, owner, repo string, runID int64) ([]Artifact, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/artifacts", c.baseURL, owner, repo, runID)

	var result struct {
		Artifacts []Artifact `json:"artifacts"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return result.Artifacts, nil
}

// DownloadRawZip downloads an artifact as a raw ZIP.
func (c *Client) DownloadRawZip(ctx context.Context, owner, repo string, artifactID int64) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/artifacts/%d/zip", c.baseURL, owner, repo, artifactID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

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
func (c *Client) DownloadAndExtract(ctx context.Context, owner, repo string, artifactID int64) ([]byte, error) {
	zipData, err := c.DownloadRawZip(ctx, owner, repo, artifactID)
	if err != nil {
		return nil, err
	}

	return c.extractFirstFile(zipData)
}

func (c *Client) extractFirstFile(zipData []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, err
	}

	var fallback []byte

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		name := strings.ToLower(f.Name)
		isJSON := strings.HasSuffix(name, ".json")
		isHTML := strings.HasSuffix(name, ".html")

		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || len(content) == 0 {
			continue
		}

		if isHTML {
			return content, nil
		}
		if isJSON {
			return content, nil
		}
		if fallback == nil {
			fallback = content
		}
	}

	if fallback != nil {
		return fallback, nil
	}

	return nil, fmt.Errorf("no files found in artifact")
}

// GetWorkflowRun fetches a single workflow run by ID.
func (c *Client) GetWorkflowRun(ctx context.Context, owner, repo string, runID int64) (*WorkflowRun, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", c.baseURL, owner, repo, runID)

	var run WorkflowRun
	if err := c.doRequest(ctx, url, &run); err != nil {
		return nil, err
	}

	return &run, nil
}

// ListJobs returns jobs for a workflow run.
func (c *Client) ListJobs(ctx context.Context, owner, repo string, runID int64) ([]Job, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/jobs", c.baseURL, owner, repo, runID)

	var result struct {
		Jobs []Job `json:"jobs"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return result.Jobs, nil
}

// GetJobLogs fetches the plain-text logs for a job.
func (c *Client) GetJobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", c.baseURL, owner, repo, jobID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

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

// GetRepoFile fetches a file from the repo's default branch via the contents API.
func (c *Client) GetRepoFile(ctx context.Context, owner, repo, path string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, path)

	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
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

func (c *Client) doRequest(ctx context.Context, url string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

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
