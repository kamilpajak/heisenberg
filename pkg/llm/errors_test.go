package llm

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *APIError
		wantMsg string
	}{
		{
			name:    "short message unchanged",
			err:     &APIError{StatusCode: 429, Status: "429 Too Many Requests", Message: "Resource exhausted"},
			wantMsg: "gemini API error: 429 Too Many Requests — Resource exhausted",
		},
		{
			name:    "without message",
			err:     &APIError{StatusCode: 500, Status: "500 Internal Server Error"},
			wantMsg: "gemini API error: 500 Internal Server Error",
		},
		{
			name: "long message truncated at first sentence",
			err: &APIError{
				StatusCode: 429,
				Status:     "429 Too Many Requests",
				Message:    "You exceeded your current quota, please check your plan and billing details. For more information on this error, head to: https://ai.google.dev/gemini-api/docs/rate-limits.",
			},
			wantMsg: "gemini API error: 429 Too Many Requests — You exceeded your current quota, please check your plan and billing details",
		},
		{
			name: "parenthetical not cut mid-clause",
			err: &APIError{
				StatusCode: 429,
				Status:     "429 Too Many Requests",
				Message:    "Resource has been exhausted (e.g. check quota).",
			},
			wantMsg: "gemini API error: 429 Too Many Requests — Resource has been exhausted (e.g. check quota).",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantMsg, tt.err.Error())
		})
	}
}

func TestAPIError_Hint(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		retries int
		want    string
	}{
		{"429 no retries", 429, 0, "Wait a moment and retry, or check your Gemini API quota at ai.google.dev"},
		{"429 with retries", 429, 3, "Retried 3 times. This may be a per-minute rate limit (resets in ~1 min) or an exhausted daily quota (resets in ~24h). Check usage at ai.google.dev"},
		{"401", 401, 0, "Check that GOOGLE_API_KEY is set and valid"},
		{"403", 403, 0, "Check that GOOGLE_API_KEY is set and valid"},
		{"500 no retries", 500, 0, "Gemini service may be experiencing issues — try again later"},
		{"500 with retries", 500, 2, "Retried 2 times. Gemini service may be experiencing issues — try again later"},
		{"503 with retries", 503, 3, "Retried 3 times. Gemini service may be experiencing issues — try again later"},
		{"418", 418, 0, "Run with --verbose for the full API response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &APIError{StatusCode: tt.code, Retries: tt.retries}
			assert.Equal(t, tt.want, err.Hint())
		})
	}
}

func TestParseAPIError(t *testing.T) {
	body := `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`
	err := parseAPIError(429, "429 Too Many Requests", []byte(body))

	assert.Equal(t, 429, err.StatusCode)
	assert.Equal(t, "429 Too Many Requests", err.Status)
	assert.Equal(t, "Resource has been exhausted (e.g. check quota).", err.Message)
	assert.Equal(t, "RESOURCE_EXHAUSTED", err.Provider)
	assert.Equal(t, body, err.RawBody)
}

func TestParseAPIError_InvalidJSON(t *testing.T) {
	err := parseAPIError(500, "500 Internal Server Error", []byte("not json"))

	assert.Equal(t, 500, err.StatusCode)
	assert.Equal(t, "", err.Message)
	assert.Equal(t, "not json", err.RawBody)
}

func TestParseAPIError_ExtractsQuotaInfo(t *testing.T) {
	body := `{"error":{"code":429,"message":"You exceeded your current quota, please check your plan and billing details. For more information on this error, head to: https://ai.google.dev/gemini-api/docs/rate-limits.\n\n* Quota exceeded for metric: generativelanguage.googleapis.com/generate_requests_per_model_per_day, limit: 250, model: gemini-3.1-pro\n\nPlease retry in 4h59m27.724879759s.","status":"RESOURCE_EXHAUSTED"}}`
	err := parseAPIError(429, "429 Too Many Requests", []byte(body))

	assert.Equal(t, "250 req/day for gemini-3.1-pro", err.QuotaDetail)
	assert.Equal(t, "5h", err.RetryAfter)
}

func TestParseAPIError_NoQuotaInfo(t *testing.T) {
	body := `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`
	err := parseAPIError(429, "429 Too Many Requests", []byte(body))

	assert.Equal(t, "", err.QuotaDetail)
	assert.Equal(t, "", err.RetryAfter)
}

func TestAPIError_Hint_WithQuotaDetails(t *testing.T) {
	err := &APIError{
		StatusCode:  429,
		Retries:     3,
		QuotaDetail: "250 req/day for gemini-3.1-pro",
		RetryAfter:  "5h",
	}
	hint := err.Hint()
	assert.Contains(t, hint, "Retried 3 times")
	assert.Contains(t, hint, "250 req/day for gemini-3.1-pro")
	assert.Contains(t, hint, "Resets in ~5h")
	assert.Contains(t, hint, "ai.google.dev")
}

func TestRoundHours(t *testing.T) {
	tests := []struct {
		hours, mins int
		want        string
	}{
		{4, 59, "5h"},
		{0, 45, "45m"},
		{0, 59, "59m"},
		{0, 20, "20m"},
		{0, 0, "0m"},
		{1, 0, "1h"},
		{0, 5, "5m"},
	}
	for _, tt := range tests {
		got := roundHours(tt.hours, tt.mins)
		assert.Equal(t, tt.want, got, "roundHours(%d, %d)", tt.hours, tt.mins)
	}
}

func TestAPIError_ErrorsAs(t *testing.T) {
	apiErr := parseAPIError(429, "429 Too Many Requests", []byte(`{}`))
	wrapped := fmt.Errorf("step 4: %w", apiErr)

	var target *APIError
	assert.True(t, errors.As(wrapped, &target))
	assert.Equal(t, 429, target.StatusCode)
}

func TestShortMessage_SentenceBoundary(t *testing.T) {
	e := &APIError{
		Message: "You exceeded your current quota, please check your plan and billing details. For more information on this error, head to: https://ai.google.dev/gemini-api/docs/rate-limits.",
	}
	msg := e.shortMessage()
	assert.Equal(t, "You exceeded your current quota, please check your plan and billing details", msg)
}

func TestShortMessage_NoCommaFallback(t *testing.T) {
	// No sentence boundary within threshold, but comma at position 35.
	// Old code would truncate at comma; new code truncates at hard limit.
	e := &APIError{
		Message: "Resource has been exhausted (e.g., check your plan and billing details and contact support for refund and upgrade)",
	}
	msg := e.shortMessage()
	// Should NOT cut at first ", " (position 33 inside parenthetical)
	assert.Len(t, msg, shortMessageThreshold, "should truncate at hard limit, not comma")
}

func TestShortMessage_Short(t *testing.T) {
	e := &APIError{Message: "Resource exhausted"}
	assert.Equal(t, "Resource exhausted", e.shortMessage())
}

func TestShortMessage_NoSentenceBoundary(t *testing.T) {
	e := &APIError{
		Message: strings.Repeat("a", 100),
	}
	msg := e.shortMessage()
	assert.Len(t, msg, shortMessageThreshold)
}
