package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GoogleClient implements the Client interface for Google Gemini
type GoogleClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
	baseURL    string
}

// NewGoogleClient creates a new Google Gemini client
func NewGoogleClient(apiKey, model string) *GoogleClient {
	return &GoogleClient{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{},
		baseURL:    "https://generativelanguage.googleapis.com/v1beta",
	}
}

// Google API request/response types
type googleRequest struct {
	Contents         []googleContent        `json:"contents"`
	GenerationConfig googleGenerationConfig `json:"generationConfig,omitempty"`
}

type googleContent struct {
	Role  string       `json:"role"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text string `json:"text"`
}

type googleGenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type googleResponse struct {
	Candidates    []googleCandidate `json:"candidates"`
	UsageMetadata googleUsage       `json:"usageMetadata"`
}

type googleCandidate struct {
	Content googleContent `json:"content"`
}

type googleUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Complete sends a request to Google Gemini
func (c *GoogleClient) Complete(ctx context.Context, messages []Message) (*Response, error) {
	// Convert messages to Google format
	contents := make([]googleContent, 0, len(messages))
	for _, msg := range messages {
		role := msg.Role
		// Google uses "user" and "model" (not "assistant")
		if role == "assistant" {
			role = "model"
		}
		// Google doesn't support system role directly - prepend to first user message
		if role == "system" {
			continue // Handle below
		}
		contents = append(contents, googleContent{
			Role:  role,
			Parts: []googlePart{{Text: msg.Content}},
		})
	}

	// Handle system message by prepending to first user message
	for i, msg := range messages {
		if msg.Role == "system" && len(contents) > 0 {
			contents[0].Parts[0].Text = msg.Content + "\n\n" + contents[0].Parts[0].Text
			_ = i
			break
		}
	}

	reqBody := googleRequest{
		Contents: contents,
		GenerationConfig: googleGenerationConfig{
			Temperature:     0.1,
			MaxOutputTokens: 4096,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, c.model, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var googleResp googleResponse
	if err := json.Unmarshal(body, &googleResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(googleResp.Candidates) == 0 {
		return nil, fmt.Errorf("no response candidates")
	}

	content := ""
	for _, part := range googleResp.Candidates[0].Content.Parts {
		content += part.Text
	}

	return &Response{
		Content:      content,
		InputTokens:  googleResp.UsageMetadata.PromptTokenCount,
		OutputTokens: googleResp.UsageMetadata.CandidatesTokenCount,
		Model:        c.model,
	}, nil
}

// Provider returns the provider name
func (c *GoogleClient) Provider() Provider {
	return ProviderGoogle
}

// Model returns the model name
func (c *GoogleClient) Model() string {
	return c.model
}
