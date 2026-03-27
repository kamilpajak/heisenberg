// Package saas provides an HTTP client for the Heisenberg SaaS API.
package saas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// Client sends analysis results to the Heisenberg SaaS API.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient creates a SaaS client from environment variables.
// Returns nil if HEISENBERG_API_URL or HEISENBERG_API_KEY is not set.
func NewClient() *Client {
	url := os.Getenv("HEISENBERG_API_URL")
	key := os.Getenv("HEISENBERG_API_KEY")
	if url == "" || key == "" {
		return nil
	}
	return &Client{
		baseURL: url,
		apiKey:  key,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// NewTestClient creates a client with explicit URL and key (for testing).
func NewTestClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL returns the configured API base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// SubmitParams holds the data needed to persist an analysis.
type SubmitParams struct {
	OrgID     string
	Owner     string
	Repo      string
	RunID     int64
	Branch    string
	CommitSHA string
	Result    *llm.AnalysisResult
}

// SubmitAnalysis sends an analysis result to the SaaS API for persistence.
// Returns the analysis ID on success.
func (c *Client) SubmitAnalysis(ctx context.Context, p SubmitParams) (string, error) {
	body := map[string]any{
		"owner":    p.Owner,
		"repo":     p.Repo,
		"run_id":   p.RunID,
		"category": p.Result.Category,
		"text":     p.Result.Text,
	}
	if p.Branch != "" {
		body["branch"] = p.Branch
	}
	if p.CommitSHA != "" {
		body["commit_sha"] = p.CommitSHA
	}
	if p.Result.Confidence > 0 {
		body["confidence"] = p.Result.Confidence
	}
	if p.Result.Sensitivity != "" {
		body["sensitivity"] = p.Result.Sensitivity
	}
	if p.Result.RCA != nil {
		body["rca"] = p.Result.RCA
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/organizations/%s/analyses", c.baseURL, p.OrgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to submit analysis: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.ID, nil
}
