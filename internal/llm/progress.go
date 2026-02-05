package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// ProgressEvent represents a single progress update during analysis.
type ProgressEvent struct {
	Type     string          `json:"type"`               // "step", "tool", "result", "info", "done", "error"
	Step     int             `json:"step,omitempty"`     // current iteration
	MaxStep  int             `json:"max,omitempty"`      // max iterations
	Message  string          `json:"message,omitempty"`  // human-readable message
	Tool     string          `json:"tool,omitempty"`     // tool name
	Args     string          `json:"args,omitempty"`     // tool arguments (JSON)
	Preview  string          `json:"preview,omitempty"`  // truncated result preview (SSE backward compat)
	Analysis *AnalysisResult `json:"analysis,omitempty"` // final analysis (for "done" type)
	ModelMs  int             `json:"model_ms,omitempty"` // model call duration in ms
	Tokens   int             `json:"tokens,omitempty"`   // prompt token count
	Chars    int             `json:"chars,omitempty"`    // tool result size in characters
	ToolMs   int             `json:"tool_ms,omitempty"`  // tool execution duration in ms
}

// ProgressEmitter receives progress events during analysis.
type ProgressEmitter interface {
	Emit(event ProgressEvent)
}

const alignWidth = 56

// TextEmitter formats progress events as colored terminal output with spinners.
type TextEmitter struct {
	w   io.Writer
	sp  *spinner.Spinner
	tty bool
}

// NewTextEmitter creates a TextEmitter that writes to w.
// It detects TTY capability for color and spinner support,
// and configures the global color.NoColor based on w.
func NewTextEmitter(w io.Writer) *TextEmitter {
	tty := false
	if f, ok := w.(*os.File); ok {
		tty = isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	color.NoColor = !tty || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
	return &TextEmitter{w: w, tty: tty}
}

// Close stops any running spinner. Call before printing final results.
func (e *TextEmitter) Close() {
	e.stopSpinner()
}

func (e *TextEmitter) stopSpinner() {
	if e.sp != nil {
		e.sp.Stop()
		e.sp = nil
	}
}

func (e *TextEmitter) startSpinner(msg string) {
	e.stopSpinner()
	if !e.tty {
		fmt.Fprintf(e.w, "  %s\n", msg)
		return
	}
	e.sp = spinner.New(spinner.CharSets[14], 80*time.Millisecond, spinner.WithWriter(e.w))
	e.sp.Prefix = "  "
	e.sp.Suffix = " " + msg
	e.sp.Start()
}

// Emit writes a formatted progress event.
func (e *TextEmitter) Emit(ev ProgressEvent) {
	dim := color.New(color.FgHiBlack)
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan)

	switch ev.Type {
	case "step":
		e.startSpinner(ev.Message)

	case "tool":
		e.stopSpinner()

		check := green.Sprint("✓")
		toolName := cyan.Sprint(ev.Tool)
		counter := dim.Sprintf("%d/%d", ev.Step, ev.MaxStep)

		argsStr := ""
		argsVisible := ""
		if ev.Args != "" && ev.Tool != "done" {
			if h := humanizeArgs(ev.Args); h != "" {
				argsVisible = " " + h
				argsStr = " " + dim.Sprint(h)
			}
		}

		// Right-align counter: "  ✓ " (4 visible chars) + tool + args
		visibleLeft := 4 + len(ev.Tool) + len(argsVisible)
		counterText := fmt.Sprintf("%d/%d", ev.Step, ev.MaxStep)
		padding := alignWidth - visibleLeft - len(counterText)
		if padding < 1 {
			padding = 1
		}

		fmt.Fprintf(e.w, "  %s %s%s%s%s\n", check, toolName, argsStr, strings.Repeat(" ", padding), counter)

	case "result":
		_, _ = dim.Fprintf(e.w, "    ↳ %s\n", formatStats(ev))

	case "info":
		e.stopSpinner()
		fmt.Fprintf(e.w, "  %s\n", ev.Message)

	case "error":
		red := color.New(color.FgRed)
		_, _ = red.Fprintf(e.w, "Error: %s\n", ev.Message)
	}
}

// humanizeArgs converts JSON args to a readable format like "(key: value, key2: value2)".
func humanizeArgs(argsJSON string) string {
	args := map[string]any{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return argsJSON
	}
	if len(args) == 0 {
		return ""
	}

	shortKeys := map[string]string{
		"artifact_name":                   "artifact",
		"missing_information_sensitivity": "sensitivity",
	}

	var parts []string
	for k, v := range args {
		display := k
		if short, ok := shortKeys[k]; ok {
			display = short
		}
		switch val := v.(type) {
		case float64:
			if val == float64(int64(val)) {
				parts = append(parts, fmt.Sprintf("%s: %d", display, int64(val)))
			} else {
				parts = append(parts, fmt.Sprintf("%s: %g", display, val))
			}
		default:
			parts = append(parts, fmt.Sprintf("%s: %v", display, v))
		}
	}

	return "(" + strings.Join(parts, ", ") + ")"
}

// formatStats builds a compact stats string from a result event.
// Example: "model 3.2s, 12,847 tok · result 82,431 chars"
func formatStats(ev ProgressEvent) string {
	var parts []string

	if ev.ModelMs > 0 || ev.Tokens > 0 {
		modelPart := "model"
		if ev.ModelMs > 0 {
			modelPart += " " + formatDuration(ev.ModelMs)
		}
		if ev.Tokens > 0 {
			modelPart += ", " + formatNumber(ev.Tokens) + " tok"
		}
		parts = append(parts, modelPart)
	}

	if ev.Chars > 0 {
		resultPart := "result " + formatNumber(ev.Chars) + " chars"
		parts = append(parts, resultPart)
	}

	if len(parts) == 0 {
		return "ok"
	}
	return strings.Join(parts, " · ")
}

// formatDuration formats milliseconds as a human-readable duration.
func formatDuration(ms int) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// formatNumber formats an integer with thousands separators.
func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
