package cluster

import (
	"regexp"
	"strings"
	"unicode"
)

// maxLogTail is the number of bytes from the end of a log to scan for errors.
// Most CI failures appear near the end of the output.
const maxLogTail = 30000

// GitHub Actions timestamp prefix: 2024-01-15T10:30:00.1234567Z
var timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s?`)

// ANSI escape codes
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// Normalization patterns
var (
	hexRe        = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	uuidRe       = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	numberRe     = regexp.MustCompile(`\b\d+\b`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// Stack trace patterns (applied to timestamp-stripped lines)
var (
	// Go: goroutine N [running]: ... file.go:42
	goTraceRe = regexp.MustCompile(`(?m)^\t(.+\.go:\d+)`)
	// JS/TS: at Function (file.ts:42:15) or (file.ts:42:15)
	jsTraceRe = regexp.MustCompile(`\(([^)]+\.[jt]sx?:\d+):\d+\)`)
	// Python: File "file.py", line 42
	pyTraceRe = regexp.MustCompile(`File "([^"]+)", line (\d+)`)
	// Java: at com.example.Class.method(File.java:42)
	javaTraceRe = regexp.MustCompile(`at .+\(([^)]+\.java:\d+)\)`)
	// Rust: thread 'main' panicked at file.rs:42:5
	rustTraceRe = regexp.MustCompile(`panicked at ([^:\s]+:\d+)`)
)

// Error message patterns
var (
	errorMsgRe      = regexp.MustCompile(`(?im)^(?:Error|ERROR|error|FAIL|FAILED|AssertionError|AssertError|TimeoutError|TypeError|ReferenceError):\s*(.+)`)
	connectionRe    = regexp.MustCompile(`(?i)(connection refused|ECONNREFUSED|ERR_CONNECTION_REFUSED|ECONNRESET)`)
	exitCodeRe      = regexp.MustCompile(`(?i)(?:exit code|exited with|exit status)\s+(\d+)`)
	playwrightErrRe = regexp.MustCompile(`(?i)expect\(.+\)\..+`)
	timeoutRe       = regexp.MustCompile(`(?i)(timed?\s*out|timeout\s+\d+\s*m?s?\s+exceeded)`)
)

// Fallback: last line containing error keywords
var errorKeywordRe = regexp.MustCompile(`(?i)(error|fail|panic|timeout|refused|abort|crash|killed|segfault)`)

// ExtractSignature parses a raw job log and returns an ErrorSignature.
// Returns a zero-value signature if no error pattern is found.
func ExtractSignature(logText string) ErrorSignature {
	if logText == "" {
		return ErrorSignature{}
	}

	// Focus on the tail of the log (where errors appear)
	text := logText
	if len(text) > maxLogTail {
		text = text[len(text)-maxLogTail:]
	}

	// Strip GitHub Actions timestamps from every line
	text = stripTimestamps(text)

	// Try extractors in order of specificity
	if sig := tryExitCode(text); sig.Category != "" {
		return sig
	}
	if sig := tryStackTrace(text); sig.Category != "" {
		return sig
	}
	if sig := tryErrorMessage(text); sig.Category != "" {
		return sig
	}
	return tryFallback(text)
}

// stripTimestamps removes GitHub Actions timestamp prefixes from every line.
func stripTimestamps(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = timestampRe.ReplaceAllString(line, "")
	}
	return strings.Join(lines, "\n")
}

func tryExitCode(text string) ErrorSignature {
	m := exitCodeRe.FindStringSubmatch(text)
	if m == nil {
		return ErrorSignature{}
	}
	raw := m[0]
	norm := normalize("exit code " + m[1])
	return ErrorSignature{
		Category:   "exit_code",
		Normalized: norm,
		RawExcerpt: raw,
		Tokens:     tokenize(norm),
	}
}

func tryStackTrace(text string) ErrorSignature {
	type pattern struct {
		re      *regexp.Regexp
		useLast bool // true = use last match (user code frame); false = first match (crash site)
	}

	patterns := []pattern{
		{goTraceRe, false},   // Go: first frame = crash site
		{jsTraceRe, true},    // JS: last frame = user code (skip node_modules)
		{pyTraceRe, true},    // Python: last frame = where error raised
		{javaTraceRe, false}, // Java: first frame = exception origin
		{rustTraceRe, false}, // Rust: panicked at = crash site
	}

	for _, p := range patterns {
		matches := p.re.FindAllStringSubmatch(text, -1)
		if len(matches) > 0 {
			idx := 0
			if p.useLast {
				idx = len(matches) - 1
			}
			m := matches[idx]
			raw := m[0]
			location := m[1]
			if len(m) > 2 {
				// Python: File + line combined
				location = m[1] + ":" + m[2]
			}
			// Strip directory, keep filename:line
			parts := strings.Split(location, "/")
			short := parts[len(parts)-1]

			norm := normalize(short)
			return ErrorSignature{
				Category:   "stack_trace",
				Normalized: norm,
				RawExcerpt: raw,
				Tokens:     tokenize(norm),
			}
		}
	}
	return ErrorSignature{}
}

func tryErrorMessage(text string) ErrorSignature {
	// Check specific patterns first
	if m := connectionRe.FindString(text); m != "" {
		norm := normalize(m)
		return ErrorSignature{
			Category:   "error_message",
			Normalized: norm,
			RawExcerpt: m,
			Tokens:     tokenize(norm),
		}
	}
	if m := timeoutRe.FindString(text); m != "" {
		norm := normalize(m)
		return ErrorSignature{
			Category:   "error_message",
			Normalized: norm,
			RawExcerpt: m,
			Tokens:     tokenize(norm),
		}
	}
	if m := playwrightErrRe.FindString(text); m != "" {
		norm := normalize(m)
		return ErrorSignature{
			Category:   "error_message",
			Normalized: norm,
			RawExcerpt: m,
			Tokens:     tokenize(norm),
		}
	}

	// Generic error message
	if m := errorMsgRe.FindStringSubmatch(text); m != nil {
		raw := m[0]
		msg := m[1]
		norm := normalize(msg)
		return ErrorSignature{
			Category:   "error_message",
			Normalized: norm,
			RawExcerpt: raw,
			Tokens:     tokenize(norm),
		}
	}

	return ErrorSignature{}
}

func tryFallback(text string) ErrorSignature {
	lines := strings.Split(text, "\n")
	// Scan from bottom — errors are at the end
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if errorKeywordRe.MatchString(line) {
			norm := normalize(line)
			return ErrorSignature{
				Category:   "fallback",
				Normalized: norm,
				RawExcerpt: line,
				Tokens:     tokenize(norm),
			}
		}
	}
	return ErrorSignature{}
}

// normalize strips noise from an error string for comparison.
func normalize(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	s = hexRe.ReplaceAllString(s, "<hex>")
	s = uuidRe.ReplaceAllString(s, "<uuid>")
	s = numberRe.ReplaceAllString(s, "<n>")
	s = whitespaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	return s
}

// tokenize splits a normalized string into tokens for Jaccard similarity.
// Filters out tokens shorter than 3 characters.
func tokenize(s string) []string {
	split := func(c rune) bool {
		return unicode.IsSpace(c) || unicode.IsPunct(c)
	}
	raw := strings.FieldsFunc(s, split)
	var tokens []string
	for _, t := range raw {
		if len(t) >= 3 {
			tokens = append(tokens, t)
		}
	}
	return tokens
}
