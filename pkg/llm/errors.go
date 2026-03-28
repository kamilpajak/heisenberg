package llm

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// APIError represents a non-200 response from the Gemini API.
// It preserves structured fields for human-friendly formatting in the CLI
// while keeping the raw body for --verbose output.
type APIError struct {
	StatusCode  int    // HTTP status code (e.g. 429)
	Status      string // HTTP status text (e.g. "429 Too Many Requests")
	Provider    string // Error status from provider (e.g. "RESOURCE_EXHAUSTED")
	Message     string // Human-readable message from provider (may be long)
	RawBody     string // Full response body for verbose/debug
	Retries     int    // Number of retries attempted before giving up
	QuotaDetail string // Parsed quota info, e.g. "250 req/day for gemini-3.1-pro"
	RetryAfter  string // Parsed retry time, e.g. "5h"
}

func (e *APIError) Error() string {
	msg := e.shortMessage()
	if msg != "" {
		return fmt.Sprintf("gemini API error: %s — %s", e.Status, msg)
	}
	return fmt.Sprintf("gemini API error: %s", e.Status)
}

// shortMessage returns the first clause of Message, truncating at ", " or ". ".
const shortMessageThreshold = 80

func (e *APIError) shortMessage() string {
	msg := e.Message
	if msg == "" || len(msg) <= shortMessageThreshold {
		return msg
	}
	// Truncate at first sentence boundary (". "), then fall back to comma
	if idx := strings.Index(msg, ". "); idx > 0 && idx <= shortMessageThreshold {
		return msg[:idx]
	}
	if idx := strings.Index(msg, ", "); idx > 0 && idx <= shortMessageThreshold {
		return msg[:idx]
	}
	return msg[:shortMessageThreshold]
}

// Hint returns an actionable suggestion based on the HTTP status code.
func (e *APIError) Hint() string {
	prefix := ""
	if e.Retries > 0 {
		prefix = fmt.Sprintf("Retried %d times.", e.Retries)
	}

	switch {
	case e.StatusCode == 429 && e.Retries > 0:
		return e.hint429WithRetries(prefix)
	case e.StatusCode == 429:
		return "Wait a moment and retry, or check your Gemini API quota at ai.google.dev"
	case e.StatusCode == 401 || e.StatusCode == 403:
		return "Check that GOOGLE_API_KEY is set and valid"
	case e.StatusCode >= 500 && prefix != "":
		return prefix + " Gemini service may be experiencing issues — try again later"
	case e.StatusCode >= 500:
		return "Gemini service may be experiencing issues — try again later"
	default:
		return "Run with --verbose for the full API response"
	}
}

func (e *APIError) hint429WithRetries(prefix string) string {
	parts := []string{prefix}
	if e.QuotaDetail != "" {
		parts = append(parts, "Daily quota: "+e.QuotaDetail+".")
	}
	if e.RetryAfter != "" {
		parts = append(parts, "Resets in ~"+e.RetryAfter+".")
	}
	if e.QuotaDetail == "" && e.RetryAfter == "" {
		parts = append(parts, "This may be a per-minute rate limit (resets in ~1 min) or an exhausted daily quota (resets in ~24h).")
	}
	parts = append(parts, "Check usage at ai.google.dev")
	return strings.Join(parts, " ")
}

// ConfigError represents a configuration problem (e.g. missing API key).
type ConfigError struct {
	Message string
}

func (e *ConfigError) Error() string {
	return e.Message
}

var (
	quotaRe = regexp.MustCompile(`limit:\s*(\d+),\s*model:\s*([\w.-]+)`)
	retryRe = regexp.MustCompile(`retry in\s+(\d+)h(\d+)m`)
)

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

		// Extract quota details from verbose Gemini messages
		if m := quotaRe.FindStringSubmatch(errResp.Error.Message); m != nil {
			apiErr.QuotaDetail = m[1] + " req/day for " + m[2]
		}
		if m := retryRe.FindStringSubmatch(errResp.Error.Message); m != nil {
			hours, _ := strconv.Atoi(m[1])
			mins, _ := strconv.Atoi(m[2])
			apiErr.RetryAfter = roundHours(hours, mins)
		}
	}

	return apiErr
}

// roundHours produces a human-friendly duration like "5h" or "30m".
func roundHours(hours, mins int) string {
	total := float64(hours) + float64(mins)/60
	rounded := int(math.Round(total))
	if rounded < 1 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh", rounded)
}
