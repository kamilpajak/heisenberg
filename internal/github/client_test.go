package github

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("")
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.baseURL != "https://api.github.com" {
		t.Errorf("Expected baseURL https://api.github.com, got %s", client.baseURL)
	}
}

func TestNewClientWithToken(t *testing.T) {
	client := NewClient("test-token")
	if client.token != "test-token" {
		t.Errorf("Expected token test-token, got %s", client.token)
	}
}

func TestMatchesAnyPattern(t *testing.T) {
	tests := []struct {
		filename string
		patterns []string
		expected bool
	}{
		{"report.json", []string{"*.json"}, true},
		{"results.xml", []string{"*.json"}, false},
		{"test-results.json", []string{"*.json", "*.xml"}, true},
		{"data.txt", []string{}, true}, // Empty patterns = match all
		{"nested/path/report.json", []string{"report.json"}, true},
	}

	for _, tt := range tests {
		result := matchesAnyPattern(tt.filename, tt.patterns)
		if result != tt.expected {
			t.Errorf("matchesAnyPattern(%q, %v) = %v, want %v",
				tt.filename, tt.patterns, result, tt.expected)
		}
	}
}

func TestExtractZipFiles(t *testing.T) {
	// Create a simple in-memory zip for testing
	// This would require creating actual zip bytes, which is complex
	// For now, test with nil/empty data
	_, err := extractZipFiles(nil, []string{"*.json"})
	if err == nil {
		t.Error("Expected error for nil zip data")
	}

	_, err = extractZipFiles([]byte{}, []string{"*.json"})
	if err == nil {
		t.Error("Expected error for empty zip data")
	}
}
