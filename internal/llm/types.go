package llm

import "context"

// GenerateRequest is the request body for Gemini generateContent.
type GenerateRequest struct {
	Contents          []Content         `json:"contents"`
	Tools             []Tool            `json:"tools,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
}

// GenerateResponse is the response from Gemini generateContent.
type GenerateResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// UsageMetadata contains token usage information from Gemini.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Candidate is a single response candidate.
type Candidate struct {
	Content       Content        `json:"content"`
	FinishReason  string         `json:"finishReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// SafetyRating indicates content safety assessment.
type SafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
	Blocked     bool   `json:"blocked,omitempty"`
}

// Content represents a message in the conversation.
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

// Part is a union type: exactly one of Text, FunctionCall, or FunctionResponse is set.
// ThoughtSignature is an opaque token returned by Gemini 3+ models that must be
// preserved on functionCall parts when sending history back to the API.
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
}

// FunctionCall is a tool invocation requested by the model.
type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// FunctionResponse is the result of executing a tool.
type FunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// Tool declares available functions.
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations"`
}

// FunctionDeclaration describes a callable function.
type FunctionDeclaration struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Parameters  *Schema `json:"parameters,omitempty"`
}

// Schema describes the JSON schema for function parameters.
type Schema struct {
	Type        string            `json:"type"`
	Description string            `json:"description,omitempty"`
	Properties  map[string]Schema `json:"properties,omitempty"`
	Required    []string          `json:"required,omitempty"`
	Enum        []string          `json:"enum,omitempty"`
}

// Outcome categories for the done tool.
const (
	CategoryDiagnosis    = "diagnosis"
	CategoryNoFailures   = "no_failures"
	CategoryNotSupported = "not_supported"
)

// ToolExecutor executes tool calls on behalf of the agent loop.
// Implementations handle domain-specific tool logic (GitHub, traces, etc).
type ToolExecutor interface {
	Execute(ctx context.Context, call FunctionCall) (string, bool, error)
	HasPendingTraces() bool
	DiagnosisCategory() string
	DiagnosisConfidence() int
	DiagnosisSensitivity() string
	DiagnosisRCA() *RootCauseAnalysis
	GetEmitter() ProgressEmitter
}

// AnalysisResult holds the final output from the agent loop.
type AnalysisResult struct {
	Text        string             `json:"text"`
	Category    string             `json:"category"`      // "diagnosis", "no_failures", "not_supported", or "" (model skipped done)
	Confidence  int                `json:"confidence"`    // 0-100, meaningful only for "diagnosis"
	Sensitivity string             `json:"sensitivity"`   // "high", "medium", "low", meaningful only for "diagnosis"
	RCA         *RootCauseAnalysis `json:"rca,omitempty"` // Structured diagnosis, only for "diagnosis"
}

// GenerationConfig controls response generation.
type GenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}
