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
	Items       *Schema           `json:"items,omitempty"` // For array types
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
	DiagnosisRCAs() []RootCauseAnalysis
	GetEmitter() ProgressEmitter
	HasTestArtifacts() bool
}

// SchemaV1 is the current JSON output schema version.
const SchemaV1 = "1"

// AnalysisResult holds the final output from the agent loop.
type AnalysisResult struct {
	Text               string              `json:"text"`
	Category           string              `json:"category"`                      // "diagnosis", "no_failures", "not_supported", or "" (model skipped done)
	Confidence         int                 `json:"confidence"`                    // 0-100, meaningful only for "diagnosis"
	Sensitivity        string              `json:"sensitivity"`                   // "high", "medium", "low", meaningful only for "diagnosis"
	RCAs               []RootCauseAnalysis `json:"analyses,omitempty"`            // Structured diagnoses, one per failing test
	RunID              int64               `json:"run_id,omitempty"`              // GitHub workflow run ID
	Owner              string              `json:"owner,omitempty"`               // Repository owner
	Repo               string              `json:"repo,omitempty"`                // Repository name
	Branch             string              `json:"branch,omitempty"`              // Git branch name
	CommitSHA          string              `json:"commit_sha,omitempty"`          // Git commit SHA
	Event              string              `json:"event,omitempty"`               // GitHub event type (push, pull_request)
	Eval               *EvalMeta           `json:"eval,omitempty"`                // Performance metadata for eval
	OriginalConfidence int                 `json:"original_confidence,omitempty"` // Pre-calibration confidence (0 = not adjusted)
	CalibrationReason  string              `json:"calibration_reason,omitempty"`  // Why confidence was capped
	Calibration        *CalibrationSignals `json:"calibration_signals,omitempty"` // Deterministic signals used for confidence adjustment
}

// CalibrationSignals contains deterministic signals for confidence adjustment.
// Exported with JSON tags to enable logging as training data for future
// data-driven calibration (#50).
type CalibrationSignals struct {
	BlastRadius            float64 `json:"blast_radius"`
	DiffIntersection       bool    `json:"diff_intersection"`
	AllSameErrorType       bool    `json:"all_same_error_type"`
	HasNetworkErrors       bool    `json:"has_network_errors"`
	BugLocationIsCode      bool    `json:"bug_location_is_code"`
	BugLocConfLow          bool    `json:"bug_loc_conf_low"`
	HasHiddenInfraEvidence bool    `json:"has_hidden_infra_evidence"`
	DiffTouchesErrorPaths  bool    `json:"diff_touches_error_paths"`
	LowIterations          bool    `json:"low_iterations"`
}

// EvalMeta captures performance metrics from the agent loop for evaluation.
type EvalMeta struct {
	Model         string `json:"model"`
	Iterations    int    `json:"iterations"`
	MaxIterations int    `json:"max_iterations"`
	ModelMs       int    `json:"model_ms"`
	Tokens        int    `json:"tokens"`
	WallMs        int    `json:"wall_ms"`
	Clustered     bool   `json:"clustered,omitempty"`
	ClusterCount  int    `json:"cluster_count,omitempty"`
	ClusterMethod string `json:"cluster_method,omitempty"`
}

// GenerationConfig controls response generation.
type GenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}
