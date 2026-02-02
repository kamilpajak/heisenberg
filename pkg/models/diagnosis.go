package models

// Confidence represents the confidence level of a diagnosis
type Confidence string

const (
	ConfidenceHigh   Confidence = "HIGH"
	ConfidenceMedium Confidence = "MEDIUM"
	ConfidenceLow    Confidence = "LOW"
)

// Diagnosis represents an AI-generated analysis of test failures
type Diagnosis struct {
	RootCause             string     `json:"root_cause"`
	Evidence              []string   `json:"evidence"`
	SuggestedFix          string     `json:"suggested_fix"`
	Confidence            Confidence `json:"confidence"`
	ConfidenceExplanation string     `json:"confidence_explanation,omitempty"`
}

// AnalysisResult represents the complete result of analyzing a test report
type AnalysisResult struct {
	Diagnosis    Diagnosis `json:"diagnosis"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
}
