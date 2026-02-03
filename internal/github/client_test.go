package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyArtifact(t *testing.T) {
	tests := []struct {
		name string
		want ArtifactType
	}{
		{"html-report--attempt-1", ArtifactHTML},
		{"html-report--attempt-2", ArtifactHTML},
		{"playwright-report", ArtifactHTML},
		{"test-results.json", ArtifactJSON},
		{"playwright_report.json", ArtifactJSON},
		{"blob-report-1", ArtifactBlob},
		{"blob-report-2", ArtifactBlob},
		{"blob-report-10", ArtifactBlob},
		{"test-results", ""},
		{"e2e-coverage", ""},
		{"combined-test-catalog", ""},
		{"check-reports", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifyArtifact(tt.name))
		})
	}
}
