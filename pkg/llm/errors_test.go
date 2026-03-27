package llm

import (
	"errors"
	"fmt"
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
			name:    "with message",
			err:     &APIError{StatusCode: 429, Status: "429 Too Many Requests", Message: "Resource exhausted"},
			wantMsg: "gemini API error: 429 Too Many Requests — Resource exhausted",
		},
		{
			name:    "without message",
			err:     &APIError{StatusCode: 500, Status: "500 Internal Server Error"},
			wantMsg: "gemini API error: 500 Internal Server Error",
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

func TestAPIError_ErrorsAs(t *testing.T) {
	apiErr := parseAPIError(429, "429 Too Many Requests", []byte(`{}`))
	wrapped := fmt.Errorf("step 4: %w", apiErr)

	var target *APIError
	assert.True(t, errors.As(wrapped, &target))
	assert.Equal(t, 429, target.StatusCode)
}
