package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicClient implements the Client interface for Anthropic Claude
type AnthropicClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
	baseURL    string
}

// NewAnthropicClient creates a new Anthropic client
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	return &AnthropicClient{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{},
		baseURL:    "https://api.anthropic.com/v1",
	}
}

// Anthropic API request/response types
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
	Model   string             `json:"model"`
	Usage   anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Complete sends a request to Anthropic
func (c *AnthropicClient) Complete(ctx context.Context, messages []Message) (*Response, error) {
	// Separate system message from conversation
	var systemPrompt string
	anthropicMessages := make([]anthropicMessage, 0, len(messages))

	for _, msg := range messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			continue
		}
		anthropicMessages = append(anthropicMessages, anthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	reqBody := anthropicRequest{
		Model:       c.model,
		MaxTokens:   4096,
		System:      systemPrompt,
		Messages:    anthropicMessages,
		Temperature: 0.1,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/messages", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract text content
	content := ""
	for _, c := range anthropicResp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	return &Response{
		Content:      content,
		InputTokens:  anthropicResp.Usage.InputTokens,
		OutputTokens: anthropicResp.Usage.OutputTokens,
		Model:        anthropicResp.Model,
	}, nil
}

// Provider returns the provider name
func (c *AnthropicClient) Provider() Provider {
	return ProviderAnthropic
}

// Model returns the model name
func (c *AnthropicClient) Model() string {
	return c.model
}
