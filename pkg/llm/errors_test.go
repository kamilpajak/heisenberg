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
		code int
		want string
	}{
		{429, "Wait a moment and retry, or check your Gemini API quota at ai.google.dev"},
		{401, "Check that GOOGLE_API_KEY is set and valid"},
		{403, "Check that GOOGLE_API_KEY is set and valid"},
		{500, "Gemini service may be experiencing issues — try again later"},
		{503, "Gemini service may be experiencing issues — try again later"},
		{418, "Run with --verbose for the full API response"},
	}
	for _, tt := range tests {
		err := &APIError{StatusCode: tt.code}
		assert.Equal(t, tt.want, err.Hint(), "code %d", tt.code)
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
