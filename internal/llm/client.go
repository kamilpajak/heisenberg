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

// emit sends a progress event via the handler's emitter, if set.
func emit(h *ToolHandler, ev ProgressEvent) {
	if h != nil && h.Emitter != nil {
		h.Emitter.Emit(ev)
	}
}

const maxIterations = 10

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// previewExcerpt returns a short excerpt of s, preferring content around error
// keywords. Strips ANSI codes. Falls back to the tail if no keywords found.
func previewExcerpt(s string, maxLen int) string {
	clean := ansiRe.ReplaceAllString(s, "")
	if len(clean) <= maxLen {
		return clean
	}
	lower := strings.ToLower(clean)
	// Specific patterns first; generic "error" last (catches ##[error] boilerplate)
	for _, kw := range []string{"FAIL", "Error:", "timeout", "panic", "error"} {
		target := strings.ToLower(kw)
		if idx := strings.LastIndex(lower, target); idx != -1 {
			start := max(0, idx-maxLen/4)
			end := min(len(clean), start+maxLen)
			return "..." + clean[start:end] + "..."
		}
	}
	return "..." + clean[len(clean)-maxLen:]
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

// RunAgentLoop runs the agentic conversation loop. The model can call tools
// iteratively until it produces a text response or hits the iteration limit.
func (c *Client) RunAgentLoop(ctx context.Context, handler *ToolHandler, initialContext string, verbose bool) (*AnalysisResult, error) {
	tools := []Tool{{FunctionDeclarations: ToolDeclarations()}}

	system := &Content{
		Parts: []Part{{Text: `You are an expert CI/CD failure analyst. You have access to tools that let you inspect a GitHub Actions workflow run.

Your goal: determine the root cause of test failures and provide actionable guidance.

Strategy:
1. Review the initial context (run info, jobs, artifacts)
2. Use tools to gather more data — fetch artifacts, read logs, inspect config files
3. Focus on FAILED jobs and their logs
4. For Playwright test failures, fetch the HTML report artifact first
5. Use get_test_traces on test-results artifacts to get browser actions, console errors, and failed HTTP requests from Playwright trace recordings
6. When you have enough information, call the "done" tool, then provide your analysis

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

	history := []Content{
		{Role: "user", Parts: []Part{{Text: initialContext}}},
	}

	pendingDone := false
	for i := range maxIterations {
		step := i + 1
		stepMsg := "Calling model..."
		if pendingDone {
			stepMsg = "Generating final analysis..."
		}
		emit(handler, ProgressEvent{Type: "step", Step: step, MaxStep: maxIterations, Message: stepMsg})

		t0 := time.Now()
		resp, err := c.generate(ctx, history, tools, system)
		modelMs := int(time.Since(t0).Milliseconds())
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", step, err)
		}

		tokens := 0
		if resp.UsageMetadata != nil {
			tokens = resp.UsageMetadata.PromptTokenCount
		}

		if len(resp.Candidates) == 0 {
			return nil, fmt.Errorf("step %d: empty response from model", step)
		}

		candidate := &resp.Candidates[0]

		if isEmptyResponse(candidate) {
			history, candidate, modelMs, tokens, err = c.handleEmptyResponse(ctx, history, tools, system, handler, step, verbose, modelMs, tokens, candidate)
			if err != nil {
				return nil, err
			}
		}

		modelContent := candidate.Content
		modelContent.Role = "model"
		history = append(history, modelContent)

		// Check for function calls
		var calls []FunctionCall
		for _, p := range modelContent.Parts {
			if p.FunctionCall != nil {
				calls = append(calls, *p.FunctionCall)
			}
		}

		if len(calls) == 0 {
			// No function calls — model returned text.
			if verbose {
				emit(handler, ProgressEvent{Type: "result", Step: step, MaxStep: maxIterations, ModelMs: modelMs, Tokens: tokens})
			}

			// But if traces are pending, force a trace fetch before finishing.
			if handler.HasPendingTraces() {
				traceResult := c.forceTraces(ctx, handler, step, maxIterations, verbose)
				history = append(history, Content{
					Role:  "user",
					Parts: []Part{{Text: "I also fetched the Playwright traces. Incorporate this data into your analysis:\n\n" + traceResult}},
				})
				continue
			}

			var texts []string
			for _, p := range modelContent.Parts {
				if p.Text != "" {
					texts = append(texts, p.Text)
				}
			}
			if len(texts) == 0 {
				// This shouldn't happen after the empty response check above, but guard anyway
				return nil, fmt.Errorf("step %d: model returned neither text nor function calls", step)
			}
			return buildResult(texts, handler), nil
		}

		responseParts, done, err := c.executeCalls(ctx, handler, calls, step, verbose, modelMs, tokens)
		if err != nil {
			return nil, err
		}

		history = append(history, Content{Role: "user", Parts: responseParts})

		// If model called done but traces are pending, inject trace data first.
		if done && handler.HasPendingTraces() {
			traceResult := c.forceTraces(ctx, handler, step, maxIterations, verbose)
			history = append(history, Content{
				Role:  "user",
				Parts: []Part{{Text: "Before your final analysis, here is Playwright trace data you must incorporate:\n\n" + traceResult}},
			})
			// Don't set done — let the model regenerate with trace data.
			continue
		}

		if done {
			// Model called "done" — do one more generate to get the final text
			pendingDone = true
			continue
		}
	}

	if pendingDone {
		return c.generateFinal(ctx, history, tools, system, handler, verbose)
	}

	return nil, fmt.Errorf("agent loop exceeded %d iterations without completing", maxIterations)
}

// buildResult creates an AnalysisResult from collected text parts, applying
// defaults when the model skipped the done tool.
func buildResult(texts []string, handler *ToolHandler) *AnalysisResult {
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
func (c *Client) handleEmptyResponse(
	ctx context.Context,
	history []Content,
	tools []Tool,
	system *Content,
	handler *ToolHandler,
	step int,
	verbose bool,
	modelMs, tokens int,
	candidate *Candidate,
) ([]Content, *Candidate, int, int, error) {
	diag := describeEmptyResponse(candidate)
	if verbose {
		emit(handler, ProgressEvent{Type: "result", Step: step, MaxStep: maxIterations, ModelMs: modelMs, Tokens: tokens})
	}
	history = append(history, Content{
		Role:  "user",
		Parts: []Part{{Text: "Please provide your analysis or call a tool. Do not respond with empty content."}},
	})
	emit(handler, ProgressEvent{Type: "step", Step: step, MaxStep: maxIterations, Message: "Retrying (empty response)..."})
	t0 := time.Now()
	resp, err := c.generate(ctx, history, tools, system)
	modelMs = int(time.Since(t0).Milliseconds())
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("step %d retry: %w", step, err)
	}
	if resp.UsageMetadata != nil {
		tokens = resp.UsageMetadata.PromptTokenCount
	}
	if len(resp.Candidates) == 0 || isEmptyResponse(&resp.Candidates[0]) {
		return nil, nil, 0, 0, fmt.Errorf("step %d: model returned empty response after retry (%s)", step, diag)
	}
	return history, &resp.Candidates[0], modelMs, tokens, nil
}

// executeCalls runs each function call, emitting progress events and collecting responses.
func (c *Client) executeCalls(
	ctx context.Context,
	handler *ToolHandler,
	calls []FunctionCall,
	step int,
	verbose bool,
	modelMs, tokens int,
) ([]Part, bool, error) {
	var responseParts []Part
	done := false
	for ci, call := range calls {
		toolEvent := ProgressEvent{Type: "tool", Step: step, MaxStep: maxIterations, Tool: call.Name}
		if verbose && len(call.Args) > 0 {
			argsJSON, _ := json.Marshal(call.Args)
			toolEvent.Args = string(argsJSON)
		}
		emit(handler, toolEvent)

		t1 := time.Now()
		result, isDone, err := handler.Execute(ctx, call)
		toolMs := int(time.Since(t1).Milliseconds())
		if err != nil {
			return nil, false, fmt.Errorf("step %d, tool %s: %w", step, call.Name, err)
		}

		if isDone {
			done = true
		}

		if verbose {
			ev := ProgressEvent{
				Type:    "result",
				Step:    step,
				MaxStep: maxIterations,
				Chars:   len(result),
				ToolMs:  toolMs,
			}
			// Attach model stats to the last tool call in this step
			if ci == len(calls)-1 {
				ev.ModelMs = modelMs
				ev.Tokens = tokens
			}
			// Keep Preview for SSE backward compat
			if len(result) > 0 {
				ev.Preview = previewExcerpt(result, 200)
			}
			emit(handler, ev)
		}

		responseParts = append(responseParts, Part{
			FunctionResponse: &FunctionResponse{
				Name:     call.Name,
				Response: map[string]any{"result": result},
			},
		})
	}
	return responseParts, done, nil
}

// generateFinal performs one last model call after the done tool was signalled,
// extracting the final text analysis.
func (c *Client) generateFinal(
	ctx context.Context,
	history []Content,
	tools []Tool,
	system *Content,
	handler *ToolHandler,
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
		var texts []string
		for _, p := range resp.Candidates[0].Content.Parts {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		if len(texts) > 0 {
			return buildResult(texts, handler), nil
		}
	}
	return nil, fmt.Errorf("agent loop exceeded %d iterations without completing", maxIterations)
}

// forceTraces calls get_test_traces programmatically when the model skips it.
func (c *Client) forceTraces(ctx context.Context, handler *ToolHandler, step, maxIter int, verbose bool) string {
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
