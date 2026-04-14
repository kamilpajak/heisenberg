package logclean

import (
	"regexp"
	"strings"
)

// Line importance weights for budget-constrained selection.
// Higher weight = more likely to be preserved under tight budget.
const (
	weightNoise    = 0
	weightDefault  = 1
	weightWarning  = 3
	weightError    = 5
	weightCritical = 10
)

var criticalPrefixes = []string{
	"panic:",
	"Traceback",
	"##[error]",
	"AssertionError",
}

var criticalRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|\s)(?:FAIL|FAILED|Exception):`),
	// Bun/vitest/jest lowercase marker: "(fail) TestName" — no colon.
	regexp.MustCompile(`^\(fail\)\s`),
}

var errorRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|\s)(?:ERROR|FATAL)(?::|\s|$)`),
	regexp.MustCompile(`(?i)(?:^|\s)error:`),
	// Stack frames (Go, JS/TS, Python, Java, Rust)
	regexp.MustCompile(`^\t.+\.go:\d+`),
	regexp.MustCompile(`\([^)]+\.[jt]sx?:\d+(?::\d+)?\)`),
	regexp.MustCompile(`File ".+", line \d+`),
	regexp.MustCompile(`at .+\(.+\.java:\d+\)`),
	regexp.MustCompile(`panicked at`),
}

var warningContains = []string{
	"timed out",
	"deadlock",
	"OOMKilled",
	"connection refused",
	"connection reset",
}

// warningRegexes catch assertion-detail patterns that carry critical
// diagnostic context but don't fit the contains-list style (need anchors or
// captures). Elevating these above default-signal keeps them from losing
// budget pressure to bulk (pass)/build filler.
var warningRegexes = []*regexp.Regexp{
	regexp.MustCompile(`^(Expected|Received):`),      // jest/vitest/jasmine/pytest assertion diff
	regexp.MustCompile(`(?i)expected\s.+\sbut\sgot`), // "expected X but got Y"
	regexp.MustCompile(`^\s*assert\s[A-Za-z_(]`),     // `assert foo == bar`
}

var warningContainsLower = func() []string {
	out := make([]string, len(warningContains))
	for i, s := range warningContains {
		out[i] = strings.ToLower(s)
	}
	return out
}()

// classifyWeight assigns an importance weight to a log line.
// The line is expected to already have timestamp and leading whitespace stripped
// and to have been confirmed as signal (classifyLine != lineNoise) by the caller.
func classifyWeight(line string) int {
	for _, p := range criticalPrefixes {
		if strings.HasPrefix(line, p) {
			return weightCritical
		}
	}
	for _, re := range criticalRegexes {
		if re.MatchString(line) {
			return weightCritical
		}
	}

	for _, re := range errorRegexes {
		if re.MatchString(line) {
			return weightError
		}
	}

	lower := strings.ToLower(line)
	for _, s := range warningContainsLower {
		if strings.Contains(lower, s) {
			return weightWarning
		}
	}
	for _, re := range warningRegexes {
		if re.MatchString(line) {
			return weightWarning
		}
	}

	return weightDefault
}
