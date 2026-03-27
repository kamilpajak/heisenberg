package llm

import (
	"encoding/json"
	"fmt"
)

// APIError represents a non-200 response from the Gemini API.
// It preserves structured fields for human-friendly formatting in the CLI
// while keeping the raw body for --verbose output.
type APIError struct {
	StatusCode int    // HTTP status code (e.g. 429)
	Status     string // HTTP status text (e.g. "429 Too Many Requests")
	Provider   string // Error status from provider (e.g. "RESOURCE_EXHAUSTED")
	Message    string // Human-readable message from provider
	RawBody    string // Full response body for verbose/debug
	Retries    int    // Number of retries attempted before giving up
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("gemini API error: %s — %s", e.Status, e.Message)
	}
	return fmt.Sprintf("gemini API error: %s", e.Status)
}

// Hint returns an actionable suggestion based on the HTTP status code.
func (e *APIError) Hint() string {
	prefix := ""
	if e.Retries > 0 {
		prefix = fmt.Sprintf("Retried %d times. ", e.Retries)
	}

	switch {
	case e.StatusCode == 429:
		if e.Retries > 0 {
			return prefix + "This may be a per-minute rate limit (resets in ~1 min) or an exhausted daily quota (resets in ~24h). Check usage at ai.google.dev"
		}
		return "Wait a moment and retry, or check your Gemini API quota at ai.google.dev"
	case e.StatusCode == 401 || e.StatusCode == 403:
		return "Check that GOOGLE_API_KEY is set and valid"
	case e.StatusCode >= 500:
		return prefix + "Gemini service may be experiencing issues — try again later"
	default:
		return "Run with --verbose for the full API response"
	}
}

// ConfigError represents a configuration problem (e.g. missing API key).
type ConfigError struct {
	Message string
}

func (e *ConfigError) Error() string {
	return e.Message
}

// parseAPIError creates an APIError from a Gemini API HTTP response.
func parseAPIError(statusCode int, status string, body []byte) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
		Status:     status,
		RawBody:    string(body),
	}

	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil {
		apiErr.Message = errResp.Error.Message
		apiErr.Provider = errResp.Error.Status
	}

	return apiErr
}
