package parser

import (
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/models"
)

func TestPlaywrightParser_ParseBytes(t *testing.T) {
	// Sample Playwright JSON report
	jsonData := []byte(`{
		"suites": [{
			"title": "Test Suite",
			"file": "test.spec.ts",
			"specs": [{
				"title": "should pass",
				"file": "test.spec.ts",
				"line": 10,
				"ok": true,
				"tests": [{
					"status": "expected",
					"results": [{
						"status": "passed",
						"duration": 100
					}]
				}]
			}, {
				"title": "should fail",
				"file": "test.spec.ts",
				"line": 20,
				"ok": false,
				"tests": [{
					"status": "unexpected",
					"results": [{
						"status": "failed",
						"duration": 200,
						"errors": [{
							"message": "Expected true to be false",
							"stack": "Error: Expected true to be false\n    at test.spec.ts:25"
						}]
					}]
				}]
			}]
		}],
		"stats": {
			"expected": 1,
			"unexpected": 1,
			"flaky": 0,
			"skipped": 0
		}
	}`)

	p := &PlaywrightParser{}
	report, err := p.ParseBytes(jsonData)

	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	// Check stats
	if report.Framework != "playwright" {
		t.Errorf("Expected framework 'playwright', got '%s'", report.Framework)
	}

	if report.TotalTests != 2 {
		t.Errorf("Expected 2 total tests, got %d", report.TotalTests)
	}

	if report.PassedTests != 1 {
		t.Errorf("Expected 1 passed test, got %d", report.PassedTests)
	}

	if report.FailedTests != 1 {
		t.Errorf("Expected 1 failed test, got %d", report.FailedTests)
	}

	// Check HasFailures
	if !report.HasFailures() {
		t.Error("Expected HasFailures() to return true")
	}

	// Check failed tests extraction
	failed := report.FailedTestCases()
	if len(failed) != 1 {
		t.Fatalf("Expected 1 failed test case, got %d", len(failed))
	}

	if failed[0].Name != "should fail" {
		t.Errorf("Expected failed test name 'should fail', got '%s'", failed[0].Name)
	}

	if failed[0].Status != models.StatusFailed {
		t.Errorf("Expected status 'failed', got '%s'", failed[0].Status)
	}

	if failed[0].ErrorMessage != "Expected true to be false" {
		t.Errorf("Unexpected error message: %s", failed[0].ErrorMessage)
	}
}

func TestPlaywrightParser_ParseBytes_BlobFormat(t *testing.T) {
	// Blob format uses passed/failed instead of expected/unexpected
	jsonData := []byte(`{
		"suites": [{
			"title": "Blob Report Tests",
			"specs": [{
				"title": "Unknown",
				"tests": [{
					"status": "unexpected",
					"results": [{
						"status": "timedOut",
						"duration": 30000,
						"errors": [{
							"message": "Test timeout of 30000ms exceeded"
						}]
					}]
				}]
			}]
		}],
		"stats": {
			"passed": 0,
			"failed": 1,
			"skipped": 0,
			"total": 1
		}
	}`)

	p := &PlaywrightParser{}
	report, err := p.ParseBytes(jsonData)

	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	if report.FailedTests != 1 {
		t.Errorf("Expected 1 failed test, got %d", report.FailedTests)
	}

	if report.TotalTests != 1 {
		t.Errorf("Expected 1 total test, got %d", report.TotalTests)
	}
}

func TestPlaywrightParser_ParseBytes_NoFailures(t *testing.T) {
	jsonData := []byte(`{
		"suites": [],
		"stats": {
			"expected": 5,
			"unexpected": 0,
			"flaky": 0,
			"skipped": 0
		}
	}`)

	p := &PlaywrightParser{}
	report, err := p.ParseBytes(jsonData)

	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	if report.HasFailures() {
		t.Error("Expected HasFailures() to return false")
	}
}
