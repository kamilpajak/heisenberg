package analyzer

import (
	"fmt"
	"strings"

	"github.com/kamilpajak/heisenberg/pkg/models"
)

const systemPrompt = `You are an expert test failure analyst. Your task is to analyze test failures and provide actionable root cause analysis.

When analyzing failures, consider:
1. Error messages and stack traces
2. Patterns across multiple failures
3. Common causes: timing issues, flaky selectors, race conditions, environment problems
4. The test code context when available

Respond in JSON format with the following structure:
{
  "root_cause": "Clear explanation of why the test failed",
  "evidence": ["Evidence point 1", "Evidence point 2"],
  "suggested_fix": "Specific actionable fix recommendation",
  "confidence": "HIGH|MEDIUM|LOW",
  "confidence_explanation": "Why you have this confidence level"
}

Be concise but thorough. Focus on actionable insights.`

// BuildPrompt creates the analysis prompt from a test report
func BuildPrompt(report *models.Report) string {
	var sb strings.Builder

	// Summary
	sb.WriteString(fmt.Sprintf(`## Test Summary
- Framework: %s
- Total: %d tests
- Passed: %d
- Failed: %d
- Skipped: %d

`, report.Framework, report.TotalTests, report.PassedTests, report.FailedTests, report.SkippedTests))

	// Failed tests
	failed := report.FailedTestCases()
	if len(failed) == 0 {
		sb.WriteString("No failed tests found.\n")
		return sb.String()
	}

	sb.WriteString("## Failed Tests\n\n")
	for i, tc := range failed {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("\n... and %d more failures\n", len(failed)-10))
			break
		}

		sb.WriteString(fmt.Sprintf("### %d. %s\n", i+1, tc.Name))

		if tc.FilePath != "" {
			location := tc.FilePath
			if tc.LineNumber > 0 {
				location = fmt.Sprintf("%s:%d", tc.FilePath, tc.LineNumber)
			}
			sb.WriteString(fmt.Sprintf("**Location:** `%s`\n", location))
		}

		if tc.DurationMS > 0 {
			sb.WriteString(fmt.Sprintf("**Duration:** %dms\n", tc.DurationMS))
		}

		if tc.ErrorMessage != "" {
			// Truncate long error messages
			errMsg := tc.ErrorMessage
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
			sb.WriteString(fmt.Sprintf("**Error:**\n```\n%s\n```\n", errMsg))
		}

		if tc.ErrorStack != "" {
			// Truncate long stack traces
			stack := tc.ErrorStack
			if len(stack) > 1000 {
				stack = stack[:1000] + "\n..."
			}
			sb.WriteString(fmt.Sprintf("**Stack Trace:**\n```\n%s\n```\n", stack))
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// GetSystemPrompt returns the system prompt for analysis
func GetSystemPrompt() string {
	return systemPrompt
}
