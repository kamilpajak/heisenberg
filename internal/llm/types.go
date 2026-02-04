package llm

// GenerateRequest is the request body for Gemini generateContent.
type GenerateRequest struct {
	Contents         []Content         `json:"contents"`
	Tools            []Tool            `json:"tools,omitempty"`
	GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
	SystemInstruction *Content         `json:"systemInstruction,omitempty"`
}

// GenerateResponse is the response from Gemini generateContent.
type GenerateResponse struct {
	Candidates []Candidate `json:"candidates"`
}

// Candidate is a single response candidate.
type Candidate struct {
	Content Content `json:"content"`
}

// Content represents a message in the conversation.
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

// Part is a union type: exactly one of Text, FunctionCall, or FunctionResponse is set.
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
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

// AnalysisResult holds the final output from the agent loop.
type AnalysisResult struct {
	Text        string `json:"text"`
	Category    string `json:"category"`    // "diagnosis", "no_failures", "not_supported", or "" (model skipped done)
	Confidence  int    `json:"confidence"`  // 0-100, meaningful only for "diagnosis"
	Sensitivity string `json:"sensitivity"` // "high", "medium", "low", meaningful only for "diagnosis"
}

// GenerationConfig controls response generation.
type GenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}
