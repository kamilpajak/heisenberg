package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Client handles LLM API calls
type Client struct {
	apiKey  string
	baseURL string
	model   string
}

// NewClient creates a new LLM client (Google Gemini)
func NewClient() (*Client, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLE_API_KEY environment variable required")
	}

	return &Client{
		apiKey:  apiKey,
		baseURL: "https://generativelanguage.googleapis.com/v1beta",
		model:   "gemini-2.0-flash",
	}, nil
}

// Request represents the prompt and response
type Request struct {
	Prompt   string
	Response string
}

// Analyze sends content to LLM and returns the analysis
func (c *Client) Analyze(ctx context.Context, content []byte) (*Request, error) {
	prompt := buildPrompt(content)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.1,
			"maxOutputTokens": 2048,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, c.model, c.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	return &Request{
		Prompt:   prompt,
		Response: result.Candidates[0].Content.Parts[0].Text,
	}, nil
}

func buildPrompt(content []byte) string {
	// Truncate if too large
	maxSize := 100000
	if len(content) > maxSize {
		content = content[:maxSize]
	}

	return fmt.Sprintf(`Analyze this test report from a CI/CD pipeline. Identify any test failures and provide a root cause analysis.

Your response should be concise and actionable. Focus on:
1. What tests failed (if any)
2. The likely root cause
3. Suggested fix

If there are no failures, simply state that all tests passed.

Test Report:
%s`, string(content))
}
