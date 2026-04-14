package semcluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GeminiEmbedder is a minimal HTTP-backed Embedder for Google's Gemini
// embedding API. It intentionally duplicates a slice of
// ee/patterns.EmbeddingClient so pkg/semcluster stays importable from OSS
// binaries without crossing the license boundary.
type GeminiEmbedder struct {
	apiKey     string
	baseURL    string
	model      string
	dimensions int
	httpClient *http.Client
}

// NewGeminiEmbedder returns a GeminiEmbedder using apiKey for authentication.
// Default model: gemini-embedding-001 at 768 dimensions.
func NewGeminiEmbedder(apiKey string) *GeminiEmbedder {
	return &GeminiEmbedder{
		apiKey:     apiKey,
		baseURL:    "https://generativelanguage.googleapis.com/v1beta",
		model:      "gemini-embedding-001",
		dimensions: 768,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithHTTPClient overrides the HTTP client (used for VCR in tests).
func (e *GeminiEmbedder) WithHTTPClient(c *http.Client) *GeminiEmbedder {
	e.httpClient = c
	return e
}

type geminiEmbedRequest struct {
	Model                string             `json:"model"`
	Content              geminiEmbedContent `json:"content"`
	OutputDimensionality int                `json:"outputDimensionality,omitempty"`
}

type geminiEmbedContent struct {
	Parts []geminiEmbedPart `json:"parts"`
}

type geminiEmbedPart struct {
	Text string `json:"text"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *geminiEmbedError `json:"error,omitempty"`
}

type geminiEmbedError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Embed satisfies the Embedder interface.
func (e *GeminiEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := geminiEmbedRequest{
		Model:                fmt.Sprintf("models/%s", e.model),
		Content:              geminiEmbedContent{Parts: []geminiEmbedPart{{Text: text}}},
		OutputDimensionality: e.dimensions,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", e.baseURL, e.model, e.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
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

	var result geminiEmbedResponse
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
