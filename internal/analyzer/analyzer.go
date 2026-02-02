package analyzer

import (
	"context"
	"fmt"

	"github.com/kamilpajak/heisenberg/pkg/models"
)

// Provider represents an LLM provider
type Provider string

const (
	ProviderGoogle    Provider = "google"
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

// Analyzer performs AI-powered analysis of test failures
type Analyzer struct {
	provider Provider
	model    string
	apiKey   string
}

// New creates a new Analyzer
func New(provider Provider, model, apiKey string) *Analyzer {
	return &Analyzer{
		provider: provider,
		model:    model,
		apiKey:   apiKey,
	}
}

// Analyze performs AI analysis on a test report
func (a *Analyzer) Analyze(ctx context.Context, report *models.Report) (*models.AnalysisResult, error) {
	if !report.HasFailures() {
		return nil, fmt.Errorf("no failures to analyze")
	}

	prompt := a.buildPrompt(report)

	// TODO: Implement actual LLM calls based on provider
	_ = prompt

	return &models.AnalysisResult{
		Diagnosis: models.Diagnosis{
			RootCause:    "Analysis not yet implemented",
			Evidence:     []string{},
			SuggestedFix: "Implement LLM integration",
			Confidence:   models.ConfidenceLow,
		},
		Provider: string(a.provider),
		Model:    a.model,
	}, nil
}

func (a *Analyzer) buildPrompt(report *models.Report) string {
	failed := report.FailedTestCases()

	prompt := fmt.Sprintf(`Analyze the following test failures and provide:
1. Root cause analysis
2. Evidence supporting your diagnosis
3. Suggested fix

Test Summary:
- Total: %d
- Passed: %d
- Failed: %d
- Skipped: %d

Failed Tests:
`, report.TotalTests, report.PassedTests, report.FailedTests, report.SkippedTests)

	for _, tc := range failed {
		prompt += fmt.Sprintf("\n## %s\n", tc.Name)
		if tc.FilePath != "" {
			prompt += fmt.Sprintf("File: %s:%d\n", tc.FilePath, tc.LineNumber)
		}
		if tc.ErrorMessage != "" {
			prompt += fmt.Sprintf("Error: %s\n", tc.ErrorMessage)
		}
		if tc.ErrorStack != "" {
			prompt += fmt.Sprintf("Stack:\n%s\n", tc.ErrorStack)
		}
	}

	return prompt
}
