package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/kamilpajak/heisenberg/pkg/models"
)

// Analyzer performs AI-powered analysis of test failures
type Analyzer struct {
	client llm.Client
}

// New creates a new Analyzer with the specified LLM provider
func New(provider llm.Provider, model, apiKey string) (*Analyzer, error) {
	client, err := llm.NewClient(llm.Config{
		Provider: provider,
		Model:    model,
		APIKey:   apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	return &Analyzer{client: client}, nil
}

// Analyze performs AI analysis on a test report
func (a *Analyzer) Analyze(ctx context.Context, report *models.Report) (*models.AnalysisResult, error) {
	if !report.HasFailures() {
		return nil, fmt.Errorf("no failures to analyze")
	}

	// Build messages
	messages := []llm.Message{
		{Role: "system", Content: GetSystemPrompt()},
		{Role: "user", Content: BuildPrompt(report)},
	}

	// Call LLM
	resp, err := a.client.Complete(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	// Parse response
	diagnosis, err := parseDiagnosis(resp.Content)
	if err != nil {
		// If parsing fails, create a basic diagnosis with the raw response
		diagnosis = &models.Diagnosis{
			RootCause:             resp.Content,
			Evidence:              []string{},
			SuggestedFix:          "Could not parse structured response",
			Confidence:            models.ConfidenceLow,
			ConfidenceExplanation: "Response was not in expected JSON format",
		}
	}

	return &models.AnalysisResult{
		Diagnosis:    *diagnosis,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		Provider:     string(a.client.Provider()),
		Model:        resp.Model,
	}, nil
}

// parseDiagnosis extracts the diagnosis from the LLM response
func parseDiagnosis(content string) (*models.Diagnosis, error) {
	// Try to extract JSON from the response
	// The response might have markdown code blocks or extra text
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var raw struct {
		RootCause             string   `json:"root_cause"`
		Evidence              []string `json:"evidence"`
		SuggestedFix          string   `json:"suggested_fix"`
		Confidence            string   `json:"confidence"`
		ConfidenceExplanation string   `json:"confidence_explanation"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Map confidence string to enum
	confidence := models.ConfidenceMedium
	switch strings.ToUpper(raw.Confidence) {
	case "HIGH":
		confidence = models.ConfidenceHigh
	case "LOW":
		confidence = models.ConfidenceLow
	}

	return &models.Diagnosis{
		RootCause:             raw.RootCause,
		Evidence:              raw.Evidence,
		SuggestedFix:          raw.SuggestedFix,
		Confidence:            confidence,
		ConfidenceExplanation: raw.ConfidenceExplanation,
	}, nil
}

// extractJSON attempts to extract a JSON object from text that may contain markdown
func extractJSON(content string) string {
	// Try to find JSON in code blocks first
	if start := strings.Index(content, "```json"); start != -1 {
		start += 7 // len("```json")
		if end := strings.Index(content[start:], "```"); end != -1 {
			return strings.TrimSpace(content[start : start+end])
		}
	}

	// Try plain code blocks
	if start := strings.Index(content, "```"); start != -1 {
		start += 3
		if end := strings.Index(content[start:], "```"); end != -1 {
			candidate := strings.TrimSpace(content[start : start+end])
			if strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}

	// Try to find raw JSON object
	if start := strings.Index(content, "{"); start != -1 {
		// Find matching closing brace
		depth := 0
		for i := start; i < len(content); i++ {
			switch content[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return content[start : i+1]
				}
			}
		}
	}

	return ""
}
