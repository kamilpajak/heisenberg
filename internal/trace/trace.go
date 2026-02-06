package trace

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

const maxActions = 10

// TestTrace holds extracted trace data for a single test directory.
type TestTrace struct {
	TestDir        string
	ErrorContext   string
	Actions        []Action
	ConsoleErrors  []string
	FailedRequests []Request
}

// Action represents a browser action from the trace.
type Action struct {
	Class  string // Frame, Page, Locator
	Method string // click, fill, goto, waitForSelector
	Params string // truncated JSON params
}

// Request represents a failed HTTP request.
type Request struct {
	Method string
	URL    string
	Status int
}

// dirFiles groups trace-related files found in a test directory.
type dirFiles struct {
	traceZip     *zip.File
	errorContext *zip.File
}

// ParseArtifact extracts trace data from a GitHub artifact ZIP (which contains
// nested test directories, each potentially containing a trace.zip).
func ParseArtifact(zipData []byte) ([]TestTrace, error) {
	outer, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("open outer zip: %w", err)
	}

	dirs := indexDirFiles(outer.File)

	var traces []TestTrace
	for dir, files := range dirs {
		if t, ok := buildTrace(dir, files); ok {
			traces = append(traces, t)
		}
	}

	if len(traces) == 0 {
		return nil, fmt.Errorf("no test trace directories found in artifact")
	}

	return traces, nil
}

func indexDirFiles(files []*zip.File) map[string]*dirFiles {
	dirs := make(map[string]*dirFiles)

	for _, f := range files {
		if f.FileInfo().IsDir() {
			continue
		}
		dir := path.Dir(f.Name)
		if dir == "." {
			dir = ""
		}
		base := path.Base(f.Name)

		if _, ok := dirs[dir]; !ok {
			dirs[dir] = &dirFiles{}
		}

		switch base {
		case "trace.zip":
			dirs[dir].traceZip = f
		case "error-context.md":
			dirs[dir].errorContext = f
		}
	}

	return dirs
}

func buildTrace(dir string, files *dirFiles) (TestTrace, bool) {
	if files.traceZip == nil && files.errorContext == nil {
		return TestTrace{}, false
	}

	t := TestTrace{TestDir: dir}

	if files.errorContext != nil {
		data, err := readZipFile(files.errorContext)
		if err == nil {
			t.ErrorContext = string(data)
		}
	}

	if files.traceZip != nil {
		if parseTraceZip(files.traceZip, &t) != nil {
			return TestTrace{}, false
		}
	}

	return t, true
}

func parseTraceZip(f *zip.File, t *TestTrace) error {
	data, err := readZipFile(f)
	if err != nil {
		return err
	}

	inner, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open inner trace.zip: %w", err)
	}

	for _, entry := range inner.File {
		base := path.Base(entry.Name)
		switch {
		case strings.HasSuffix(base, "-trace.trace") || base == "0-trace.trace":
			if parseBrowserTrace(entry, t) != nil {
				continue
			}
		case strings.HasSuffix(base, "-trace.network") || base == "0-trace.network":
			if parseNetworkTrace(entry, t) != nil {
				continue
			}
		}
	}

	return nil
}

// traceEvent represents a single line from a .trace NDJSON file.
type traceEvent struct {
	Type        string         `json:"type"`
	Class       string         `json:"class"`
	Method      string         `json:"method"`
	Params      map[string]any `json:"params"`
	MessageType string         `json:"messageType"`
	Text        string         `json:"text"`
}

func parseBrowserTrace(f *zip.File, t *TestTrace) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	var actions []Action

	for scanner.Scan() {
		var ev traceEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}

		switch ev.Type {
		case "before":
			switch ev.Class {
			case "Frame", "Page", "Locator":
				actions = append(actions, Action{
					Class:  ev.Class,
					Method: ev.Method,
					Params: truncateParams(ev.Params),
				})
			}
		case "console":
			if ev.MessageType == "error" && ev.Text != "" {
				t.ConsoleErrors = append(t.ConsoleErrors, ev.Text)
			}
		}
	}

	// Keep last N actions (most relevant near failure).
	if len(actions) > maxActions {
		actions = actions[len(actions)-maxActions:]
	}
	t.Actions = actions

	return scanner.Err()
}

type networkRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type networkResponse struct {
	Status int `json:"status"`
}

type networkSnapshot struct {
	Request  networkRequest  `json:"request"`
	Response networkResponse `json:"response"`
}

// networkEvent represents a single line from a .network NDJSON file.
type networkEvent struct {
	Type     string          `json:"type"`
	Snapshot networkSnapshot `json:"snapshot"`
}

func parseNetworkTrace(f *zip.File, t *TestTrace) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	for scanner.Scan() {
		var ev networkEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}

		if ev.Type == "resource-snapshot" && ev.Snapshot.Response.Status >= 400 {
			t.FailedRequests = append(t.FailedRequests, Request{
				Method: ev.Snapshot.Request.Method,
				URL:    ev.Snapshot.Request.URL,
				Status: ev.Snapshot.Response.Status,
			})
		}
	}

	return scanner.Err()
}

func truncateParams(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	b, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	s := string(b)
	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// FormatSummary renders all traces into a single readable text block.
func FormatSummary(traces []TestTrace) string {
	var b strings.Builder

	for i, t := range traces {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		formatTrace(&b, t)
	}

	return b.String()
}

func formatTrace(b *strings.Builder, t TestTrace) {
	fmt.Fprintf(b, "## Test: %s\n\n", t.TestDir)
	formatActions(b, t.Actions)
	formatConsoleErrors(b, t.ConsoleErrors)
	formatFailedRequests(b, t.FailedRequests)
	formatErrorContext(b, t.ErrorContext)
}

func formatActions(b *strings.Builder, actions []Action) {
	if len(actions) == 0 {
		return
	}
	b.WriteString("### Last Browser Actions\n")
	for _, a := range actions {
		if a.Params != "" {
			fmt.Fprintf(b, "- %s.%s %s\n", a.Class, a.Method, a.Params)
		} else {
			fmt.Fprintf(b, "- %s.%s\n", a.Class, a.Method)
		}
	}
	b.WriteString("\n")
}

func formatConsoleErrors(b *strings.Builder, errors []string) {
	if len(errors) == 0 {
		return
	}
	b.WriteString("### Console Errors\n")
	for _, msg := range errors {
		fmt.Fprintf(b, "- %s\n", msg)
	}
	b.WriteString("\n")
}

func formatFailedRequests(b *strings.Builder, requests []Request) {
	if len(requests) == 0 {
		return
	}
	b.WriteString("### Failed HTTP Requests\n")
	for _, r := range requests {
		fmt.Fprintf(b, "- %s %s → %d\n", r.Method, r.URL, r.Status)
	}
	b.WriteString("\n")
}

func formatErrorContext(b *strings.Builder, errorCtx string) {
	if errorCtx == "" {
		return
	}
	b.WriteString("### Error Context (page snapshot)\n")
	b.WriteString(errorCtx)
	b.WriteString("\n")
}
