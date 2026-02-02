package discovery

import (
	"testing"
)

func TestMatchesArtifactPattern(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"playwright-report", true},
		{"test-results", true},
		{"e2e-report", true},
		{"blob-report", true},
		{"coverage", false},
		{"build-artifacts", false},
		{"Playwright Report", true}, // Case insensitive
	}

	for _, tt := range tests {
		result := matchesArtifactPattern(tt.name)
		if result != tt.expected {
			t.Errorf("matchesArtifactPattern(%q) = %v, want %v",
				tt.name, result, tt.expected)
		}
	}
}

func TestDetectReportType_Playwright(t *testing.T) {
	// Standard Playwright format
	content := []byte(`{
		"config": {},
		"suites": [],
		"stats": {
			"expected": 10,
			"unexpected": 2,
			"flaky": 0,
			"skipped": 1
		}
	}`)

	reportType, hasFailures, total, failed := detectReportType(content)

	if reportType != ReportTypePlaywright {
		t.Errorf("Expected ReportTypePlaywright, got %s", reportType)
	}
	if !hasFailures {
		t.Error("Expected hasFailures to be true")
	}
	if total != 13 {
		t.Errorf("Expected total 13, got %d", total)
	}
	if failed != 2 {
		t.Errorf("Expected failed 2, got %d", failed)
	}
}

func TestDetectReportType_PlaywrightBlob(t *testing.T) {
	// Blob format uses passed/failed
	content := []byte(`{
		"suites": [],
		"stats": {
			"passed": 8,
			"failed": 3,
			"skipped": 0,
			"total": 11
		}
	}`)

	reportType, hasFailures, total, failed := detectReportType(content)

	if reportType != ReportTypePlaywright {
		t.Errorf("Expected ReportTypePlaywright, got %s", reportType)
	}
	if !hasFailures {
		t.Error("Expected hasFailures to be true")
	}
	if total != 11 {
		t.Errorf("Expected total 11, got %d", total)
	}
	if failed != 3 {
		t.Errorf("Expected failed 3, got %d", failed)
	}
}

func TestDetectReportType_Jest(t *testing.T) {
	content := []byte(`{
		"testResults": [],
		"numTotalTests": 5
	}`)

	reportType, _, _, _ := detectReportType(content)

	if reportType != ReportTypeJest {
		t.Errorf("Expected ReportTypeJest, got %s", reportType)
	}
}

func TestDetectReportType_Unknown(t *testing.T) {
	content := []byte(`{
		"some": "random",
		"data": 123
	}`)

	reportType, _, _, _ := detectReportType(content)

	if reportType != ReportTypeUnknown {
		t.Errorf("Expected ReportTypeUnknown, got %s", reportType)
	}
}

func TestDetectReportType_InvalidJSON(t *testing.T) {
	content := []byte(`not json`)

	reportType, _, _, _ := detectReportType(content)

	if reportType != ReportTypeUnknown {
		t.Errorf("Expected ReportTypeUnknown for invalid JSON, got %s", reportType)
	}
}

func TestAnalyzePlaywrightStats_NoFailures(t *testing.T) {
	stats := map[string]interface{}{
		"expected":   float64(10),
		"unexpected": float64(0),
		"flaky":      float64(0),
		"skipped":    float64(2),
	}

	hasFailures, total, failed := analyzePlaywrightStats(stats)

	if hasFailures {
		t.Error("Expected hasFailures to be false")
	}
	if total != 12 {
		t.Errorf("Expected total 12, got %d", total)
	}
	if failed != 0 {
		t.Errorf("Expected failed 0, got %d", failed)
	}
}
