package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIClient implements the Client interface for OpenAI
type OpenAIClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
	baseURL    string
}

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{},
		baseURL:    "https://api.openai.com/v1",
	}
}

// OpenAI API request/response types
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
	Model   string         `json:"model"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Complete sends a request to OpenAI
func (c *OpenAIClient) Complete(ctx context.Context, messages []Message) (*Response, error) {
	// Convert messages to OpenAI format
	openAIMessages := make([]openAIMessage, len(messages))
	for i, msg := range messages {
		openAIMessages[i] = openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	reqBody := openAIRequest{
		Model:       c.model,
		Messages:    openAIMessages,
		Temperature: 0.1,
		MaxTokens:   4096,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

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

	var openAIResp openAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices")
	}

	return &Response{
		Content:      openAIResp.Choices[0].Message.Content,
		InputTokens:  openAIResp.Usage.PromptTokens,
		OutputTokens: openAIResp.Usage.CompletionTokens,
		Model:        openAIResp.Model,
	}, nil
}

// Provider returns the provider name
func (c *OpenAIClient) Provider() Provider {
	return ProviderOpenAI
}

// Model returns the model name
func (c *OpenAIClient) Model() string {
	return c.model
}
