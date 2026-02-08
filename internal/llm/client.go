package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// emit sends a progress event via the executor's emitter, if set.
func emit(h ToolExecutor, ev ProgressEvent) {
	if h != nil && h.GetEmitter() != nil {
		h.GetEmitter().Emit(ev)
	}
}

const maxIterations = 20
const softLimitIteration = 15

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// previewExcerpt returns a short excerpt of s, preferring content around error
// keywords. Strips ANSI codes. Falls back to the tail if no keywords found.
// Uses rune-based slicing to avoid splitting multi-byte UTF-8 characters.
func previewExcerpt(s string, maxLen int) string {
	clean := ansiRe.ReplaceAllString(s, "")
	runes := []rune(clean)
	if len(runes) <= maxLen {
		return clean
	}
	lower := strings.ToLower(clean)
	// Specific patterns first; generic "error" last (catches ##[error] boilerplate)
	for _, kw := range []string{"FAIL", "Error:", "timeout", "panic", "error"} {
		target := strings.ToLower(kw)
		if idx := strings.LastIndex(lower, target); idx != -1 {
			// Convert byte index to rune index
			runeIdx := len([]rune(clean[:idx]))
			start := max(0, runeIdx-maxLen/4)
			end := min(len(runes), start+maxLen)
			return "..." + string(runes[start:end]) + "..."
		}
	}
	return "..." + string(runes[len(runes)-maxLen:])
}

// Client handles Gemini API calls with function calling support.
type Client struct {
	apiKey  string
	baseURL string
	model   string
}

// isEmptyResponse checks if the model returned neither text nor function calls.
func isEmptyResponse(c *Candidate) bool {
	if c == nil {
		return true
	}
	for _, p := range c.Content.Parts {
		if p.Text != "" || p.FunctionCall != nil {
			return false
		}
	}
	return true
}

// describeEmptyResponse builds a diagnostic message for empty model responses.
func describeEmptyResponse(c *Candidate) string {
	if c == nil {
		return "no candidate"
	}
	msg := fmt.Sprintf("finishReason=%s", c.FinishReason)
	for _, sr := range c.SafetyRatings {
		if sr.Blocked || sr.Probability == "HIGH" || sr.Probability == "MEDIUM" {
			msg += fmt.Sprintf(", safety %s=%s", sr.Category, sr.Probability)
			if sr.Blocked {
				msg += " (blocked)"
			}
		}
	}
	return msg
}

// NewClient creates a new LLM client (Google Gemini).
func NewClient() (*Client, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLE_API_KEY environment variable required")
	}

	return &Client{
		apiKey:  apiKey,
		baseURL: "https://generativelanguage.googleapis.com/v1beta",
		model:   "gemini-3-pro-preview",
	}, nil
}

func systemPrompt() *Content {
	return &Content{
		Parts: []Part{{Text: `You are an expert CI/CD failure analyst. You have access to tools that let you inspect a GitHub Actions workflow run.

Your goal: determine the root cause of test failures and provide actionable guidance.

Strategy:
1. Review the initial context (run info, jobs, artifacts)
2. Use tools to gather more data — fetch artifacts, read logs, inspect config files
3. Focus on FAILED jobs and their logs
4. For Playwright test failures, fetch the HTML report artifact first
5. Use get_test_traces on test-results artifacts to get browser actions, console errors, and failed HTTP requests from Playwright trace recordings
6. When you have enough information, you MUST call the "done" tool first, then provide your analysis text. Never skip the done tool.

When calling the "done" tool, classify your conclusion:
- category "diagnosis": you identified a specific failure root cause. Include confidence (0-100) and missing_information_sensitivity.
- category "no_failures": all tests are passing, nothing to diagnose.
- category "not_supported": the test framework or artifact format cannot be analyzed.

For "diagnosis" category:
  Confidence scoring (0-100):
  - 80-100: Clear root cause identified with strong evidence
  - 40-79: Likely cause identified but some ambiguity remains
  - 0-39: Uncertain, multiple possible causes or insufficient evidence

  Missing information sensitivity:
  - high: Backend logs, Docker state, or CI environment data would likely reveal the root cause
  - medium: Additional data might help but current evidence is reasonable
  - low: Diagnosis is well-supported by frontend/test evidence alone

Be thorough but efficient. Don't fetch data you don't need.`}},
	}
}

// extractCalls returns all function calls from a model response.
func extractCalls(c Content) []FunctionCall {
	var calls []FunctionCall
	for _, p := range c.Parts {
		if p.FunctionCall != nil {
			calls = append(calls, *p.FunctionCall)
		}
	}
	return calls
}

// collectTexts returns all text parts from a model response.
func collectTexts(c Content) []string {
	var texts []string
	for _, p := range c.Parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return texts
}

// stepInfo bundles per-step metadata threaded through the agent loop.
type stepInfo struct {
	step      int
	iteration int
	verbose   bool
	modelMs   int
	tokens    int
}

// loopState tracks mutable state across agent loop iterations.
type loopState struct {
	history        []Content
	pendingDone    bool
	hasCalledTools bool
	doneNudged     bool
	savedText      string
	calledTools    map[string]bool // tracks tool+args hashes to detect duplicates
	softWarned     bool            // true after soft limit warning injected
}

// RunAgentLoop runs the agentic conversation loop. The model can call tools
// iteratively until it produces a text response or hits the iteration limit.
func (c *Client) RunAgentLoop(ctx context.Context, handler ToolExecutor, toolDecls []FunctionDeclaration, initialContext string, verbose bool) (*AnalysisResult, error) {
	tools := []Tool{{FunctionDeclarations: toolDecls}}
	system := systemPrompt()

	s := &loopState{
		history: []Content{
			{Role: "user", Parts: []Part{{Text: initialContext}}},
		},
		calledTools: make(map[string]bool),
	}

	for i := range maxIterations {
		si := &stepInfo{step: i + 1, iteration: i, verbose: verbose}

		// Inject soft limit warning at iteration 15
		if i == softLimitIteration && !s.softWarned {
			s.softWarned = true
			remaining := maxIterations - i
			s.history = append(s.history, Content{
				Role:  "user",
				Parts: []Part{{Text: fmt.Sprintf("Note: You have %d iterations remaining. Please consolidate your findings and move toward a final diagnosis soon.", remaining)}},
			})
		}

		stepMsg := "Calling model..."
		if s.pendingDone {
			stepMsg = "Generating final analysis..."
		}
		emit(handler, ProgressEvent{Type: "step", Step: si.step, MaxStep: maxIterations, Message: stepMsg})

		candidate, err := c.callModel(ctx, s, tools, system, handler, si)
		if err != nil {
			return nil, err
		}

		modelContent := candidate.Content
		modelContent.Role = "model"
		s.history = append(s.history, modelContent)

		result, err := c.processResponse(ctx, s, handler, modelContent, si)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
	}

	if s.pendingDone {
		return c.generateFinal(ctx, s.history, tools, system, handler, verbose)
	}

	return nil, fmt.Errorf("agent loop exceeded %d iterations without completing", maxIterations)
}

// callModel calls the LLM and handles empty response retries.
// It populates si.modelMs and si.tokens.
func (c *Client) callModel(ctx context.Context, s *loopState, tools []Tool, system *Content, handler ToolExecutor, si *stepInfo) (*Candidate, error) {
	t0 := time.Now()
	resp, err := c.generate(ctx, s.history, tools, system)
	si.modelMs = int(time.Since(t0).Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("step %d: %w", si.step, err)
	}

	if resp.UsageMetadata != nil {
		si.tokens = resp.UsageMetadata.PromptTokenCount
	}

	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("step %d: empty response from model", si.step)
	}

	candidate := &resp.Candidates[0]
	if isEmptyResponse(candidate) {
		candidate, err = c.handleEmptyResponse(ctx, s, tools, system, handler, si, candidate)
		if err != nil {
			return nil, err
		}
	}

	return candidate, nil
}

// processResponse dispatches the model response to text or tool handling.
// Returns a non-nil result when the loop should return, nil when it should continue.
func (c *Client) processResponse(ctx context.Context, s *loopState, handler ToolExecutor, content Content, si *stepInfo) (*AnalysisResult, error) {
	calls := extractCalls(content)
	if len(calls) == 0 {
		return c.handleTextResponse(ctx, s, handler, content, si)
	}
	return c.handleToolResponse(ctx, s, handler, calls, si)
}

// handleTextResponse processes a text-only model response, handling pending
// traces, done-nudge logic, and result building.
func (c *Client) handleTextResponse(ctx context.Context, s *loopState, handler ToolExecutor, content Content, si *stepInfo) (*AnalysisResult, error) {
	if si.verbose {
		emit(handler, ProgressEvent{Type: "result", Step: si.step, MaxStep: maxIterations, ModelMs: si.modelMs, Tokens: si.tokens})
	}

	// If traces are pending, force a trace fetch before finishing.
	if handler.HasPendingTraces() {
		traceResult := c.forceTraces(ctx, handler, si.step, maxIterations, si.verbose)
		s.history = append(s.history, Content{
			Role:  "user",
			Parts: []Part{{Text: "I also fetched the Playwright traces. Incorporate this data into your analysis:\n\n" + traceResult}},
		})
		return nil, nil
	}

	// Nudge fallback: model was nudged but still didn't call done.
	// Return the original analysis text with defaults.
	if s.doneNudged && handler.DiagnosisCategory() == "" {
		return buildResult([]string{s.savedText}, handler), nil
	}

	texts := collectTexts(content)
	if len(texts) == 0 {
		return nil, fmt.Errorf("step %d: model returned neither text nor function calls", si.step)
	}

	// Nudge: model returned text without calling done after a real analysis.
	// Save the text, ask the model to call done, and continue.
	if handler.DiagnosisCategory() == "" && s.hasCalledTools && !s.doneNudged && si.iteration < maxIterations-1 {
		s.savedText = strings.Join(texts, "\n")
		s.doneNudged = true
		s.history = append(s.history, Content{
			Role:  "user",
			Parts: []Part{{Text: "You provided your analysis but forgot to call the 'done' tool. Please call the 'done' tool now with your confidence and missing_information_sensitivity assessment. Do not repeat your analysis text."}},
		})
		emit(handler, ProgressEvent{Type: "step", Step: si.step, MaxStep: maxIterations, Message: "Requesting structured metadata..."})
		return nil, nil
	}

	return buildResult(texts, handler), nil
}

// handleToolResponse executes tool calls and handles done signalling,
// pending traces, and nudge-based early returns.
func (c *Client) handleToolResponse(ctx context.Context, s *loopState, handler ToolExecutor, calls []FunctionCall, si *stepInfo) (*AnalysisResult, error) {
	responseParts, done, err := c.executeCalls(ctx, s, handler, calls, si)
	if err != nil {
		return nil, err
	}
	s.hasCalledTools = true
	s.history = append(s.history, Content{Role: "user", Parts: responseParts})

	// If model called done but traces are pending, inject trace data first.
	if done && handler.HasPendingTraces() {
		traceResult := c.forceTraces(ctx, handler, si.step, maxIterations, si.verbose)
		s.history = append(s.history, Content{
			Role:  "user",
			Parts: []Part{{Text: "Before your final analysis, here is Playwright trace data you must incorporate:\n\n" + traceResult}},
		})
		return nil, nil
	}

	// If model called done after a nudge, return the saved analysis text
	// with the structured metadata from the done call.
	if done && s.savedText != "" {
		return buildResult([]string{s.savedText}, handler), nil
	}

	if done {
		s.pendingDone = true
	}

	return nil, nil
}

// buildResult creates an AnalysisResult from collected text parts, applying
// defaults when the model skipped the done tool.
func buildResult(texts []string, handler ToolExecutor) *AnalysisResult {
	result := &AnalysisResult{
		Text:        strings.Join(texts, "\n"),
		Category:    handler.DiagnosisCategory(),
		Confidence:  handler.DiagnosisConfidence(),
		Sensitivity: handler.DiagnosisSensitivity(),
	}
	if result.Category == "" {
		result.Category = CategoryDiagnosis
		result.Confidence = 50
		result.Sensitivity = "medium"
	}
	return result
}

// handleEmptyResponse retries once when the model returns an empty response
// (a known Gemini issue with reasoning tokens consuming the output budget).
// It mutates s.history and si.modelMs/si.tokens in place.
func (c *Client) handleEmptyResponse(
	ctx context.Context,
	s *loopState,
	tools []Tool,
	system *Content,
	handler ToolExecutor,
	si *stepInfo,
	candidate *Candidate,
) (*Candidate, error) {
	diag := describeEmptyResponse(candidate)
	if si.verbose {
		emit(handler, ProgressEvent{Type: "result", Step: si.step, MaxStep: maxIterations, ModelMs: si.modelMs, Tokens: si.tokens})
	}
	s.history = append(s.history, Content{
		Role:  "user",
		Parts: []Part{{Text: "Please provide your analysis or call a tool. Do not respond with empty content."}},
	})
	emit(handler, ProgressEvent{Type: "step", Step: si.step, MaxStep: maxIterations, Message: "Retrying (empty response)..."})
	t0 := time.Now()
	resp, err := c.generate(ctx, s.history, tools, system)
	si.modelMs = int(time.Since(t0).Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("step %d retry: %w", si.step, err)
	}
	if resp.UsageMetadata != nil {
		si.tokens = resp.UsageMetadata.PromptTokenCount
	}
	if len(resp.Candidates) == 0 || isEmptyResponse(&resp.Candidates[0]) {
		return nil, fmt.Errorf("step %d: model returned empty response after retry (%s)", si.step, diag)
	}
	return &resp.Candidates[0], nil
}

// callKey creates a unique key for a tool call based on name and arguments.
func callKey(call FunctionCall) string {
	argsJSON, _ := json.Marshal(call.Args)
	return call.Name + ":" + string(argsJSON)
}

// executeCalls runs each function call, emitting progress events and collecting responses.
// It detects duplicate calls (same tool + args) and returns an error to the model instead of re-executing.
func (c *Client) executeCalls(ctx context.Context, s *loopState, handler ToolExecutor, calls []FunctionCall, si *stepInfo) ([]Part, bool, error) {
	var responseParts []Part
	done := false
	for ci, call := range calls {
		toolEvent := ProgressEvent{Type: "tool", Step: si.step, MaxStep: maxIterations, Tool: call.Name}
		if si.verbose && len(call.Args) > 0 {
			argsJSON, _ := json.Marshal(call.Args)
			toolEvent.Args = string(argsJSON)
		}
		emit(handler, toolEvent)

		// Check for duplicate calls (same tool + args)
		key := callKey(call)
		if call.Name != "done" && s.calledTools[key] {
			// Return error to model instead of re-executing
			result := `{"error": "You already called this tool with these exact arguments. Analyze the data you have or try different arguments."}`
			emitToolResult(handler, si, ci, len(calls), 0, result)
			responseParts = append(responseParts, Part{
				FunctionResponse: &FunctionResponse{
					Name:     call.Name,
					Response: map[string]any{"result": result},
				},
			})
			continue
		}
		s.calledTools[key] = true

		t1 := time.Now()
		result, isDone, err := handler.Execute(ctx, call)
		toolMs := int(time.Since(t1).Milliseconds())
		if err != nil {
			return nil, false, fmt.Errorf("step %d, tool %s: %w", si.step, call.Name, err)
		}

		if isDone {
			done = true
		}

		emitToolResult(handler, si, ci, len(calls), toolMs, result)

		responseParts = append(responseParts, Part{
			FunctionResponse: &FunctionResponse{
				Name:     call.Name,
				Response: map[string]any{"result": result},
			},
		})
	}
	return responseParts, done, nil
}

// emitToolResult sends a verbose progress event for a completed tool call.
func emitToolResult(handler ToolExecutor, si *stepInfo, ci, totalCalls, toolMs int, result string) {
	if !si.verbose {
		return
	}
	ev := ProgressEvent{
		Type:    "result",
		Step:    si.step,
		MaxStep: maxIterations,
		Chars:   len(result),
		ToolMs:  toolMs,
	}
	// Attach model stats to the last tool call in this step
	if ci == totalCalls-1 {
		ev.ModelMs = si.modelMs
		ev.Tokens = si.tokens
	}
	// Keep Preview for SSE backward compat
	if len(result) > 0 {
		ev.Preview = previewExcerpt(result, 200)
	}
	emit(handler, ev)
}

// generateFinal performs one last model call after the done tool was signalled,
// extracting the final text analysis.
func (c *Client) generateFinal(
	ctx context.Context,
	history []Content,
	tools []Tool,
	system *Content,
	handler ToolExecutor,
	verbose bool,
) (*AnalysisResult, error) {
	emit(handler, ProgressEvent{Type: "step", Step: maxIterations, MaxStep: maxIterations, Message: "Generating final analysis..."})
	t0 := time.Now()
	resp, err := c.generate(ctx, history, tools, system)
	finalModelMs := int(time.Since(t0).Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("final step: %w", err)
	}
	finalTokens := 0
	if resp.UsageMetadata != nil {
		finalTokens = resp.UsageMetadata.PromptTokenCount
	}
	if verbose {
		emit(handler, ProgressEvent{Type: "result", Step: maxIterations, MaxStep: maxIterations, ModelMs: finalModelMs, Tokens: finalTokens})
	}
	if len(resp.Candidates) > 0 {
		texts := collectTexts(resp.Candidates[0].Content)
		if len(texts) > 0 {
			return buildResult(texts, handler), nil
		}
	}
	return nil, fmt.Errorf("agent loop exceeded %d iterations without completing", maxIterations)
}

// forceTraces calls get_test_traces programmatically when the model skips it.
func (c *Client) forceTraces(ctx context.Context, handler ToolExecutor, step, maxIter int, verbose bool) string {
	emit(handler, ProgressEvent{Type: "tool", Step: step, MaxStep: maxIter, Tool: "get_test_traces", Message: "Forcing get_test_traces (model skipped it)"})
	t0 := time.Now()
	result, _, _ := handler.Execute(ctx, FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})
	toolMs := int(time.Since(t0).Milliseconds())
	if verbose {
		ev := ProgressEvent{Type: "result", Step: step, MaxStep: maxIter, Chars: len(result), ToolMs: toolMs}
		if len(result) > 0 {
			ev.Preview = previewExcerpt(result, 200)
		}
		emit(handler, ev)
	}
	return result
}

func (c *Client) generate(ctx context.Context, history []Content, tools []Tool, system *Content) (*GenerateResponse, error) {
	req := GenerateRequest{
		Contents:          history,
		Tools:             tools,
		SystemInstruction: system,
		GenerationConfig: &GenerationConfig{
			Temperature:     0.1,
			MaxOutputTokens: 8192,
		},
	}

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, c.model, c.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini API error: %s - %s", resp.Status, string(body))
	}

	var result GenerateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}
