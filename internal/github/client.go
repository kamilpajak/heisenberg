package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Client handles GitHub API interactions
type Client struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new GitHub client
func NewClient(token string) *Client {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return &Client{
		token:      token,
		httpClient: &http.Client{},
		baseURL:    "https://api.github.com",
	}
}

// Repository represents a GitHub repository
type Repository struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Language    string `json:"language"`
	HTMLURL     string `json:"html_url"`
	HasActions  bool   `json:"has_actions"`
}

// WorkflowRun represents a GitHub Actions workflow run
type WorkflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	HTMLURL      string `json:"html_url"`
	CreatedAt    string `json:"created_at"`
	HeadBranch   string `json:"head_branch"`
	HeadSHA      string `json:"head_sha"`
	WorkflowID   int64  `json:"workflow_id"`
	RunNumber    int    `json:"run_number"`
	RunAttempt   int    `json:"run_attempt"`
	TriggerEvent string `json:"event"`
}

// Artifact represents a GitHub Actions artifact
type Artifact struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_in_bytes"`
	Expired   bool   `json:"expired"`
}

// ListWorkflowRuns fetches workflow runs for a repository
func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo string) ([]WorkflowRun, error) {
	// Only get completed runs to ensure artifacts are available
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=10&status=completed", c.baseURL, owner, repo)

	var result struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return result.WorkflowRuns, nil
}

// ListArtifacts fetches artifacts for a workflow run
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

// DownloadArtifact downloads an artifact and returns the raw bytes
func (c *Client) DownloadArtifact(ctx context.Context, owner, repo string, artifactID int64) ([]byte, error) {
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
		return nil, fmt.Errorf("failed to download artifact: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// GetRepository fetches repository information
func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*Repository, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, repo)

	var result Repository
	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SearchRepositories searches for repositories matching query
func (c *Client) SearchRepositories(ctx context.Context, query string, minStars, limit int) ([]Repository, error) {
	// Add stars filter to query
	fullQuery := fmt.Sprintf("%s stars:>=%d", query, minStars)
	url := fmt.Sprintf("%s/search/repositories?q=%s&sort=stars&order=desc&per_page=%d",
		c.baseURL, strings.ReplaceAll(fullQuery, " ", "+"), limit)

	var result struct {
		Items []Repository `json:"items"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return result.Items, nil
}

// ExtractedFile represents a file extracted from an artifact
type ExtractedFile struct {
	Name    string
	Content []byte
}

// ExtractArtifact downloads and extracts an artifact, returning files matching patterns
func (c *Client) ExtractArtifact(ctx context.Context, owner, repo string, artifactID int64, patterns []string) ([]ExtractedFile, error) {
	zipData, err := c.DownloadArtifact(ctx, owner, repo, artifactID)
	if err != nil {
		return nil, err
	}

	return extractZipFiles(zipData, patterns)
}

// extractZipFiles extracts files from zip data matching given patterns
func extractZipFiles(zipData []byte, patterns []string) ([]ExtractedFile, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to read zip: %w", err)
	}

	var files []ExtractedFile
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Check if file matches any pattern
		if !matchesAnyPattern(f.Name, patterns) {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}

		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}

		files = append(files, ExtractedFile{
			Name:    f.Name,
			Content: content,
		})
	}

	return files, nil
}

// matchesAnyPattern checks if filename matches any of the glob patterns
func matchesAnyPattern(filename string, patterns []string) bool {
	if len(patterns) == 0 {
		return true // No patterns = match all
	}

	baseName := filepath.Base(filename)
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, baseName); matched {
			return true
		}
		// Also try matching the full path
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
	}
	return false
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
