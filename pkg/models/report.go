package models

// TestStatus represents the status of a test case
type TestStatus string

const (
	StatusPassed  TestStatus = "passed"
	StatusFailed  TestStatus = "failed"
	StatusSkipped TestStatus = "skipped"
)

// TestCase represents a single test result
type TestCase struct {
	Name         string     `json:"name"`
	Status       TestStatus `json:"status"`
	DurationMS   int64      `json:"duration_ms,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	ErrorStack   string     `json:"error_stack,omitempty"`
	FilePath     string     `json:"file_path,omitempty"`
	LineNumber   int        `json:"line_number,omitempty"`
}

// TestSuite represents a collection of test cases
type TestSuite struct {
	Name     string      `json:"name"`
	Tests    []TestCase  `json:"tests"`
	Suites   []TestSuite `json:"suites,omitempty"`
	FilePath string      `json:"file_path,omitempty"`
}

// Report represents a normalized test report
type Report struct {
	Framework    string      `json:"framework"`
	TotalTests   int         `json:"total_tests"`
	PassedTests  int         `json:"passed_tests"`
	FailedTests  int         `json:"failed_tests"`
	SkippedTests int         `json:"skipped_tests"`
	DurationMS   int64       `json:"duration_ms,omitempty"`
	Suites       []TestSuite `json:"suites"`
}

// HasFailures returns true if the report contains any failures
func (r *Report) HasFailures() bool {
	return r.FailedTests > 0
}

// FailedTestCases returns all failed test cases from the report
func (r *Report) FailedTestCases() []TestCase {
	var failed []TestCase
	for _, suite := range r.Suites {
		failed = append(failed, collectFailedTests(suite)...)
	}
	return failed
}

func collectFailedTests(suite TestSuite) []TestCase {
	var failed []TestCase
	for _, test := range suite.Tests {
		if test.Status == StatusFailed {
			failed = append(failed, test)
		}
	}
	for _, nested := range suite.Suites {
		failed = append(failed, collectFailedTests(nested)...)
	}
	return failed
}
