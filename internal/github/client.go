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

// Artifact represents a GitHub Actions artifact
type Artifact struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_in_bytes"`
	Expired   bool   `json:"expired"`
}

// WorkflowRun represents a GitHub Actions workflow run
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

// FetchTestArtifact finds and downloads a test artifact from a repo
func (c *Client) FetchTestArtifact(ctx context.Context, owner, repo string) ([]byte, string, error) {
	// Get recent failed workflow runs
	runs, err := c.listWorkflowRuns(ctx, owner, repo)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list workflow runs: %w", err)
	}

	// Find a run with artifacts (prefer failed runs)
	for _, run := range runs {
		artifacts, err := c.listArtifacts(ctx, owner, repo, run.ID)
		if err != nil {
			continue
		}

		for _, artifact := range artifacts {
			if artifact.Expired {
				continue
			}

			// Try to download and extract
			content, err := c.downloadAndExtract(ctx, owner, repo, artifact.ID)
			if err != nil {
				continue
			}

			if len(content) > 0 {
				return content, artifact.Name, nil
			}
		}
	}

	return nil, "", fmt.Errorf("no test artifacts found")
}

func (c *Client) listWorkflowRuns(ctx context.Context, owner, repo string) ([]WorkflowRun, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=10&status=completed", c.baseURL, owner, repo)

	var result struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return result.WorkflowRuns, nil
}

func (c *Client) listArtifacts(ctx context.Context, owner, repo string, runID int64) ([]Artifact, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/artifacts", c.baseURL, owner, repo, runID)

	var result struct {
		Artifacts []Artifact `json:"artifacts"`
	}

	if err := c.doRequest(ctx, url, &result); err != nil {
		return nil, err
	}

	return result.Artifacts, nil
}

func (c *Client) downloadAndExtract(ctx context.Context, owner, repo string, artifactID int64) ([]byte, error) {
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

	zipData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return c.extractFirstJSON(zipData)
}

func (c *Client) extractFirstJSON(zipData []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, err
	}

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
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

		// Return first JSON-like content
		if len(content) > 0 && (content[0] == '{' || content[0] == '[') {
			return content, nil
		}
	}

	return nil, fmt.Errorf("no JSON files found in artifact")
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
