package github

import "testing"

func TestClassifyArtifact(t *testing.T) {
	tests := []struct {
		name string
		want ArtifactType
	}{
		// HTML reports
		{"html-report--attempt-1", ArtifactHTML},
		{"html-report--attempt-2", ArtifactHTML},
		{"playwright-report", ArtifactHTML},

		// JSON reports
		{"test-results.json", ArtifactJSON},
		{"playwright_report.json", ArtifactJSON},

		// Blob reports
		{"blob-report-1", ArtifactBlob},
		{"blob-report-2", ArtifactBlob},
		{"blob-report-10", ArtifactBlob},

		// Unrecognized
		{"test-results", ""},
		{"e2e-coverage", ""},
		{"combined-test-catalog", ""},
		{"check-reports", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyArtifact(tt.name)
			if got != tt.want {
				t.Errorf("ClassifyArtifact(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
