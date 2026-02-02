package analyzer

import (
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain JSON",
			input:    `{"root_cause": "test"}`,
			expected: `{"root_cause": "test"}`,
		},
		{
			name:     "JSON in markdown code block",
			input:    "Here is the analysis:\n```json\n{\"root_cause\": \"test\"}\n```\nDone.",
			expected: `{"root_cause": "test"}`,
		},
		{
			name:     "JSON in plain code block",
			input:    "Analysis:\n```\n{\"root_cause\": \"test\"}\n```",
			expected: `{"root_cause": "test"}`,
		},
		{
			name:     "JSON with surrounding text",
			input:    "The result is {\"root_cause\": \"test\"} as shown.",
			expected: `{"root_cause": "test"}`,
		},
		{
			name:     "nested JSON",
			input:    `{"outer": {"inner": "value"}}`,
			expected: `{"outer": {"inner": "value"}}`,
		},
		{
			name:     "no JSON",
			input:    "No JSON here",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseDiagnosis(t *testing.T) {
	validJSON := `{
		"root_cause": "The test timed out",
		"evidence": ["Evidence 1", "Evidence 2"],
		"suggested_fix": "Increase timeout",
		"confidence": "HIGH",
		"confidence_explanation": "Clear timeout error"
	}`

	diagnosis, err := parseDiagnosis(validJSON)
	if err != nil {
		t.Fatalf("parseDiagnosis failed: %v", err)
	}

	if diagnosis.RootCause != "The test timed out" {
		t.Errorf("Unexpected root cause: %s", diagnosis.RootCause)
	}

	if len(diagnosis.Evidence) != 2 {
		t.Errorf("Expected 2 evidence items, got %d", len(diagnosis.Evidence))
	}

	if diagnosis.SuggestedFix != "Increase timeout" {
		t.Errorf("Unexpected suggested fix: %s", diagnosis.SuggestedFix)
	}

	if diagnosis.Confidence != "HIGH" {
		t.Errorf("Expected HIGH confidence, got %s", diagnosis.Confidence)
	}
}

func TestParseDiagnosis_InvalidJSON(t *testing.T) {
	_, err := parseDiagnosis("not json")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}
