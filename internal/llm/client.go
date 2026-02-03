package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// emit sends a progress event via the handler's emitter, if set.
func emit(h *ToolHandler, ev ProgressEvent) {
	if h != nil && h.Emitter != nil {
		h.Emitter.Emit(ev)
	}
}

const maxIterations = 10

// Client handles Gemini API calls with function calling support.
type Client struct {
	apiKey  string
	baseURL string
	model   string
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
		model:   "gemini-2.5-flash",
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

	for i := range maxIterations {
		step := i + 1
		emit(handler, ProgressEvent{Type: "step", Step: step, MaxStep: maxIterations, Message: "Calling model..."})

		resp, err := c.generate(ctx, history, tools, system)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", step, err)
		}

		if len(resp.Candidates) == 0 {
			return nil, fmt.Errorf("step %d: empty response from model", step)
		}

		modelContent := resp.Candidates[0].Content
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
			// But if traces are pending, force a trace fetch before finishing.
			if handler.HasPendingTraces() {
				traceResult := c.forceTraces(ctx, handler, step, maxIterations, verbose)
				history = append(history, Content{
					Role: "user",
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
				return nil, fmt.Errorf("step %d: model returned neither text nor function calls", step)
			}
			return &AnalysisResult{
				Text:        strings.Join(texts, "\n"),
				Category:    handler.DiagnosisCategory(),
				Confidence:  handler.DiagnosisConfidence(),
				Sensitivity: handler.DiagnosisSensitivity(),
			}, nil
		}

		// Execute each function call
		var responseParts []Part
		done := false
		for _, call := range calls {
			toolEvent := ProgressEvent{Type: "tool", Step: step, MaxStep: maxIterations, Tool: call.Name}
			if verbose && len(call.Args) > 0 {
				argsJSON, _ := json.Marshal(call.Args)
				toolEvent.Args = string(argsJSON)
			}
			emit(handler, toolEvent)

			result, isDone, err := handler.Execute(ctx, call)
			if err != nil {
				return nil, fmt.Errorf("step %d, tool %s: %w", step, call.Name, err)
			}

			if isDone {
				done = true
			}

			if verbose && len(result) > 0 {
				preview := result
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				emit(handler, ProgressEvent{Type: "result", Step: step, MaxStep: maxIterations, Preview: preview})
			}

			responseParts = append(responseParts, Part{
				FunctionResponse: &FunctionResponse{
					Name:     call.Name,
					Response: map[string]any{"result": result},
				},
			})
		}

		history = append(history, Content{Role: "user", Parts: responseParts})

		// If model called done but traces are pending, inject trace data first.
		if done && handler.HasPendingTraces() {
			traceResult := c.forceTraces(ctx, handler, step, maxIterations, verbose)
			history = append(history, Content{
				Role: "user",
				Parts: []Part{{Text: "Before your final analysis, here is Playwright trace data you must incorporate:\n\n" + traceResult}},
			})
			// Don't set done — let the model regenerate with trace data.
			continue
		}

		if done {
			// Model called "done" — do one more generate to get the final text
			emit(handler, ProgressEvent{Type: "step", Step: step, MaxStep: maxIterations, Message: "Model signalled done, generating final analysis..."})
			continue
		}
	}

	return nil, fmt.Errorf("agent loop exceeded %d iterations without completing", maxIterations)
}

// forceTraces calls get_test_traces programmatically when the model skips it.
func (c *Client) forceTraces(ctx context.Context, handler *ToolHandler, step, maxIter int, verbose bool) string {
	emit(handler, ProgressEvent{Type: "tool", Step: step, MaxStep: maxIter, Tool: "get_test_traces", Message: "Forcing get_test_traces (model skipped it)"})
	result, _, _ := handler.Execute(ctx, FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})
	if verbose && len(result) > 0 {
		preview := result
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		emit(handler, ProgressEvent{Type: "result", Step: step, MaxStep: maxIter, Preview: preview})
	}
	return result
}

func (c *Client) generate(ctx context.Context, history []Content, tools []Tool, system *Content) (*GenerateResponse, error) {
	req := GenerateRequest{
		Contents:         history,
		Tools:            tools,
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
		return nil, fmt.Errorf("Gemini API error: %s - %s", resp.Status, string(body))
	}

	var result GenerateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}
