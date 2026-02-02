package parser

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kamilpajak/heisenberg/pkg/models"
)

// PlaywrightParser parses Playwright JSON reports
type PlaywrightParser struct{}

// playwrightReport represents the raw Playwright JSON structure
type playwrightReport struct {
	Suites []playwrightSuite `json:"suites"`
	Stats  playwrightStats   `json:"stats"`
}

type playwrightStats struct {
	Expected   int `json:"expected"`
	Unexpected int `json:"unexpected"`
	Flaky      int `json:"flaky"`
	Skipped    int `json:"skipped"`
	// Blob format uses these
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Total  int `json:"total"`
}

type playwrightSuite struct {
	Title  string            `json:"title"`
	File   string            `json:"file"`
	Specs  []playwrightSpec  `json:"specs"`
	Suites []playwrightSuite `json:"suites"`
}

type playwrightSpec struct {
	Title string           `json:"title"`
	File  string           `json:"file"`
	Line  int              `json:"line"`
	OK    *bool            `json:"ok"`
	Tests []playwrightTest `json:"tests"`
}

type playwrightTest struct {
	Status  string             `json:"status"`
	Results []playwrightResult `json:"results"`
}

type playwrightResult struct {
	Status   string            `json:"status"`
	Duration int64             `json:"duration"`
	Errors   []playwrightError `json:"errors"`
}

type playwrightError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

// Parse reads and parses a Playwright JSON report file
func (p *PlaywrightParser) Parse(path string) (*models.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read report: %w", err)
	}

	var raw playwrightReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse report: %w", err)
	}

	return p.normalize(raw), nil
}

// ParseBytes parses Playwright JSON from raw bytes
func (p *PlaywrightParser) ParseBytes(data []byte) (*models.Report, error) {
	var raw playwrightReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse report: %w", err)
	}

	return p.normalize(raw), nil
}

func (p *PlaywrightParser) normalize(raw playwrightReport) *models.Report {
	// Support both JSON reporter (expected/unexpected) and blob format (passed/failed)
	passed := raw.Stats.Passed
	if passed == 0 {
		passed = raw.Stats.Expected
	}

	failed := raw.Stats.Failed
	if failed == 0 {
		failed = raw.Stats.Unexpected + raw.Stats.Flaky
	}

	total := raw.Stats.Total
	if total == 0 {
		total = passed + failed + raw.Stats.Skipped
	}

	report := &models.Report{
		Framework:    "playwright",
		TotalTests:   total,
		PassedTests:  passed,
		FailedTests:  failed,
		SkippedTests: raw.Stats.Skipped,
		Suites:       make([]models.TestSuite, 0, len(raw.Suites)),
	}

	for _, suite := range raw.Suites {
		report.Suites = append(report.Suites, p.normalizeSuite(suite))
	}

	return report
}

func (p *PlaywrightParser) normalizeSuite(raw playwrightSuite) models.TestSuite {
	suite := models.TestSuite{
		Name:     raw.Title,
		FilePath: raw.File,
		Tests:    make([]models.TestCase, 0),
		Suites:   make([]models.TestSuite, 0),
	}

	for _, spec := range raw.Specs {
		if test := p.normalizeSpec(spec); test != nil {
			suite.Tests = append(suite.Tests, *test)
		}
	}

	for _, nested := range raw.Suites {
		suite.Suites = append(suite.Suites, p.normalizeSuite(nested))
	}

	return suite
}

func (p *PlaywrightParser) normalizeSpec(spec playwrightSpec) *models.TestCase {
	if len(spec.Tests) == 0 {
		return nil
	}

	test := spec.Tests[0]
	if len(test.Results) == 0 {
		return nil
	}

	result := test.Results[len(test.Results)-1]

	tc := &models.TestCase{
		Name:       spec.Title,
		FilePath:   spec.File,
		LineNumber: spec.Line,
		DurationMS: result.Duration,
	}

	// Determine status
	switch result.Status {
	case "passed":
		tc.Status = models.StatusPassed
	case "skipped":
		tc.Status = models.StatusSkipped
	default:
		tc.Status = models.StatusFailed
	}

	// Extract error info
	if len(result.Errors) > 0 {
		tc.ErrorMessage = result.Errors[0].Message
		tc.ErrorStack = result.Errors[0].Stack
	}

	return tc
}
