package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewExcerpt_Short(t *testing.T) {
	assert.Equal(t, "short text", previewExcerpt("short text", 200))
}

func TestPreviewExcerpt_FindsErrorKeyword(t *testing.T) {
	long := "lots of setup noise " + repeat("x", 200) + " Error: something broke badly " + repeat("y", 100)
	result := previewExcerpt(long, 80)
	assert.Contains(t, result, "Error: something broke badly")
	assert.True(t, len(result) <= 90, "excerpt should be bounded")
}

func TestPreviewExcerpt_FindsFAIL(t *testing.T) {
	long := repeat("a", 300) + "FAIL tests/login.spec.ts" + repeat("b", 100)
	result := previewExcerpt(long, 80)
	assert.Contains(t, result, "FAIL")
}

func TestPreviewExcerpt_FallsBackToTail(t *testing.T) {
	long := repeat("a", 100) + "useful tail content"
	result := previewExcerpt(long, 30)
	assert.True(t, len(result) <= 40)
	assert.Contains(t, result, "useful tail content")
}

func TestPreviewExcerpt_StripsANSI(t *testing.T) {
	ansi := "before \x1b[31mred text\x1b[0m after"
	result := previewExcerpt(ansi, 200)
	assert.NotContains(t, result, "\x1b")
	assert.Contains(t, result, "red text")
}

func TestPreviewExcerpt_KeywordPriority(t *testing.T) {
	long := repeat("x", 100) + "##[error]Process completed" + repeat("y", 100) + "FAIL test.spec.ts" + repeat("z", 100)
	result := previewExcerpt(long, 80)
	assert.Contains(t, result, "FAIL test.spec.ts")
}

func TestIsEmptyResponse_Nil(t *testing.T) {
	assert.True(t, isEmptyResponse(nil))
}

func TestIsEmptyResponse_EmptyParts(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{}}}
	assert.True(t, isEmptyResponse(c))
}

func TestIsEmptyResponse_WithText(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{{Text: "hello"}}}}
	assert.False(t, isEmptyResponse(c))
}

func TestIsEmptyResponse_WithFunctionCall(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{{FunctionCall: &FunctionCall{Name: "done"}}}}}
	assert.False(t, isEmptyResponse(c))
}

func TestIsEmptyResponse_OnlyEmptyParts(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{{}, {}}}}
	assert.True(t, isEmptyResponse(c))
}

func TestDescribeEmptyResponse_Nil(t *testing.T) {
	assert.Equal(t, "no candidate", describeEmptyResponse(nil))
}

func TestDescribeEmptyResponse_WithFinishReason(t *testing.T) {
	c := &Candidate{FinishReason: "STOP"}
	assert.Equal(t, "finishReason=STOP", describeEmptyResponse(c))
}

func TestDescribeEmptyResponse_WithBlockedSafety(t *testing.T) {
	c := &Candidate{
		FinishReason: "SAFETY",
		SafetyRatings: []SafetyRating{
			{Category: "HARM_CATEGORY_DANGEROUS", Probability: "HIGH", Blocked: true},
			{Category: "HARM_CATEGORY_HARASSMENT", Probability: "NEGLIGIBLE"},
		},
	}
	result := describeEmptyResponse(c)
	assert.Contains(t, result, "finishReason=SAFETY")
	assert.Contains(t, result, "HARM_CATEGORY_DANGEROUS=HIGH (blocked)")
	assert.NotContains(t, result, "HARASSMENT")
}

func TestGenerate_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.URL.Path, "/models/test-model:generateContent")

		resp := GenerateResponse{
			Candidates: []Candidate{{
				Content:      Content{Parts: []Part{{Text: "analysis result"}}},
				FinishReason: "STOP",
			}},
			UsageMetadata: &UsageMetadata{
				PromptTokenCount:     100,
				CandidatesTokenCount: 50,
				TotalTokenCount:      150,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := &Client{apiKey: "test-key", baseURL: ts.URL, model: "test-model"}
	resp, err := c.generate(context.Background(), []Content{}, nil, nil)

	require.NoError(t, err)
	require.Len(t, resp.Candidates, 1)
	assert.Equal(t, "analysis result", resp.Candidates[0].Content.Parts[0].Text)
	assert.Equal(t, 100, resp.UsageMetadata.PromptTokenCount)
	assert.Equal(t, 150, resp.UsageMetadata.TotalTokenCount)
}

func TestGenerate_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer ts.Close()

	c := &Client{apiKey: "test-key", baseURL: ts.URL, model: "test-model"}
	_, err := c.generate(context.Background(), []Content{}, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gemini API error")
	assert.Contains(t, err.Error(), "429")
}

func TestGenerate_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer ts.Close()

	c := &Client{apiKey: "test-key", baseURL: ts.URL, model: "test-model"}
	_, err := c.generate(context.Background(), []Content{}, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse response")
}

func TestGenerate_ContextCancelled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response
		select {}
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	c := &Client{apiKey: "test-key", baseURL: ts.URL, model: "test-model"}
	_, err := c.generate(ctx, []Content{}, nil, nil)

	require.Error(t, err)
}

func TestGenerate_SendsRequestBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, float64(0.1), req.GenerationConfig.Temperature)
		assert.Equal(t, 8192, req.GenerationConfig.MaxOutputTokens)

		resp := GenerateResponse{Candidates: []Candidate{{Content: Content{Parts: []Part{{Text: "ok"}}}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := &Client{apiKey: "test-key", baseURL: ts.URL, model: "test-model"}
	_, err := c.generate(context.Background(), []Content{{Role: "user", Parts: []Part{{Text: "test"}}}}, nil, nil)
	require.NoError(t, err)
}

func TestNewClient_MissingAPIKey(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := NewClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GOOGLE_API_KEY")
}

func TestNewClient_Success(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "test-key")
	c, err := NewClient()
	require.NoError(t, err)
	assert.Equal(t, "test-key", c.apiKey)
	assert.Equal(t, "gemini-2.5-flash", c.model)
}

// repeat creates a string of n copies of s.
func repeat(s string, n int) string {
	result := ""
	for range n {
		result += s
	}
	return result
}
