package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// WorkflowRun represents a GitHub Actions workflow run
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	CreatedAt  string `json:"created_at"`
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
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=10", c.baseURL, owner, repo)

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
		return fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
