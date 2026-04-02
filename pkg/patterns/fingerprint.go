package patterns

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// Fingerprint represents the key signals extracted from an RCA for pattern matching.
type Fingerprint struct {
	FailureType string
	FilePattern string   // glob pattern derived from file path (e.g. "*.spec.ts")
	ErrorTokens []string // normalized, tokenized root cause
}

// ComputeFingerprint extracts a matchable fingerprint from an RCA.
func ComputeFingerprint(rca *llm.RootCauseAnalysis) Fingerprint {
	fp := Fingerprint{
		FailureType: strings.ToLower(rca.FailureType),
	}

	if rca.Location != nil && rca.Location.FilePath != "" {
		ext := filepath.Ext(rca.Location.FilePath)
		if ext != "" {
			fp.FilePattern = "*" + ext
		}
	}

	fp.ErrorTokens = tokenize(normalize(rca.RootCause))
	return fp
}

// normalize strips noise from text for comparison.
func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// tokenize splits a normalized string into tokens (3+ characters).
func tokenize(s string) []string {
	words := strings.Fields(s)
	var tokens []string
	for _, w := range words {
		if len(w) >= 3 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}
