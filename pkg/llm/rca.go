package llm

import (
	"fmt"
	"strings"
)

// Failure type constants for categorizing test failures.
const (
	FailureTypeTimeout   = "timeout"
	FailureTypeAssertion = "assertion"
	FailureTypeNetwork   = "network"
	FailureTypeInfra     = "infra"
	FailureTypeFlake     = "flake"
)

// Evidence type constants for categorizing supporting data.
const (
	EvidenceScreenshot = "screenshot"
	EvidenceTrace      = "trace"
	EvidenceLog        = "log"
	EvidenceNetwork    = "network"
	EvidenceCode       = "code"
)

// RootCauseAnalysis holds structured diagnosis information.
type RootCauseAnalysis struct {
	Title       string        `json:"title"`              // Short summary, e.g. "Timeout waiting for Submit Button"
	FailureType string        `json:"failure_type"`       // timeout, assertion, network, infra, flake
	Location    *CodeLocation `json:"location,omitempty"` // Where the failure occurred
	Symptom     string        `json:"symptom"`            // What failed
	RootCause   string        `json:"root_cause"`         // Why it failed
	Evidence    []Evidence    `json:"evidence"`           // Supporting data points
	Remediation string        `json:"remediation"`        // How to fix it
}

// CodeLocation identifies a specific location in source code.
type CodeLocation struct {
	FilePath     string `json:"file_path"`               // e.g. "tests/checkout.spec.ts"
	LineNumber   int    `json:"line_number,omitempty"`   // e.g. 45
	FunctionName string `json:"function_name,omitempty"` // e.g. "test('user can checkout')"
}

// Evidence represents a piece of supporting data for the diagnosis.
type Evidence struct {
	Type    string `json:"type"`    // screenshot, trace, log, network, code
	Content string `json:"content"` // Description of the evidence
}

// ParseRCAFromArgs extracts RootCauseAnalysis from done tool arguments.
func ParseRCAFromArgs(args map[string]any) *RootCauseAnalysis {
	if args == nil {
		return &RootCauseAnalysis{}
	}

	rca := &RootCauseAnalysis{
		Title:       stringArg(args, "title"),
		FailureType: stringArg(args, "failure_type"),
		Symptom:     stringArg(args, "symptom"),
		RootCause:   stringArg(args, "root_cause"),
		Remediation: stringArg(args, "remediation"),
	}

	// Parse location if file_path is present
	if filePath := stringArg(args, "file_path"); filePath != "" {
		rca.Location = &CodeLocation{
			FilePath:     filePath,
			LineNumber:   intArgValue(args, "line_number"),
			FunctionName: stringArg(args, "function_name"),
		}
	}

	// Parse evidence array
	if evidenceRaw, ok := args["evidence"].([]any); ok {
		for _, ev := range evidenceRaw {
			if evMap, ok := ev.(map[string]any); ok {
				rca.Evidence = append(rca.Evidence, Evidence{
					Type:    stringArg(evMap, "type"),
					Content: stringArg(evMap, "content"),
				})
			}
		}
	}

	return rca
}

// FormatForCLI returns a formatted string for CLI output.
func (rca *RootCauseAnalysis) FormatForCLI() string {
	var b strings.Builder

	// Header with failure type and location
	header := fmt.Sprintf("%s ERROR", strings.ToUpper(rca.FailureType))
	if rca.Location != nil {
		loc := rca.Location.FilePath
		if rca.Location.LineNumber > 0 {
			loc = fmt.Sprintf("%s:%d", rca.Location.FilePath, rca.Location.LineNumber)
		}
		header = fmt.Sprintf("%s in %s", header, loc)
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	// Root cause section
	b.WriteString("ROOT CAUSE\n")
	b.WriteString(rca.RootCause)
	b.WriteString("\n\n")

	// Evidence section (if any)
	if len(rca.Evidence) > 0 {
		b.WriteString("EVIDENCE\n")
		for _, ev := range rca.Evidence {
			icon := evidenceIcon(ev.Type)
			b.WriteString(fmt.Sprintf("%s %s\n", icon, ev.Content))
		}
		b.WriteString("\n")
	}

	// Fix section
	b.WriteString("FIX\n")
	b.WriteString(rca.Remediation)

	return b.String()
}

// evidenceIcon returns an emoji icon for the evidence type.
func evidenceIcon(t string) string {
	switch t {
	case EvidenceScreenshot:
		return "[Screenshot]"
	case EvidenceTrace:
		return "[Trace]"
	case EvidenceLog:
		return "[Log]"
	case EvidenceNetwork:
		return "[Network]"
	case EvidenceCode:
		return "[Code]"
	default:
		return "[Evidence]"
	}
}

// stringArg extracts a string from args, returning empty string if not found.
func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// intArgValue extracts an int from args, returning 0 if not found.
func intArgValue(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
