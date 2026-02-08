package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPreviewExcerpt_UTF8Safe(t *testing.T) {
	// Test with multi-byte UTF-8 characters (Polish, emoji)
	long := repeat("ą", 50) + "Error: błąd połączenia 🔥" + repeat("ę", 50)
	result := previewExcerpt(long, 40)
	// Should not panic and should contain the error message
	assert.Contains(t, result, "Error:")
	// Result should be valid UTF-8 (no broken characters)
	assert.True(t, isValidUTF8(result), "result should be valid UTF-8")
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false // replacement character indicates invalid UTF-8
		}
	}
	return true
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
	assert.Equal(t, "gemini-3-pro-preview", c.model, "must use Gemini 3 Pro")
}

// noopEmitter discards all progress events.
type noopEmitter struct{}

func (noopEmitter) Emit(ProgressEvent) {}

// mockToolHandler is a test double for ToolExecutor that handles the done tool.
type mockToolHandler struct {
	emitter     ProgressEmitter
	category    string
	confidence  int
	sensitivity string
}

func (m *mockToolHandler) Execute(_ context.Context, call FunctionCall) (string, bool, error) {
	if call.Name == "done" {
		if cat, ok := call.Args["category"].(string); ok {
			m.category = cat
		} else {
			m.category = CategoryDiagnosis
		}
		if conf, ok := call.Args["confidence"].(float64); ok {
			m.confidence = int(conf)
		} else {
			m.confidence = 50
		}
		if sens, ok := call.Args["missing_information_sensitivity"].(string); ok {
			m.sensitivity = sens
		} else {
			m.sensitivity = "medium"
		}
		return "", true, nil
	}
	return "unknown tool: " + call.Name, false, nil
}

func (m *mockToolHandler) HasPendingTraces() bool       { return false }
func (m *mockToolHandler) DiagnosisCategory() string    { return m.category }
func (m *mockToolHandler) DiagnosisConfidence() int     { return m.confidence }
func (m *mockToolHandler) DiagnosisSensitivity() string { return m.sensitivity }
func (m *mockToolHandler) GetEmitter() ProgressEmitter  { return m.emitter }

// testToolDeclarations returns minimal tool declarations for tests.
func testToolDeclarations() []FunctionDeclaration {
	return []FunctionDeclaration{
		{Name: "done", Description: "Signal done"},
		{Name: "fake_tool", Description: "Test tool"},
	}
}

// mockServer creates a test server that returns responses in sequence.
func mockServer(t *testing.T, responses []GenerateResponse) *httptest.Server {
	t.Helper()
	call := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if call >= len(responses) {
			t.Fatalf("unexpected call %d (only %d responses)", call, len(responses))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses[call])
		call++
	}))
}

func TestRunAgentLoop_TextOnlyResponse(t *testing.T) {
	ts := mockServer(t, []GenerateResponse{
		{
			Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "The test failed because..."}}}}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 500, TotalTokenCount: 600},
		},
	})
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "initial context", false)

	require.NoError(t, err)
	assert.Equal(t, "The test failed because...", result.Text)
	assert.Equal(t, CategoryDiagnosis, result.Category)
	assert.Equal(t, 50, result.Confidence)
}

func TestRunAgentLoop_TextOnlyVerbose(t *testing.T) {
	ts := mockServer(t, []GenerateResponse{
		{
			Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "analysis"}}}}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 200},
		},
	})
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", true)

	require.NoError(t, err)
	assert.Equal(t, "analysis", result.Text)
}

func TestRunAgentLoop_DoneThenText(t *testing.T) {
	ts := mockServer(t, []GenerateResponse{
		// Step 1: model calls done tool
		{
			Candidates: []Candidate{{Content: Content{Parts: []Part{{
				FunctionCall: &FunctionCall{
					Name: "done",
					Args: map[string]any{"category": "diagnosis", "confidence": float64(90), "missing_information_sensitivity": "low"},
				},
			}}}}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 1000},
		},
		// Step 2: model returns final text
		{
			Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "Root cause: timeout"}}}}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 1200},
		},
	})
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", true)

	require.NoError(t, err)
	assert.Equal(t, "Root cause: timeout", result.Text)
	assert.Equal(t, CategoryDiagnosis, result.Category)
	assert.Equal(t, 90, result.Confidence)
	assert.Equal(t, "low", result.Sensitivity)
}

func TestRunAgentLoop_EmptyResponseRetry(t *testing.T) {
	ts := mockServer(t, []GenerateResponse{
		// Step 1: empty response (reasoning tokens consumed output)
		{
			Candidates:    []Candidate{{Content: Content{Parts: []Part{{}}}, FinishReason: "STOP"}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 5000},
		},
		// Step 1 retry: actual text response
		{
			Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "Recovered analysis"}}}}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 5100},
		},
	})
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", true)

	require.NoError(t, err)
	assert.Equal(t, "Recovered analysis", result.Text)
}

func TestRunAgentLoop_EmptyResponseRetryFails(t *testing.T) {
	ts := mockServer(t, []GenerateResponse{
		{
			Candidates:    []Candidate{{Content: Content{Parts: []Part{{}}}, FinishReason: "STOP"}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 5000},
		},
		// Retry also empty
		{
			Candidates:    []Candidate{{Content: Content{Parts: []Part{{}}}, FinishReason: "STOP"}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 5100},
		},
	})
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	_, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response after retry")
}

func TestRunAgentLoop_NoCandidates(t *testing.T) {
	ts := mockServer(t, []GenerateResponse{
		{Candidates: []Candidate{}},
	})
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	_, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response from model")
}

func TestRunAgentLoop_ToolCallVerbose(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp GenerateResponse
		switch call {
		case 0:
			// Model calls an unknown tool (returns result without GitHub client)
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "fake_tool",
						Args: map[string]any{"key": "value"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 800, TotalTokenCount: 900},
			}
		case 1:
			// Model returns text without calling done → triggers nudge
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "diagnosis text"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 1000},
			}
		default:
			// After nudge: model calls done with structured metadata
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "done",
						Args: map[string]any{"category": "diagnosis", "confidence": float64(85), "missing_information_sensitivity": "low"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 1100},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
		call++
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", true)

	require.NoError(t, err)
	assert.Equal(t, "diagnosis text", result.Text)
	assert.Equal(t, CategoryDiagnosis, result.Category)
	assert.Equal(t, 85, result.Confidence)
	assert.Equal(t, "low", result.Sensitivity)
}

func TestRunAgentLoop_DoneNudgeFallback(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp GenerateResponse
		switch call {
		case 0:
			// Model calls a tool (establishes hasCalledTools)
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "fake_tool",
						Args: map[string]any{},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
			}
		case 1:
			// Model returns text without done → triggers nudge
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "my analysis"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 700},
			}
		default:
			// After nudge: model returns text again instead of calling done
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "still no done call"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 800},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
		call++
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.NoError(t, err)
	assert.Equal(t, "my analysis", result.Text, "should return the original saved text")
	assert.Equal(t, CategoryDiagnosis, result.Category)
	assert.Equal(t, 50, result.Confidence, "should use default confidence")
	assert.Equal(t, "medium", result.Sensitivity, "should use default sensitivity")
}

func TestRunAgentLoop_GenerateErrorMidLoop(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if call == 0 {
			// First call: model calls done
			resp := GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "done",
						Args: map[string]any{"category": "diagnosis", "confidence": float64(80), "missing_information_sensitivity": "low"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			call++
			return
		}
		// Second call: API error
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "server error"}`))
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	_, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gemini API error")
}

func TestEmit_NilHandler(t *testing.T) {
	// Should not panic
	emit(nil, ProgressEvent{Type: "info", Message: "test"})
}

func TestEmit_NilEmitter(t *testing.T) {
	h := &mockToolHandler{}
	// Should not panic
	emit(h, ProgressEvent{Type: "info", Message: "test"})
}

// repeat creates a string of n copies of s.
func repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

func TestCallKey_BasicTool(t *testing.T) {
	call := FunctionCall{Name: "get_job_logs", Args: map[string]any{"job_id": float64(123)}}
	key := callKey(call)
	assert.Equal(t, `get_job_logs:{"job_id":123}`, key)
}

func TestCallKey_NoArgs(t *testing.T) {
	call := FunctionCall{Name: "list_jobs", Args: nil}
	key := callKey(call)
	assert.Equal(t, "list_jobs:null", key)
}

func TestCallKey_EmptyArgs(t *testing.T) {
	call := FunctionCall{Name: "list_jobs", Args: map[string]any{}}
	key := callKey(call)
	assert.Equal(t, "list_jobs:{}", key)
}

func TestCallKey_DifferentArgsSameToolDifferentKeys(t *testing.T) {
	call1 := FunctionCall{Name: "get_job_logs", Args: map[string]any{"job_id": float64(123)}}
	call2 := FunctionCall{Name: "get_job_logs", Args: map[string]any{"job_id": float64(456)}}

	key1 := callKey(call1)
	key2 := callKey(call2)

	assert.NotEqual(t, key1, key2)
}

func TestCallKey_SameArgsSameKey(t *testing.T) {
	call1 := FunctionCall{Name: "get_artifact", Args: map[string]any{"name": "html-report"}}
	call2 := FunctionCall{Name: "get_artifact", Args: map[string]any{"name": "html-report"}}

	key1 := callKey(call1)
	key2 := callKey(call2)

	assert.Equal(t, key1, key2)
}

func TestRunAgentLoop_DuplicateCallDetection(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp GenerateResponse
		switch call {
		case 0:
			// Model calls fake_tool with args
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "fake_tool",
						Args: map[string]any{"key": "value"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
			}
		case 1:
			// Model calls same tool with same args again (should be blocked)
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "fake_tool",
						Args: map[string]any{"key": "value"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 600},
			}
		default:
			// Model returns final text
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "final analysis"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 700},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
		call++
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.NoError(t, err)
	assert.Equal(t, "final analysis", result.Text)
	// Test passed if no infinite loop occurred - duplicate was detected
}

func TestRunAgentLoop_DuplicateCallAllowsDifferentArgs(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp GenerateResponse
		switch call {
		case 0:
			// Model calls fake_tool with args
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "fake_tool",
						Args: map[string]any{"key": "value1"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
			}
		case 1:
			// Model calls same tool with DIFFERENT args (should be allowed)
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "fake_tool",
						Args: map[string]any{"key": "value2"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 600},
			}
		default:
			// Model returns final text
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "analysis complete"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 700},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
		call++
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.NoError(t, err)
	assert.Equal(t, "analysis complete", result.Text)
}

// mockToolHandlerWithTraces is a mock that returns pending traces.
type mockToolHandlerWithTraces struct {
	mockToolHandler
	pendingTraces bool
	tracesResult  string
}

func (m *mockToolHandlerWithTraces) HasPendingTraces() bool { return m.pendingTraces }
func (m *mockToolHandlerWithTraces) Execute(_ context.Context, call FunctionCall) (string, bool, error) {
	if call.Name == "get_test_traces" {
		m.pendingTraces = false // Mark traces as fetched
		return m.tracesResult, false, nil
	}
	return m.mockToolHandler.Execute(context.Background(), call)
}

func TestRunAgentLoop_ForceTraces(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp GenerateResponse
		switch call {
		case 0:
			// Model returns text response - triggers forceTraces because handler has pending traces
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "initial analysis"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
			}
		default:
			// After forceTraces, model returns final analysis
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "final analysis with traces"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 700},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
		call++
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandlerWithTraces{
		mockToolHandler: mockToolHandler{emitter: noopEmitter{}},
		pendingTraces:   true,
		tracesResult:    "trace data here",
	}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", true)

	require.NoError(t, err)
	assert.Equal(t, "final analysis with traces", result.Text)
}

func TestRunAgentLoop_DoneWithPendingTraces(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp GenerateResponse
		switch call {
		case 0:
			// Model calls done but there are pending traces
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "done",
						Args: map[string]any{"category": "diagnosis", "confidence": float64(85), "missing_information_sensitivity": "low"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
			}
		default:
			// After forceTraces, model returns final text
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{Text: "analysis with forced traces"}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 800},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
		call++
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandlerWithTraces{
		mockToolHandler: mockToolHandler{emitter: noopEmitter{}},
		pendingTraces:   true,
		tracesResult:    "trace data",
	}

	result, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", true)

	require.NoError(t, err)
	assert.Equal(t, "analysis with forced traces", result.Text)
	assert.Equal(t, 85, result.Confidence)
}

func TestRunAgentLoop_GenerateFinalEmptyResponse(t *testing.T) {
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp GenerateResponse
		switch call {
		case 0:
			// Step 1: model calls done
			resp = GenerateResponse{
				Candidates: []Candidate{{Content: Content{Parts: []Part{{
					FunctionCall: &FunctionCall{
						Name: "done",
						Args: map[string]any{"category": "diagnosis", "confidence": float64(80), "missing_information_sensitivity": "low"},
					},
				}}}}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
			}
		case 1:
			// Step 2: final response has no text (triggers retry in handleEmptyResponse)
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{}}}, FinishReason: "STOP"}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 600},
			}
		default:
			// Retry also returns empty
			resp = GenerateResponse{
				Candidates:    []Candidate{{Content: Content{Parts: []Part{{}}}, FinishReason: "STOP"}},
				UsageMetadata: &UsageMetadata{PromptTokenCount: 600},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
		call++
	}))
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	_, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestRunAgentLoop_GenerateFinalNoCandidates(t *testing.T) {
	ts := mockServer(t, []GenerateResponse{
		// Step 1: model calls done
		{
			Candidates: []Candidate{{Content: Content{Parts: []Part{{
				FunctionCall: &FunctionCall{
					Name: "done",
					Args: map[string]any{"category": "diagnosis", "confidence": float64(80), "missing_information_sensitivity": "low"},
				},
			}}}}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 500},
		},
		// Step 2: final response has no candidates
		{
			Candidates:    []Candidate{},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 600},
		},
	})
	defer ts.Close()

	c := &Client{apiKey: "test", baseURL: ts.URL, model: "m"}
	handler := &mockToolHandler{emitter: noopEmitter{}}

	_, err := c.RunAgentLoop(context.Background(), handler, testToolDeclarations(), "context", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}
