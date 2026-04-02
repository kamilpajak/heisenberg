// Package patterns provides dynamic pattern recognition for the SaaS tier.
package patterns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kamilpajak/heisenberg/pkg/llm"
)

const (
	defaultBaseURL        = "https://generativelanguage.googleapis.com/v1beta"
	defaultEmbeddingModel = "gemini-embedding-001"
	defaultDimensions     = 768
)

// EmbeddingClient generates vector embeddings via the Gemini embedding API.
type EmbeddingClient struct {
	apiKey     string
	baseURL    string
	model      string
	dimensions int
	httpClient *http.Client
}

// NewEmbeddingClient creates an embedding client.
// If apiKey is empty, falls back to GOOGLE_API_KEY environment variable.
func NewEmbeddingClient(apiKey string) (*EmbeddingClient, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLE_API_KEY required for embedding client")
	}
	return &EmbeddingClient{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		model:      defaultEmbeddingModel,
		dimensions: defaultDimensions,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// embedRequest is the request body for Gemini embedContent.
type embedRequest struct {
	Model                string       `json:"model"`
	Content              embedContent `json:"content"`
	OutputDimensionality int          `json:"outputDimensionality,omitempty"`
}

type embedContent struct {
	Parts []embedPart `json:"parts"`
}

type embedPart struct {
	Text string `json:"text"`
}

// embedResponse is the response from Gemini embedContent.
type embedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *embedError `json:"error,omitempty"`
}

type embedError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Embed generates a vector embedding for the given text.
func (c *EmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := embedRequest{
		Model: fmt.Sprintf("models/%s", c.model),
		Content: embedContent{
			Parts: []embedPart{{Text: text}},
		},
		OutputDimensionality: c.dimensions,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", c.baseURL, c.model, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result embedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal embedding response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("embedding API error %d: %s", result.Error.Code, result.Error.Message)
	}

	if len(result.Embedding.Values) == 0 {
		return nil, fmt.Errorf("embedding API returned empty vector")
	}

	return result.Embedding.Values, nil
}

// NewTestEmbeddingClient creates an embedding client pointing at a custom base URL.
// Used in integration tests with httptest servers.
func NewTestEmbeddingClient(baseURL string) *EmbeddingClient {
	return &EmbeddingClient{
		apiKey:     "test-key",
		baseURL:    baseURL,
		model:      defaultEmbeddingModel,
		dimensions: defaultDimensions,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// ComputeEmbeddingText builds the text that will be embedded for an RCA.
// It uses the same signals as pkg/patterns.ComputeFingerprint but in natural
// language form to give the embedding model richer semantic signal.
func ComputeEmbeddingText(rca *llm.RootCauseAnalysis) string {
	var b strings.Builder

	fmt.Fprintf(&b, "failure_type: %s\n", rca.FailureType)

	if rca.Location != nil && rca.Location.FilePath != "" {
		ext := filepath.Ext(rca.Location.FilePath)
		if ext != "" {
			fmt.Fprintf(&b, "file_pattern: *%s\n", ext)
		}
	}

	fmt.Fprintf(&b, "root_cause: %s\n", rca.RootCause)

	if rca.Symptom != "" {
		fmt.Fprintf(&b, "symptom: %s\n", rca.Symptom)
	}

	return b.String()
}
